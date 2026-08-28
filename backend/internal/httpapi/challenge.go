package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// The challenge API: POST /present and POST /cleanup, speaking lego's httpreq
// contract in its default mode (design D2).
//
// The body lego sends is {"fqdn": "...", "value": "gfj9Xq..."} — note the
// trailing dot on the name.
//
// WHICH name arrives is the part worth being careful about, because two different
// names are both correct and both common. lego builds the payload from
// dns01.GetChallengeInfo, whose EffectiveFQDN is the result of following the
// CNAME at _acme-challenge.<domain>. With CNAME following on — its default, and
// the premise of aliasing — we receive OUR name:
//
//	{"fqdn": "demo-example-com.acmeservice.cloudpowered.services.", ...}
//
// and we receive the customer's own name:
//
//	{"fqdn": "_acme-challenge.demo.example.com.", ...}
//
// from a client that leaves the aliasing to us — lego with
// LEGO_DISABLE_CNAME_SUPPORT=true, other ACME clients whose convention that is,
// and lego itself when the CNAME does not resolve.
//
// BOTH FORMS ARE FIRST-CLASS. A name inside our configured zone resolves by alias
// LABEL; anything else resolves by challenge name. Authorization, normalization,
// status codes and log level are identical either way. Which form arrived is
// recorded as a log field and a two-valued metric label, because it says which
// convention a load balancer's client follows — and for no stronger conclusion than that;
// see findAlias.
//
// lego's HTTPREQ_MODE=RAW, which sends {"domain","token","keyAuth"} and expects
// the server to compute the TXT value itself, is deliberately NOT implemented: it
// would mean reimplementing the key-authorization digest for no benefit. A RAW
// body therefore fails validation with a 400 that names the fields we did expect,
// so the misconfiguration is diagnosable rather than mysterious.
//
// Every non-200 makes lego fail the challenge and retry later, which is exactly
// what we want when a write did not happen. Nothing here answers 200 optimistically.

// ChallengeOptions configure the challenge listener's routes. Everything except
// LastUsed and Log is required.
type ChallengeOptions struct {
	// DB is the datastore the alias lookup and the challenge writes go through.
	DB *gorm.DB

	// Zone is the zone this service is authoritative for (ACME_ZONE), e.g.
	// "acmeservice.cloudpowered.services." — with or without the trailing
	// dot. A submitted name inside it is one of our alias names and resolves
	// by label; anything else is a customer challenge name.
	Zone string

	// View is the in-memory zone view, refreshed synchronously after every
	// successful write and before the response is written — that refresh is
	// what makes "answerable by the time /present returns" true (design D6).
	View *zoneview.View

	// Lookup resolves a presented key identifier for authentication.
	Lookup apikey.Lookup

	// LastUsed records last_used_at, rate-limited. nil disables it.
	LastUsed *apikey.LastUsed

	// TrustedProxyCIDRs are the peers whose X-Forwarded-For is believed when
	// deciding a request's source address (ACME_TRUSTED_PROXY_CIDRS).
	TrustedProxyCIDRs []string

	// Lifetime is how long a published value stays answerable
	// (ACME_CHALLENGE_LIFETIME).
	Lifetime time.Duration

	// Metrics, when set, receives a counter of submitted name forms
	// (fqdn_form="alias"|"challenge") registered on its Registry. nil records
	// nothing.
	Metrics *obs.Metrics

	// Log receives publish, withdraw and refusal events. nil uses slog.Default().
	Log *slog.Logger
}

