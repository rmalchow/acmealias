package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/miekg/dns"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/resolvercheck"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Verification: what a project's domains need published in somebody else's
// zone, checked against live DNS and answered per domain.
//
// The PLAN is gone — the record table, its zone snippet and the `results` array
// that answered the same questions a second time, keyed by record instead of by
// domain (change domain-diagnostics, G2). What a domain needs is now a property
// of the domain (`cnameTarget`, `expectedCaa` on the domain read), and this file
// keeps only the half nobody could get anywhere else: what DNS actually says.
//
// Three changes are cited by number below, and two of them number their
// decisions from D1: acme-alias-service, which specified the plan, and
// domain-setup-feedback, which made the check run per domain and answer per
// record type. Every citation names its change, because "D5" alone means two
// things in this file.

// recordKinds is which record types one verification pass asks about.
//
// The console has a button per table now — Delegation and CAA are separate
// questions, asked at separate moments — and this is what keeps the request
// honest about that. Filtering the ANSWER client-side would have been half a
// line of code and a lie: the queries would still have gone to somebody's
// recursor, and the user pressing "check the CAA" would have doubled the DNS
// traffic of the screen without being told.
//
// The zero value asks everything, so a caller that says nothing — the recorded
// re-check, an older console, curl — behaves exactly as before.
type recordKinds struct {
	delegation bool
	caa        bool
}

func (r recordKinds) wants(kind string) bool {
	if !r.delegation && !r.caa {
		return true
	}
	return (kind == "CNAME" && r.delegation) || (kind == "CAA" && r.caa)
}

// parseRecordKinds reads ?records=delegation,caa.
//
// An unrecognised value is IGNORED rather than refused, and an empty result asks
// everything. A typo in a query parameter should cost a caller nothing worse
// than the answer they would have got without it — this is a filter on how much
// work to do, not an instruction whose misspelling means something else.
func parseRecordKinds(c echo.Context) recordKinds {
	var out recordKinds
	for _, want := range strings.Split(c.QueryParam("records"), ",") {
		switch strings.TrimSpace(strings.ToLower(want)) {
		case "delegation", "cname":
			out.delegation = true
		case "caa":
			out.caa = true
		}
	}
	return out
}

// statusNotChecked is the one status on the wire resolvercheck cannot produce,
// because nothing was asked: the deployment configured no resolver. It is not a
// finding about the customer's DNS and must never be rendered as one — we are
// the ones who did not look (domain-setup-feedback D4).
const statusNotChecked = "not-checked"

// recordCheck is one record type's answer for one domain.
//
// Status carries resolvercheck's vocabulary unchanged — `missing` and
// `incorrect` stay apart, because "publish this record" and "you published it,
// at the wrong target" are different instructions to different people
// (domain-setup-feedback D2) — plus statusNotChecked.
type recordCheck struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Expected []string `json:"expected,omitempty"`
	Found    string   `json:"found,omitempty"`
}

// domainCheck is one domain's answer, one field per record type.
//
// The fields are kept apart deliberately (domain-setup-feedback D2, D3): the
// delegation is reported on an axis of presence and CAA on an axis of
// permission, and a single verdict per domain would have to pick one of the two
// vocabularies and then lie in the other. A missing delegation means nothing
// works; an unpinned CAA set means everything works, for more people than the
// customer intended. Those are not two points on one scale.
//
// CAA's commonest state, `not-published`, USED TO BE REPORTED AS CORRECT here on
// the grounds that absence lets anybody issue and therefore lets us. It is now
// the worst of the three, for the reason set out at length on CAAStatus: on a
// domain that has delegated its challenge name to this service, "anybody may
// issue" includes this service, and accounturi is the only record that says
// otherwise.
//
// CAA is absent, rather than some fifth status, when the plan holds no CAA row
// for this domain: with no issuer configured (ACME_CAA_ISSUER unset) there is
// nothing to authorize and therefore nothing to say.
type domainCheck struct {
	DomainID      model.ID     `json:"domainId"`
	Name          string       `json:"name"`
	ChallengeName string       `json:"challengeName"`
	AliasID       model.ID     `json:"aliasId"`
	Delegation    recordCheck  `json:"delegation"`
	CAA           *recordCheck `json:"caa,omitempty"`
}

