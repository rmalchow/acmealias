//go:build acceptance

package acceptance

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Onboarding a domain the way a customer does — the half of the arc that lives
// outside the service (domain-activation A1, task 5.6).
//
// A domain is OFFLINE until a check has seen its delegation: the customer's
//
//	_acme-challenge.their.domain.  CNAME  <alias>.dev.acmeservice.internal.
//
// and /present answers 409 for a domain that has never been activated. So a
// test that creates a domain and publishes for it is not testing what it thinks
// it is unless it first does the thing only the customer can do — publish that
// CNAME in their own DNS — and then waits for us to have seen it.
//
// In this stack the customer's DNS is CoreDNS serving example.com
// (development/dns/zones/example.com.zone, rendered from the checked-in
// template by development/scripts/lib.sh's render_zone). That is also the only
// zone the fake internet answers for, which is why the tests that use this put
// their domains under example.com and not, as they once did, under a name that
// resolves nowhere and can therefore never activate.

// zoneDelegations reads the _acme-challenge CNAMEs the served zone currently
// carries, in the shape render_zone takes them back.
//
// The file is the source of truth rather than a variable this package keeps:
// development/scripts/seed.sh wrote the seeded delegation there and nothing in
// Go saw it happen, and a helper that restored a remembered list would quietly
// delete it.
func zoneDelegations(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, rr := range zoneFileRecords(t) {
		c, ok := rr.(*dns.CNAME)
		if !ok || !strings.HasPrefix(c.Hdr.Name, "_acme-challenge.") {
			continue
		}
		out = append(out, c.Hdr.Name+"  IN CNAME  "+c.Target)
	}
	return out
}

// renderZone re-renders the served zone from its template with exactly these
// delegation records, and makes CoreDNS serve the result.
//
// It shells out to render_zone rather than reimplementing it, so the marker in
// development/dns/zones/example.com.zone.tmpl has one substituter and a change
// to the template cannot leave this suite writing a stale shape.
//
// The restart is not decoration. CoreDNS's `file` plugin re-reads a changed
// zone on a poll timer, so writing the file and carrying on races every caller
// — the same reason development/scripts/seed.sh restarts it.
//
// And it does not return until CoreDNS is answering again, which matters far
// beyond this file: compose.yaml gives EVERY container `dns: 172.28.0.53`, so
// while CoreDNS is down nothing in the stack can resolve a name. Returning
// early hands the next test a stack whose resolver is missing for a second or
// two — and the test that follows the restore here is the one that orders a
// certificate, where a resolver that is briefly absent costs a whole issuance
// and reads as an unrelated failure.
func renderZone(t *testing.T, lines []string) {
	t.Helper()
	args := append([]string{
		"-c", `source development/scripts/lib.sh; render_zone "$@"`, "render_zone",
	}, lines...)
	if out, err := run("bash", args...); err != nil {
		t.Fatalf("rendering development/dns/zones/example.com.zone: %v\n%s", err, out)
	}
	compose(t, "restart", "coredns")
	eventually(t, time.Minute, "the fake internet answering again after a restart", func() error {
		r, err := query(fakeNetDNS, "example.com.", dns.TypeSOA)
		if err != nil {
			return err
		}
		if len(r.Answer) == 0 {
			return fmt.Errorf("example.com. SOA: %s with no answer", dns.RcodeToString[r.Rcode])
		}
		return nil
	})
}

// onboardDomain publishes d's delegation into the fake internet and then makes
// the service look, leaving d activated and therefore publishable.
//
// The second half is a POST to the load balancer's verification route — the
// console's "check now", which unlike the GET beside it RECORDS what it saw
// (internal/httpapi/admin_plan.go, recheckPlan). Activation is deliberately not
// waited for from the poller: development/config-development.yaml runs it every
// fifteen seconds, but a test that sleeps for a background worker is a test
// that is flaky on a slow machine, and asking is both faster and exact.
//
// It is retried, and that is the resolvercheck cache rather than optimism:
// answers are memoized for twenty seconds (internal/resolvercheck/cache.go), so
// the activation poller may well have cached this name's NXDOMAIN moments
// before the zone changed underneath it.
//
// The zone is restored on cleanup. TestGeneratedPlanMatchesTheServedZone
// compares the seeded plan against the served zone record for record, in both
// directions, so a delegation left behind by one test fails another.
func onboardDomain(t *testing.T, s *session, lb string, d domainRef) {
	t.Helper()

	target := dns.Fqdn(aliasNameFor(d.AliasLabel))
	challenge := dns.Fqdn(d.ChallengeName)

	before := zoneDelegations(t)
	renderZone(t, append(slices.Clone(before), challenge+"  IN CNAME  "+target))
	t.Cleanup(func() { renderZone(t, before) })

	eventually(t, time.Minute, "the fake internet serving "+challenge, func() error {
		r, err := query(fakeNetDNS, challenge, dns.TypeCNAME)
		if err != nil {
			return err
		}
		for _, rr := range r.Answer {
			if c, ok := rr.(*dns.CNAME); ok && c.Target == target {
				return nil
			}
		}
		return fmt.Errorf("%s does not answer %s yet (%v)", challenge, target, r.Answer)
	})

	eventually(t, 2*time.Minute, "activating "+d.Name, func() error {
		var res struct {
			Resolver string `json:"resolver"`
			Domains  []struct {
				Name       string `json:"name"`
				Delegation struct {
					Status   string   `json:"status"`
					Expected []string `json:"expected"`
					Found    string   `json:"found"`
				} `json:"delegation"`
			} `json:"domains"`
		}
		if err := s.json(http.MethodPost, s.acct("/loadbalancers/"+lb+"/verification"),
			nil, http.StatusOK, &res); err != nil {
			return err
		}
		if res.Resolver == "" {
			// Nothing was asked, so nothing was recorded, and no amount of
			// retrying will change that: the deployment configured no resolver
			// (ACME_VERIFY_RESOLVER, development/.env).
			t.Fatalf("the service has no verification resolver configured, so a check-now "+
				"records nothing and %s can never activate", d.Name)
		}
		for _, dc := range res.Domains {
			if !strings.EqualFold(strings.TrimSuffix(dc.Name, "."), strings.TrimSuffix(d.Name, ".")) {
				continue
			}
			if dc.Delegation.Status == "correct" {
				return nil
			}
			return fmt.Errorf("the delegation for %s reads %q (expected %v, found %q)",
				d.Name, dc.Delegation.Status, dc.Delegation.Expected, dc.Delegation.Found)
		}
		return fmt.Errorf("the check-now answer covers %d domains and none of them is %s",
			len(res.Domains), d.Name)
	})
}