// RegisterChallenge mounts POST /present and POST /cleanup on e, behind API-key
// authentication, and installs the RealIP extractor the key's CIDR restrictions
// are judged against.
//
// It also runs apikey.CheckTrustedProxies, the fail-closed startup guard: a key
// pinned to source addresses while no proxy is trusted would be judged on a
// forgeable header, so the service refuses to start rather than display a
// protection that is not there (design D3). Returning that as an error — rather
// than leaving it to the caller to remember — is why this function can fail;
// callers must not ignore it.
//
// ctx is the startup context, used only for that check.
func RegisterChallenge(ctx context.Context, e *echo.Echo, o ChallengeOptions) error {
	switch {
	case o.DB == nil:
		return errors.New("httpapi: challenge routes need a database handle")
	case o.View == nil:
		return errors.New("httpapi: challenge routes need a zone view")
	case o.Lookup == nil:
		return errors.New("httpapi: challenge routes need an API key lookup")
	case o.Lifetime <= 0:
		return fmt.Errorf("httpapi: ACME_CHALLENGE_LIFETIME must be positive, got %s", o.Lifetime)
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	zone, err := dnsname.Normalize(o.Zone)
	if err != nil {
		return fmt.Errorf("httpapi: ACME_ZONE: %w", err)
	}

	if err := apikey.CheckTrustedProxies(ctx, o.DB, o.TrustedProxyCIDRs); err != nil {
		return err
	}
	extractor, err := apikey.IPExtractor(o.TrustedProxyCIDRs)
	if err != nil {
		return err
	}
	e.IPExtractor = extractor

	h := &challenge{opts: o, zone: zone}
	if o.Metrics != nil {
		h.formCounter, err = registerFormCounter(o.Metrics)
		if err != nil {
			return err
		}
	}
	// Route-level middleware, so authentication is part of the route's own
	// definition and cannot be left off by a caller assembling the listener.
	// It runs before the handler, and therefore before any alias lookup: an
	// unauthenticated request is answered identically whether or not the name
	// it carries exists, which is what stops the API being an oracle listing
	// the domains we serve.
	auth := apikey.Middleware(apikey.Options{
		Lookup:   o.Lookup,
		LastUsed: o.LastUsed,
		Logger:   o.Log,
	})
	// The body limit belongs to these routes rather than to the listener, since
	// design P16 put everything on one. A publish body is two short strings;
	// holding the management API's grant lists to the same 8K would be a limit
	// with no visible reason.
	limit := middleware.BodyLimit(challengeBodyLimit)

	// Namespaced, and the bare paths alongside them. HTTPREQ_ENDPOINT is a base
	// URL and the client appends the verbs itself, so the protocol can live
	// under a prefix — but every deployment already has the old endpoint
	// configured, and changing where a load balancer publishes is not a change
	// anyone should have to make to upgrade.
	for _, prefix := range []string{"", "/acme/httpreq"} {
		e.POST(prefix+"/present", h.present, auth, limit)
		e.POST(prefix+"/cleanup", h.cleanup, auth, limit)
	}
	return nil
}

type challenge struct {
	opts        ChallengeOptions
	zone        string // normalized: lowercase, no trailing dot
	formCounter *prometheus.CounterVec
}

// registerFormCounter registers the fqdn_form counter, tolerating a registry that
// already holds it so a caller may build the routes more than once (tests do) on
// one Metrics. Two label values, both compile-time constants, so this adds no
// cardinality risk.
func registerFormCounter(m *obs.Metrics) (*prometheus.CounterVec, error) {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "acmeservice_challenge_name_forms_total",
		Help: "Challenge requests by the form of the submitted name: our alias name, or the customer's challenge name.",
	}, []string{"fqdn_form"})
	if err := m.Registry.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			return already.ExistingCollector.(*prometheus.CounterVec), nil
		}
		return nil, fmt.Errorf("httpapi: register challenge name form counter: %w", err)
	}
	return c, nil
}

// errBadBody is the single 400 for a body we cannot use. It names both expected
// fields, which is what turns a RAW-mode misconfiguration from a mystery into a
// one-line diagnosis.
var errBadBody = echo.NewHTTPError(http.StatusBadRequest,
	`expected a JSON body with non-empty string fields "fqdn" and "value" `+
		`(lego httpreq default mode); HTTPREQ_MODE=RAW is not supported`)