// expectations resolves what a project's domains need published, authorizing
// dns.plan on the project as it always did.
func (a *admin) expectations(c echo.Context) ([]inventory.DomainExpectation, error) {
	id, err := idParam(c, "id")
	if err != nil {
		return nil, err
	}
	sc, err := a.scope(c)
	if err != nil {
		return nil, err
	}
	subj, err := a.subject(c)
	if err != nil {
		return nil, err
	}
	return inventory.Expectations(c.Request().Context(), a.db(), sc, subj, id, a.planOptions())
}

// verifyPlan answers what each of a project's domains actually resolves to.
//
// It answers ONCE, per domain. The `results` array it used to carry beside
// `domains` said the same thing keyed by record, and only the per-domain keying
// ever had a reader (domain-diagnostics G2); a second view of one pass is a
// second thing that can be read and believed after the first has moved on.
//
// It remains a read that stores nothing — a check is an observation, and an
// observation kept becomes a stale claim (domain-setup-feedback D1). That is
// why the route is a GET without a CSRF token, and why running it on page load
// changes only when it happens.
func (a *admin) verifyPlan(c echo.Context) error {
	exps, err := a.expectations(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"resolver": a.resolver,
		"domains":  domainChecks(exps, a.ask(c, exps)),
	})
}

// ask puts the questions to the resolver and returns the answers indexed by the
// question each one answers.
//
// No resolver configured: no questions, an empty index, and every domain reads
// "not checked" (domain-setup-feedback D4). The route used to answer 503 here,
// which was tolerable for a button somebody chose to press and is not for a
// check that runs whenever a screen opens — a deployment that never set
// ACME_VERIFY_RESOLVER has declined the feature, and telling a customer their
// setup is broken is the one thing that answer does not mean.
func (a *admin) ask(c echo.Context, exps []inventory.DomainExpectation) map[string]recordCheck {
	if a.resolver == "" {
		return nil
	}
	checks := groupForChecks(exps, a.opts.Config.CAAIssuers, parseRecordKinds(c))
	answers := runChecks(c.Request().Context(), a.checker, checks)
	// Zipped by position rather than by anything in the answer, which is what
	// makes runChecks free to keep its results in a slice: the question already
	// carries the key, and re-deriving it from the answer would be a second
	// place for the two to disagree.
	out := make(map[string]recordCheck, len(answers))
	for i, ans := range answers {
		out[checks[i].kind+"|"+checks[i].name] = ans
	}
	return out
}

// domainChecks redistributes the answers onto the domains that need them.
//
// Which records a domain needs comes from the EXPECTATION, never from the
// answers, so a domain whose check did not run still reports itself with
// statusNotChecked instead of vanishing. The join is by challenge name for the
// delegation and by registrable domain for CAA, both of which are shared: a
// wildcard and its apex have one challenge name between them and therefore one
// delegation, and every host under example.com reads the one CAA set at
// example.com. Joining on the domain's own name would leave every row but the
// first reporting a record somebody else's row already covers as absent.
func domainChecks(exps []inventory.DomainExpectation, answers map[string]recordCheck) []domainCheck {
	out := make([]domainCheck, 0, len(exps))
	for _, e := range exps {
		dc := domainCheck{
			DomainID: e.DomainID, Name: e.Name,
			ChallengeName: e.ChallengeName, AliasID: e.AliasID,
			Delegation: recordCheck{Name: e.ChallengeName, Status: statusNotChecked},
		}
		if e.Target != "" {
			// The customer's own target, carried whatever the status: the help
			// that explains a missing or wrong record is worth nothing without
			// the exact value to publish (domain-setup-feedback D5).
			dc.Delegation.Expected = []string{e.Target}
		}
		if r, ok := answers["CNAME|"+e.ChallengeName]; ok {
			dc.Delegation.Status, dc.Delegation.Found = r.Status, r.Found
		}
		// Absent CAA rdata is no issuer configured: nothing to authorize, and so
		// nothing to say. Not a fifth status — "not-published" would be a claim
		// about the customer's zone we never went and made.
		if len(e.ExpectedCAA) > 0 {
			caa := recordCheck{Name: e.Registrable, Status: statusNotChecked, Expected: e.ExpectedCAA}
			if r, ok := answers["CAA|"+e.Registrable]; ok {
				caa.Status, caa.Found = r.Status, r.Found
			}
			dc.CAA = &caa
		}
		out = append(out, dc)
	}
	return out
}

