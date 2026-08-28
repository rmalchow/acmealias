package httpapi

import (
	"context"
	"log/slog"
	"net"
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

// The DNS plan (acme-alias-service D5): structured, rendered, and checked
// against live DNS.
//
// Two changes are cited by number below, and both number their decisions from
// D1: acme-alias-service, which specified the plan, and domain-setup-feedback,
// which made the check run per domain and answer per record type. Every
// citation here names its change, because "D5" alone means two things in this
// file.

type recordView struct {
	Name  string `json:"name"`
	TTL   int    `json:"ttl"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type planView struct {
	ProjectSlug string       `json:"projectSlug"`
	ProjectName string       `json:"projectName"`
	Records     []recordView `json:"records"`
	Zone        string       `json:"zone"`
}

func (a *admin) plan(c echo.Context) (*inventory.DNSPlan, error) {
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
	return inventory.Plan(c.Request().Context(), a.db(), sc, subj, id, a.planOptions())
}

func (a *admin) getPlan(c echo.Context) error {
	p, err := a.plan(c)
	if err != nil {
		return err
	}
	v := planView{
		ProjectSlug: p.ProjectSlug, ProjectName: p.ProjectName,
		Records: make([]recordView, 0, len(p.Records)),
		Zone:    a.opts.Config.Zone,
	}
	for _, r := range p.Records {
		v.Records = append(v.Records, recordView{Name: r.Name, TTL: r.TTL, Type: r.Type, Value: r.Value})
	}
	return c.JSON(http.StatusOK, v)
}

// getPlanZone serves the same plan as the zone-file snippet the customer's DNS
// administrator can paste. text/plain, so a browser shows it and `curl >
// file` produces something usable.
func (a *admin) getPlanZone(c echo.Context) error {
	p, err := a.plan(c)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderContentDisposition,
		`attachment; filename="`+p.ProjectSlug+`.zone"`)
	return c.String(http.StatusOK, inventory.RenderZoneSnippet(p))
}

// --- live verification -----------------------------------------------------

// checkResult is one verified row of the plan. It is deliberately not
// one-per-plan-record: a name's four A records are one question to DNS and one
// answer, and reporting them as four independent "incorrect" rows would say
// the same thing four times without saying which address was missing.
//
// Status carries resolvercheck's vocabulary unchanged, including CAA's
// "not-published", which the console must render as information rather than as
// a failure: an absent CAA record is the default state of the internet and
// blocks nothing, while a CAA record naming another authority is the single
// clearest blocker this check can find (domain-setup-feedback D3).
type checkResult struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Expected []string `json:"expected"`
	Status   string   `json:"status"`
	Found    string   `json:"found,omitempty"`
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
// delegation is
// reported on an axis of presence and CAA on an axis of permission, and a
// single verdict per domain would have to pick one of the two vocabularies and
// then lie in the other. In particular CAA's commonest state, `not-published`,
// is entirely correct — anyone may issue — and folding it into a domain verdict
// would flag nearly every customer.
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

// verifyPlan resolves the plan and answers it twice over: `results`, one row
// per question asked of DNS, which is what the whole-plan control shows; and
// `domains`, the same answers redistributed onto the rows a customer actually
// reads. Both come from the one pass, so the per-domain view costs no extra
// lookups.
//
// It remains a read that stores nothing — a check is an observation, and an
// observation kept becomes a stale claim (domain-setup-feedback D1). That is
// why the route is
// a GET without a CSRF token, and why running it on page load changes only when
// it happens.
func (a *admin) verifyPlan(c echo.Context) error {
	p, err := a.plan(c)
	if err != nil {
		return err
	}

	// No resolver: answer the same shape with every domain reading "not
	// checked" (domain-setup-feedback D4). The route used to return 503 here,
	// which was
	// tolerable for a button somebody chose to press and is not for a check
	// that runs whenever a screen opens: a deployment that never set
	// ACME_VERIFY_RESOLVER has declined the feature, and telling a customer
	// their setup is broken is the one thing that answer does not mean.
	out := []checkResult{}
	if a.resolver != "" {
		out = runChecks(c.Request().Context(), a.checker,
			groupForChecks(p.Records, a.opts.Config.CAAIssuers))
	}
	return c.JSON(http.StatusOK, map[string]any{
		"resolver": a.resolver,
		"results":  out,
		"domains":  domainChecks(p, a.opts.Config.CAAIssuers, out),
	})
}

// domainChecks redistributes plan-wide results onto the domains that need
// them.
//
// Which records a domain needs comes from the plan, never from the results, so
// a domain whose check did not run still reports itself with statusNotChecked
// instead of vanishing. The join is by challenge name for the delegation and by
// registrable domain for CAA, both of which are shared: a wildcard and its apex
// have one challenge name between them and therefore one delegation, and every
// host under example.com reads the one CAA set at example.com. Joining on the
// domain's own name would leave every row but the first reporting a record
// somebody else's row already covers as absent.
//
// Address records are deliberately not here. They are the plan's question, not
// the delegation's, and `results` still answers them whole.
func domainChecks(p *inventory.DNSPlan, issuers []string, results []checkResult) []domainCheck {
	byKey := make(map[string]checkResult, len(results))
	for _, r := range results {
		byKey[r.Type+"|"+r.Name] = r
	}
	caaPlanned := map[string]bool{}
	for _, r := range p.Records {
		if r.Type == "CAA" {
			caaPlanned[r.Name] = true
		}
	}

	out := make([]domainCheck, 0, len(p.Domains))
	for _, d := range p.Domains {
		dc := domainCheck{
			DomainID: d.DomainID, Name: d.Name,
			ChallengeName: d.ChallengeName, AliasID: d.AliasID,
			Delegation: recordCheck{Name: d.ChallengeName, Status: statusNotChecked},
		}
		if d.Target != "" {
			// The customer's own target, carried whatever the status: the help
			// that explains a missing or wrong record is worth nothing without
			// the exact value to publish (domain-setup-feedback D5).
			dc.Delegation.Expected = []string{d.Target}
		}
		if r, ok := byKey["CNAME|"+d.ChallengeName]; ok {
			dc.Delegation.Status, dc.Delegation.Found = r.Status, r.Found
		}
		if d.Registrable != "" && caaPlanned[d.Registrable] {
			caa := recordCheck{Name: d.Registrable, Status: statusNotChecked, Expected: issuers}
			if r, ok := byKey["CAA|"+d.Registrable]; ok {
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
func runChecks(ctx context.Context, checker *resolvercheck.Checker, checks []planCheck) []checkResult {
	out := make([]checkResult, len(checks))
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

// planCheck is one question to ask DNS. Exactly one of the three is set.
type planCheck struct {
	name     string
	kind     string
	expected []string

	cname   *resolvercheck.CNAMECheck
	address *resolvercheck.AddressCheck
	caa     *resolvercheck.CAACheck
}

func (p planCheck) run(ctx context.Context, ch *resolvercheck.Checker) checkResult {
	res := checkResult{Name: p.name, Type: p.kind, Expected: p.expected}
	switch {
	case p.cname != nil:
		r := ch.CheckCNAME(ctx, *p.cname)
		res.Status, res.Found = string(r.Status), r.Found
	case p.address != nil:
		r := ch.CheckAddress(ctx, *p.address)
		res.Status, res.Found = string(r.Status), r.Found
	case p.caa != nil:
		r := ch.CheckCAA(ctx, *p.caa)
		res.Status, res.Found = string(r.Status), r.Found
	}
	return res
}

// groupForChecks turns plan rows into the checks resolvercheck actually takes.
//
// The plan emits one row per address per name (acme-alias-service D5's
// table), but an
// AddressCheck compares the *whole* answer set for a name and family at once,
// so the rows have to be gathered back up. CAA rows are collapsed the same
// way: the plan emits one row per tag per issuer at a registrable domain, and
// one CAACheck covers the set.
//
// ponytail: this adapter lives here because the management API is its only
// caller — there is no CLI verification subcommand in this change. If one is
// ever added, move it to internal/inventory beside Plan rather than copying it.
func groupForChecks(records []inventory.Record, issuers []string) []planCheck {
	var out []planCheck
	addrIdx := map[string]int{} // "name|family" -> index into out
	caaSeen := map[string]bool{}

	for _, r := range records {
		switch r.Type {
		case "CNAME":
			out = append(out, planCheck{
				name: r.Name, kind: "CNAME", expected: []string{r.Value},
				cname: &resolvercheck.CNAMECheck{Name: r.Name, Expected: dns.Fqdn(r.Value)},
			})
		case "A", "AAAA":
			key := r.Name + "|" + r.Type
			ip := net.ParseIP(r.Value)
			if ip == nil {
				continue // an unparsable address in the plan is a data bug, not a DNS finding
			}
			if i, ok := addrIdx[key]; ok {
				out[i].expected = append(out[i].expected, r.Value)
				out[i].address.Want = append(out[i].address.Want, ip)
				continue
			}
			addrIdx[key] = len(out)
			out = append(out, planCheck{
				name: r.Name, kind: r.Type, expected: []string{r.Value},
				address: &resolvercheck.AddressCheck{Name: r.Name, Family: r.Type, Want: []net.IP{ip}},
			})
		case "CAA":
			if caaSeen[r.Name] {
				continue
			}
			caaSeen[r.Name] = true
			out = append(out, planCheck{
				name: r.Name, kind: "CAA", expected: issuers,
				caa: &resolvercheck.CAACheck{Domain: r.Name, Issuers: issuers},
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
	p, err := a.plan(c) // authorizes dns.plan on the load balancer, as the GET does
	if err != nil {
		return err
	}
	// No resolver configured: the same 200 the GET answers, with every row
	// reading "not checked", and nothing recorded. We are the ones who did not
	// look, and stamping last_checked_at for a question we never asked would
	// make the queue think this delegation had been attended to
	// (domain-setup-feedback D4).
	out := []checkResult{}
	if a.resolver != "" {
		out = runChecks(c.Request().Context(), a.checker,
			groupForChecks(p.Records, a.opts.Config.CAAIssuers))
	}
	domains := domainChecks(p, a.opts.Config.CAAIssuers, out)
	a.recordDelegations(c, domains)
	return c.JSON(http.StatusOK, map[string]any{
		"resolver": a.resolver,
		"results":  out,
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