// errRefused is the single 403 for both "no such alias" and "that alias belongs
// to another load balancer". One message for both, so an authenticated
// caller cannot map the inventory by the difference.
var errRefused = echo.NewHTTPError(http.StatusForbidden, "not authorized for that name")

// The two actions a challenge key performs, named once in the vocabulary
// package: the CLI mints keys holding them, so a literal here would be the same
// string written twice.
const (
	challengePublish  = authz.ActionChallengePublish
	challengeWithdraw = authz.ActionChallengeWithdraw
)

// errUnfunded is the 402 for a key that is valid, permitted, and anchored to an
// account that does not work (design C7, task 4.4).
//
// 402 Payment Required, and NOT 401 or 403, is the whole point of this error
// existing separately. A 401 sends an operator to look at their API key, which
// is not the problem — the key authenticated, and the grant evaluation passed.
// A 403 sends them to look at the key's grants, which are also fine. The thing
// that is wrong is upstream of both and is not something the machine holding the
// key can fix, so the status has to say so; this is one of the few genuinely
// correct uses of 402.
//
// Unlike the errors around it this is built per request rather than being a
// package-level value, because the message names the account and the reason. The
// response lands in the logs of a machine whose owner has to understand it
// without reading our documentation, and "payment required" with no subject is
// not something anybody can act on.
func errUnfunded(acc *model.Account, reason string) *echo.HTTPError {
	label := "the account this key belongs to"
	if acc != nil && acc.Slug != "" {
		label = "account " + acc.Slug
	}
	return echo.NewHTTPError(http.StatusPaymentRequired,
		label+" cannot publish challenges: "+reason+
			". The API key is valid and permitted; add credit to the account and retry.")
}

// errNotActivated is the 409 for a domain whose delegation has never been seen
// (domain-activation A4, task 3.2).
//
// 409 Conflict, and not 402, 403 or 404, because it is the only status that
// says what is actually true: the request is well-formed, the credential
// authenticated, the grant permitted it, and the account is fine — the resource
// is simply not in a state that allows the operation. Each of the four refusals
// on this path sends its reader somewhere different, and sharing a status is
// how an operator ends up rotating a key over a missing CNAME:
//
//	402  the account cannot pay              →  add credit
//	403  suspended, or the key lacks a grant →  talk to us, or fix the grant
//	409  the delegation has never been seen  →  publish the record below
//
// Built per request rather than being a package-level value, because the whole
// point is the record it names (task 3.3). This response lands in the log of
// the machine running lego, whose operator is exactly the person who can go and
// publish the CNAME — a bare "conflict" would send them to us instead.
//
// It does NOT say "we will check again shortly" in the imperative, but that is
// what happens: the activation worker keeps looking and never gives up (A2), so
// publishing the record is the entire remedy and there is nothing to re-submit
// here afterwards.
func errNotActivated(alias *model.Alias, zone string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusConflict,
		"the delegation for that name has never been observed, so nothing was published. "+
			"Publish `"+alias.ChallengeFQDN+". IN CNAME "+alias.Label+"."+zone+".` "+
			"and retry once it resolves; we re-check continuously and stop refusing as soon as we see it.")
}

// errStorage is the single 500. It names no datastore identifier, driver message
// or query text; the real error goes to the log.
var errStorage = echo.NewHTTPError(http.StatusInternalServerError, "challenge record could not be stored")