// runChecks asks the resolver every question of the plan, in parallel and
// bounded, and returns the answers in the order they were asked.
//
// Sequential was fine for a button. It is not fine for a check that runs on
// every page open, because one unanswerable name then delays every other
// domain's row by a full timeout: a load balancer with twenty domains asks
// enough questions that a blackholing resolver would hold the handler for
// minutes with every row reading "not checked" throughout. The spec asks that
// a slow lookup degrade only the domain it concerns, and only concurrency can
// keep that promise across rows (domain-setup-feedback D1).
//
// The cap is what stops the promise being kept at the resolver's expense: one
// screen must not open sixty simultaneous queries at somebody's recursor.
// Rejected: one goroutine per question — cheap for us, and a burst nobody
// asked for at the far end.
func runChecks(ctx context.Context, checker *resolvercheck.Checker, checks []planCheck) []recordCheck {
	out := make([]recordCheck, len(checks))
	sem := make(chan struct{}, checkParallelism)
	var wg sync.WaitGroup
	for i, ch := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = ch.run(ctx, checker)
		}()
	}
	wg.Wait()
	return out
}

// checkParallelism bounds the questions one verification has outstanding.
const checkParallelism = 8

// planCheck is one question to ask DNS. Exactly one of the two is set.
//
// kind is "CNAME" or "CAA" and is half the key an answer is filed under, the
// other half being name. Two questions of different kinds can share a name in
// principle, and a key that could collide is a delegation reported as a CAA.
type planCheck struct {
	name     string
	kind     string
	expected []string

	cname *resolvercheck.CNAMECheck
	caa   *resolvercheck.CAACheck
}

func (p planCheck) run(ctx context.Context, ch *resolvercheck.Checker) recordCheck {
	res := recordCheck{Name: p.name, Expected: p.expected}
	switch {
	case p.cname != nil:
		r := ch.CheckCNAME(ctx, *p.cname)
		res.Status, res.Found = string(r.Status), r.Found
	case p.caa != nil:
		r := ch.CheckCAA(ctx, *p.caa)
		res.Status, res.Found = string(r.Status), r.Found
	}
	return res
}

// groupForChecks turns per-domain expectations into the questions resolvercheck
// actually takes, asking each one ONCE.
//
// The deduplication is the whole job and it is not an optimisation. Two domains
// that share an alias carry the identical challenge name, and every domain under
// one registrable name carries the identical CAA expectation — so a project with
// eight domains sharing four aliases would put four times the queries on
// somebody's recursor per screen open if this grouped by domain. Grouping on the
// VALUE is also what makes a wildcard and its apex share one answer, which is
// what domainChecks then hands back to both rows.
//
// Address records are gone and are not coming back: the customer typed those
// addresses into the Nodes panel themselves, and grading their DNS against them
// is a check whose only possible finding is that the customer disagrees with the
// customer (domain-diagnostics G2).
//
// issuers is passed separately from the expectation's rendered rdata because a
// CAACheck compares issuer identifiers, not zone-file lines; the rdata is what
// the customer publishes and the identifier is what we recognise in an answer.
//
// ponytail: this adapter lives here because the management API is its only
// caller — there is no CLI verification subcommand. If one is ever added, move
// it to internal/inventory beside Expectations rather than copying it.
func groupForChecks(exps []inventory.DomainExpectation, issuers []string, want recordKinds) []planCheck {
	var out []planCheck
	seen := map[string]bool{}
	for _, e := range exps {
		// No alias yet: nothing to ask for. The row still appears in the answer,
		// reading "not checked", because domainChecks builds from the
		// expectations rather than from these questions — and the same is true
		// of a record type this pass was not asked about, which is what lets the
		// console merge a delegation-only answer over the CAA answers it already
		// has instead of losing them.
		if want.wants("CNAME") && e.Target != "" && !seen["CNAME|"+e.ChallengeName] {
			seen["CNAME|"+e.ChallengeName] = true
			out = append(out, planCheck{
				name: e.ChallengeName, kind: "CNAME", expected: []string{e.Target},
				cname: &resolvercheck.CNAMECheck{Name: e.ChallengeName, Expected: dns.Fqdn(e.Target)},
			})
		}
		if want.wants("CAA") && len(e.ExpectedCAA) > 0 && !seen["CAA|"+e.Registrable] {
			seen["CAA|"+e.Registrable] = true
			out = append(out, planCheck{
				name: e.Registrable, kind: "CAA", expected: e.ExpectedCAA,
				caa: &resolvercheck.CAACheck{Domain: e.Registrable, Issuers: issuers},
			})
		}
	}
	return out
}

// verificationResolver decides which resolver live checks query, once, at
// registration.
//
// ACME_VERIFY_RESOLVER wins. When it is unset the design says "the system
// resolver", which for a DNS library means reading the same /etc/resolv.conf
// the stdlib would: resolvercheck speaks to a named server, it has no "ask the
// OS" mode. When neither is available every check reads "not checked" rather
// than "undetermined", which would read as a customer-side problem
// (domain-setup-feedback D4).
func verificationResolver(cfg config.Config, log *slog.Logger) string {
	addr, err := resolvercheck.ResolverAddress(strings.TrimSpace(cfg.VerificationResolver))
	if err != nil {
		log.Warn("no DNS verification resolver: ACME_VERIFY_RESOLVER is unset and no system resolver could be read",
			slog.Any("error", err))
		return ""
	}
	return addr
}

// --- check now (domain-activation task 4.3) --------------------------------

// recheckPlan is the verification above, run because somebody asked for it, and
// RECORDED. It is the console's "check now": a customer who has just published
// their CNAME should not have to wait out the poller's interval to see the
// domain come online.
//
// Why this is a POST when GET /verification answers the identical body: the two
// are different acts. The GET is an observation and stores nothing, which is
// what lets it run on every screen open (domain-setup-feedback D1). This one
// writes activation state, so it is a request somebody makes rather than one a
// page makes on their behalf — and it goes through the CSRF token the console
// already sends with every other write.
//
// Rejected: making the GET store whatever it saw. It would have activated
// domains as a side effect of opening a screen, which is the kind of surprise
// that makes a read unsafe to retry, and it would have made a page load a write
// on the customer's behalf that they never asked for.
//
// Rejected: a per-domain route. The delegation is per ALIAS and the plan already
// resolves every one of a load balancer's in a single bounded pass, so a
// per-domain endpoint would ask the same questions once per row and still have
// to apply each answer to both the apex and its wildcard.
//
// It NEVER deactivates. A pass here that fails to see a delegation stamps
// last_checked_at and leaves last_success_at alone (A3) — pressing "check now"
// while a zone is mid-rebuild must not be a way to take your own domain
// offline.
func (a *admin) recheckPlan(c echo.Context) error {
	exps, err := a.expectations(c) // authorizes dns.plan on the project, as the GET does
	if err != nil {
		return err
	}
	// With no resolver configured a.ask asks nothing, so every row reads "not
	// checked" and recordDelegations writes nothing: we are the ones who did not
	// look, and stamping last_checked_at for a question we never asked would make
	// the queue think this delegation had been attended to (domain-setup-feedback
	// D4).
	domains := domainChecks(exps, a.ask(c, exps))
	a.recordDelegations(c, domains)
	return c.JSON(http.StatusOK, map[string]any{
		"resolver": a.resolver,
		"domains":  domains,
	})
}

// recordDelegations writes what the pass above saw, one write per alias.
//
// Collapsed by alias id because that is the unit of the delegation: a wildcard
// and its apex produce two identical domainChecks from one lookup, and writing
// twice would be two statements of one fact. The store's own updates are keyed
// on alias_id for the same reason, so both rows are stamped by either write.
//
// A failure to record is logged and not returned. The customer asked what their
// DNS says, and they get that answer; the durable stamp is a side effect, and
// the worker will come round again regardless.
func (a *admin) recordDelegations(c echo.Context, domains []domainCheck) {
	ctx := c.Request().Context()
	at := time.Now().UTC()
	done := make(map[model.ID]bool, len(domains))
	for _, d := range domains {
		if d.AliasID == model.NilID || done[d.AliasID] {
			continue
		}
		done[d.AliasID] = true

		var err error
		switch d.Delegation.Status {
		case statusNotChecked:
			// Nothing was asked, so nothing is recorded — not even that we
			// looked, because we did not.
			continue
		case string(resolvercheck.StatusCorrect):
			_, err = store.RecordDelegationSeen(ctx, a.db(), d.AliasID, at)
		default:
			// missing, incorrect and undetermined alike: we looked and cannot
			// publish for this domain yet. The console draws the distinction
			// between them from the answer above; the stored state does not
			// need it, and A3 means nothing bad follows from an undetermined.
			_, err = store.RecordDelegationMissing(ctx, a.db(), d.AliasID, at)
		}
		if err != nil {
			a.opts.Log.WarnContext(ctx, "delegation check not recorded",
				slog.String("delegation", d.ChallengeName), slog.Any("error", err))
		}
	}
}