// decodeChallenge reads the httpreq body. It decodes from the reader directly
// rather than through echo's binder, so a request without (or with an odd)
// Content-Type is answered 400 like any other unusable body instead of 415 —
// lego sends application/json, but a proxy in between is not our problem to
// debug twice.
func decodeChallenge(r *http.Request) (fqdn, value string, err error) {
	var body struct {
		FQDN  string `json:"fqdn"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", "", errBadBody
	}
	if body.FQDN == "" || body.Value == "" {
		return "", "", errBadBody
	}
	return body.FQDN, body.Value, nil
}

// resolved is the state both handlers share once the request has been validated
// and authorized.
type resolved struct {
	key   *model.APIKey
	fqdn  string // normalized: lowercase, no trailing dot
	form  string // formAlias or formChallenge — which spelling the client sent
	value string
	alias *model.Alias
	// scope confines every subsequent read of this request to the key's own
	// account. It is built from the key row rather than from anything the
	// request said, which is what makes it unforgeable by a caller.
	scope store.Scope
}

// The two forms a submitted name can take. Two values, fixed forever, which is
// what makes this safe as a metric label.
const (
	// formAlias is our own name, <label>.<zone> — the client resolved the
	// customer's CNAME and is telling us where it pointed.
	formAlias = "alias"
	// formChallenge is the customer's own _acme-challenge.<domain> — the
	// client left the aliasing to us.
	formChallenge = "challenge"
)

// resolve validates the body, normalizes the name, resolves it to an alias and
// checks that the alias belongs to the load balancer the presented key is scoped to.
// Authorization is positive: the alias must exist AND be ours to write to.
func (h *challenge) resolve(c echo.Context, action string) (*resolved, error) {
	ctx := c.Request().Context()

	key := apikey.FromContext(c)
	if key == nil {
		// Unreachable while the routes are registered by RegisterChallenge;
		// present so that re-wiring them without the middleware fails closed
		// rather than writing records attributed to nobody.
		return nil, echo.ErrUnauthorized
	}

	fqdn, value, err := decodeChallenge(c.Request())
	if err != nil {
		return nil, err
	}
	// Trailing dot and case come off here: "_ACME-Challenge.Shop.Example.com"
	// and "_acme-challenge.shop.example.com." are the same name.
	name, err := dnsname.Normalize(fqdn)
	if err != nil {
		return nil, errBadBody
	}

	form, alias, err := h.findAlias(ctx, name)
	h.forms(form)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		h.refuse(ctx, key, name, form, "no alias is mapped to that name")
		return nil, errRefused
	case err != nil:
		h.opts.Log.ErrorContext(ctx, "alias lookup failed", slog.String("key_id", key.Identifier),
			slog.String("fqdn", name), slog.String("fqdn_form", form), slog.Any("error", err))
		return nil, errStorage
	}
	// Identical for both forms of the name: the alias must be ours to write to.
	//
	// Two checks, in this order, because they answer different questions and only
	// one of them is recoverable.
	//
	// The account check is tenant isolation (design P1). A key anchored to one
	// account may never touch another account's alias, whatever grants it holds —
	// there is no grant that can name an object outside its anchor, so this is not
	// a permission question and is not asked of the permission evaluator.
	if alias.AccountID != key.AccountID {
		h.refuse(ctx, key, name, form, "alias belongs to another account")
		return nil, errRefused
	}
	// The second check is authority within the account, and it is now a grant
	// evaluation rather than a comparison of foreign keys. The key holds grants
	// naming what it may reach; the alias carries its own ancestors, so the
	// evaluation is arithmetic over values already in memory and adds no query.
	//
	// The ordering is what makes this safe. Isolation was settled above, without
	// consulting the evaluator at all, so a bug in the grant model over-permits
	// inside one account and cannot cross between them.
	subject := authz.SubjectFromKey(key)
	if !authz.Can(subject, action, authz.ObjectOf(alias)) {
		h.refuse(ctx, key, name, form, "the key holds no grant permitting that action on that name")
		return nil, errRefused
	}
	// The third check, and its position is the decision (design C7, task 4.4).
	//
	// It cannot go earlier: /present does not know which account it belongs to
	// until the alias has been resolved, because that resolution is the
	// deliberately global read CrossAccount exists for — it ESTABLISHES the
	// account rather than being filtered by it. And it must not go later:
	// present's first write is store.Present and cleanup's is the withdraw, so
	// here is the last point at which nothing has changed and the key is already
	// authenticated. Putting it in present() alone would leave /cleanup working
	// for an account that cannot publish, which is a strange half-service.
	//
	// A personal account never fails this, because it has no balance to run out
	// of (C3), and store.Usable is the one place that is decided.
	//
	// Refusing does NOT withdraw what is already published. Values that are live
	// expire on the usual schedule, which is what makes this refusal soft in the
	// way C4 argues it is: it stops renewals, and the certificate on the wire
	// keeps working for weeks.
	scope := store.ScopeForKey(key)
	acc, err := store.GetAccount(ctx, h.opts.DB, scope)
	if err != nil {
		h.opts.Log.ErrorContext(ctx, "account lookup failed", slog.String("key_id", key.Identifier),
			slog.String("fqdn", name), slog.Any("error", err))
		return nil, errStorage
	}
	if !store.Usable(acc) {
		reason := store.SuspensionReason(acc)
		// Logged through the same outcome record as an authorization refusal so
		// it appears in the same place an operator already looks, with a reason
		// string that is deliberately NOT the authz vocabulary — nothing was
		// refused on authority here.
		h.refuse(ctx, key, name, form, "account not usable: "+reason)
		return nil, errUnfunded(acc, reason)
	}
	// The fourth check, and the last thing that happens before any write
	// (domain-activation task 3.1). A domain is offline until a check has seen
	// its delegation, and this is where that is enforced.
	//
	// It reads STORED state and asks no resolver. The observation was made out
	// of band by the activation worker; a live lookup here would put certificate
	// issuance behind a third party's availability, which is the failure the
	// whole design is arranged to avoid.
	//
	// PUBLISH ONLY. A withdrawal is exempt on purpose: /cleanup removes a value
	// we are already holding, and refusing it would leave that value answerable
	// in our zone until it expired — the gate would be making the storage it
	// exists to bound last longer. There is also nothing to protect, since
	// nothing is created.
	//
	// Nothing here, and nothing anywhere on this path, ever clears
	// last_success_at (A3). A domain that activated once stays activated: a
	// customer mid-DNS-migration whose delegation blips would otherwise have
	// their renewals stopped silently, and with thirty days of slack nobody
	// notices for weeks.
	if action == challengePublish {
		seen, err := store.DelegationSeen(ctx, h.opts.DB, scope, alias.ID)
		if err != nil {
			h.opts.Log.ErrorContext(ctx, "activation state lookup failed", slog.String("key_id", key.Identifier),
				slog.String("fqdn", name), slog.Any("error", err))
			return nil, errStorage
		}
		if !seen {
			// Logged through the same outcome record as the refusals above, with
			// a reason that is deliberately not the authz vocabulary: nothing
			// was refused on authority here either.
			h.refuse(ctx, key, name, form, "delegation never observed")
			return nil, errNotActivated(alias, h.zone)
		}
	}
	return &resolved{
		key: key, fqdn: name, form: form, value: value, alias: alias,
		scope: scope,
	}, nil
}

// findAlias resolves either form of the submitted name, and reports which one it
// was.
//
// BOTH FORMS ARE NORMAL, and the next reader will assume one of them is a
// mistake, so: which name a client sends is a property of the client, not of the
// customer's DNS. lego follows the CNAME at _acme-challenge.<domain> by default
// and therefore sends OUR name — but the same lego with
// LEGO_DISABLE_CNAME_SUPPORT=true sends the challenge name every time, other ACME
// clients differ by convention, and lego itself falls back to the challenge name
// when the CNAME does not resolve. The form is worth recording, as a log field
// and a two-valued metric label, to see which convention a load balancer's client uses. It
// is NOT evidence about whether the customer's CNAME is published — a warning on
// that basis would fire on every renewal of a correctly configured domain, and
// people would learn to ignore the log. The DNS-plan verification feature answers
// that question properly, by resolving the record.
//
// A name inside our zone is matched by its first label against aliases.label. An
// in-zone name that is not an alias label (a deeper name, or a label we never
// issued) simply matches nothing and is refused, which needs no special case.
func (h *challenge) findAlias(ctx context.Context, name string) (string, *model.Alias, error) {
	if label, inZone := strings.CutSuffix(name, "."+h.zone); inZone {
		var a model.Alias
		// ponytail: queried here rather than through a store.FindAliasByLabel,
		// which internal/store does not have and which is not this group's file
		// to add. Upgrade path: move this one line there and call it, alongside
		// FindAliasByChallengeName.
		//
		// CrossAccount for the same reason FindAliasByChallengeName carries it: a
		// label is unique across the deployment and is resolved BEFORE the account
		// is known, so the row-level security policy on `aliases` would otherwise
		// hide every row and refuse every publication. The account comparison
		// against the key's anchor happens in resolve, immediately after this
		// returns — that is what keeps the global read safe.
		err := store.CrossAccount(ctx, h.opts.DB, func(tx *gorm.DB) error {
			return tx.Where("label = ?", label).First(&a).Error
		})
		if err != nil {
			return formAlias, nil, err
		}
		return formAlias, &a, nil
	}
	a, err := store.FindAliasByChallengeName(ctx, h.opts.DB, name)
	return formChallenge, a, err
}

// forms counts the submitted name's form, when a metrics registry was supplied.
func (h *challenge) forms(form string) {
	if h.formCounter != nil {
		h.formCounter.WithLabelValues(form).Inc()
	}
}

func (h *challenge) refuse(ctx context.Context, key *model.APIKey, fqdn, form, reason string) {
	obs.LogOutcome(ctx, h.formLog(form), obs.EventAuthzRefusal, key.Identifier, fqdn, "", reason)
}

// formLog carries which spelling the client sent onto the outcome record. It goes
// on the same log line as the rest of the required attribute set rather than on a
// second line of its own — obs.LogOutcome fixes the attributes it writes, and a
// derived logger is the way to add one without changing that signature.
func (h *challenge) formLog(form string) *slog.Logger {
	return h.opts.Log.With(slog.String("fqdn_form", form))
}

func (h *challenge) present(c echo.Context) error {
	r, err := h.resolve(c, challengePublish)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	// Present ADDS a value; it never displaces the alias's other values. A
	// certificate covering example.com and *.example.com validates two
	// authorizations at ONE challenge name and the CA expects to find both TXT
	// values there at once. Repeating a value is an upsert on (alias_id, value),
	// so idempotency comes from the unique constraint, not a read-then-write.
	if _, err := store.Present(ctx, h.opts.DB, r.scope, r.alias.ID, r.value, r.key.Identifier, h.opts.Lifetime); err != nil {
		h.opts.Log.ErrorContext(ctx, "challenge value not stored",
			slog.String("key_id", r.key.Identifier), slog.String("fqdn", r.fqdn), slog.Any("error", err))
		return errStorage
	}
	h.openInteraction(c, r)
	return h.refreshed(c, r, obs.EventPublish, "published")
}

// openInteraction records the publication for the console's interaction history.
//
// Best effort, and loudly so: a history row that cannot be written must never
// fail a /present. The certificate is the point of this endpoint; the record of
// it is not, and a client that got a 500 here would retry a challenge that is
// already published and answerable. The failure is logged at WARN so it is
// visible without being an incident.
func (h *challenge) openInteraction(c echo.Context, r *resolved) {
	ctx := c.Request().Context()

	// The domains this alias served AT PUBLICATION TIME, copied in rather than
	// joined later: the whole point of the history is to still make sense after
	// someone deletes the domain.
	names, err := store.DomainNamesForAlias(ctx, h.opts.DB, r.scope, r.alias.ID)
	if err != nil {
		h.opts.Log.WarnContext(ctx, "interaction domains not read",
			slog.String("alias", r.alias.Label), slog.Any("error", err))
	}

	// The project, copied in for the same reason as the domain names: this row
	// keeps no foreign keys, and a load balancer that is later deleted would otherwise
	// leave its history attributable to nothing.
	//
	// Since design P3 the alias carries every ancestor a grant may name, so this
	// is a field read rather than the query it used to be — one fewer datastore
	// round trip on the publish path.
	projectID := &r.alias.ProjectID

	ip := c.RealIP()
	in := &model.ChallengeInteraction{
		ProjectID:     projectID,
		AliasID:       &r.alias.ID,
		AliasLabel:    r.alias.Label,
		ChallengeFQDN: r.alias.ChallengeFQDN,
		DomainNames:   names,
		KeyID:         &r.key.Identifier,
		FQDNForm:      r.form,
		ValueDigest:   store.ValueDigest(r.value),
		PublishedAt:   time.Now(),
	}
	if ip != "" {
		in.ClientIP = &ip
	}
	if err := store.OpenInteraction(ctx, h.opts.DB, in); err != nil {
		h.opts.Log.WarnContext(ctx, "interaction not recorded",
			slog.String("key_id", r.key.Identifier), slog.String("fqdn", r.fqdn), slog.Any("error", err))
	}
}

func (h *challenge) cleanup(c echo.Context) error {
	r, err := h.resolve(c, challengeWithdraw)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	// Only the (alias, value) pair, never every value at the name: a sibling
	// wildcard authorization may still be in flight. Removing a value that is
	// not there is not an error — clients retry cleanups, and the record may
	// already have expired and been swept.
	if err := store.Cleanup(ctx, h.opts.DB, r.scope, r.alias.ID, r.value); err != nil {
		h.opts.Log.ErrorContext(ctx, "challenge value not withdrawn",
			slog.String("key_id", r.key.Identifier), slog.String("fqdn", r.fqdn), slog.Any("error", err))
		return errStorage
	}
	// Same best-effort contract as the publication side: the value is gone from
	// DNS either way, and refusing the cleanup over a history row would leave a
	// client retrying a withdrawal that already happened.
	validated, unqueried, err := store.CloseInteraction(ctx, h.opts.DB, r.alias.ID, r.value)
	if err != nil {
		h.opts.Log.WarnContext(ctx, "interaction not closed",
			slog.String("key_id", r.key.Identifier), slog.String("fqdn", r.fqdn), slog.Any("error", err))
	} else if h.opts.Metrics != nil {
		// Which outcome a closed row gets comes from the close itself — the
		// row's query_count at the moment it was stamped withdrawn — never
		// from a second query afterwards, which could race a query
		// observation flush landing in between.
		if validated > 0 {
			h.opts.Metrics.ChallengeInteractions.WithLabelValues(obs.OutcomeValidated).Add(float64(validated))
		}
		if unqueried > 0 {
			h.opts.Metrics.ChallengeInteractions.WithLabelValues(obs.OutcomeUnqueried).Add(float64(unqueried))
		}
	}
	return h.refreshed(c, r, obs.EventWithdraw, "withdrawn")
}

// refreshed reloads the zone view and answers 200 — in that order. The write is
// only half of the promise; until the snapshot the DNS server reads from has been
// replaced, the value is not answerable, and "answerable by the time /present
// returns" would be a lie.
//
// A failed refresh is reported as a 500 even though the row is committed: the
// caller is told the state it asked for is not in effect, and lego retries. Both
// operations are idempotent, so a retry costs nothing.
func (h *challenge) refreshed(c echo.Context, r *resolved, event, outcome string) error {
	ctx := c.Request().Context()
	if err := h.opts.View.Refresh(ctx); err != nil {
		h.opts.Log.ErrorContext(ctx, "zone view not refreshed after challenge write",
			slog.String("key_id", r.key.Identifier), slog.String("fqdn", r.fqdn), slog.Any("error", err))
		return errStorage
	}
	obs.LogOutcome(ctx, h.formLog(r.form), event, r.key.Identifier, r.fqdn, r.alias.Label, outcome)
	return c.NoContent(http.StatusOK)
}
