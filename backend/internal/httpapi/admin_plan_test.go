package httpapi

import (
	"context"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/resolvercheck"
)

// expectedDomains is the shape domainChecks joins against: three domains, two
// challenge names (the apex and its wildcard share one), one registrable domain
// — the same worked example internal/inventory's own tests use.
func expectedDomains() []inventory.DomainExpectation {
	apexAlias, shopAlias := model.NewID(), model.NewID()
	caa := []string{`0 issue "letsencrypt.org"`, `0 issuewild "letsencrypt.org"`}
	return []inventory.DomainExpectation{
		{DomainID: model.NewID(), Name: "example.com", ChallengeName: "_acme-challenge.example.com",
			AliasID: apexAlias, Target: "example-com.svc.example.net", Registrable: "example.com", ExpectedCAA: caa},
		{DomainID: model.NewID(), Name: "*.example.com", ChallengeName: "_acme-challenge.example.com",
			AliasID: apexAlias, Target: "example-com.svc.example.net", Registrable: "example.com", ExpectedCAA: caa},
		{DomainID: model.NewID(), Name: "shop.example.com", ChallengeName: "_acme-challenge.shop.example.com",
			AliasID: shopAlias, Target: "shop-example-com.svc.example.net", Registrable: "example.com", ExpectedCAA: caa},
	}
}

// withoutCAA is the same three domains with no issuer configured, which is what
// leaves ExpectedCAA nil.
func withoutCAA(exps []inventory.DomainExpectation) []inventory.DomainExpectation {
	out := append([]inventory.DomainExpectation(nil), exps...)
	for i := range out {
		out[i].ExpectedCAA = nil
	}
	return out
}

// answered indexes hand-written answers the way a.ask does, so the join under
// test is the one the handler uses.
func answered(kind string, in map[string]recordCheck) map[string]recordCheck {
	out := map[string]recordCheck{}
	for name, r := range in {
		r.Name = name
		out[kind+"|"+name] = r
	}
	return out
}

func checksByDomain(t *testing.T, in []domainCheck) map[string]domainCheck {
	t.Helper()
	out := map[string]domainCheck{}
	for _, d := range in {
		out[d.Name] = d
	}
	return out
}

func TestDomainChecksSharedDelegationReachesBothRows(t *testing.T) {
	// One CNAME result covers the apex and its wildcard. Joining on a domain's
	// own name would leave the wildcard reporting a published record as absent.
	answers := answered("CNAME", map[string]recordCheck{
		"_acme-challenge.example.com":      {Status: "correct"},
		"_acme-challenge.shop.example.com": {Status: "missing"},
	})
	for k, v := range answered("CAA", map[string]recordCheck{"example.com": {Status: "not-published"}}) {
		answers[k] = v
	}
	got := checksByDomain(t, domainChecks(expectedDomains(), answers))

	for _, name := range []string{"example.com", "*.example.com"} {
		if s := got[name].Delegation.Status; s != "correct" {
			t.Errorf("%s: delegation = %q, want correct from the shared result", name, s)
		}
	}
	if s := got["shop.example.com"].Delegation.Status; s != "missing" {
		t.Errorf("shop: delegation = %q, want missing", s)
	}
	// Every domain reads the one CAA set, and its states stay on their own axis.
	for name, d := range got {
		if d.CAA == nil || d.CAA.Status != "not-published" {
			t.Errorf("%s: caa = %+v, want the shared not-published result", name, d.CAA)
		}
	}
}

func TestDomainChecksKeepMissingAndIncorrectApart(t *testing.T) {
	// The two states whose fixes have nothing in common (design D2), plus the
	// value the help needs: what was found, beside what was expected.
	got := checksByDomain(t, domainChecks(expectedDomains(), answered("CNAME", map[string]recordCheck{
		"_acme-challenge.example.com":      {Status: "incorrect", Found: "old-alias.svc.example.net."},
		"_acme-challenge.shop.example.com": {Status: "missing"},
	})))

	apex := got["example.com"].Delegation
	if apex.Status != "incorrect" || apex.Found != "old-alias.svc.example.net." {
		t.Errorf("apex delegation = %+v, want incorrect with what was found", apex)
	}
	if len(apex.Expected) != 1 || apex.Expected[0] != "example-com.svc.example.net" {
		t.Errorf("apex expected = %v, want this domain's own alias", apex.Expected)
	}
	if s := got["shop.example.com"].Delegation.Status; s != "missing" {
		t.Errorf("shop delegation = %q, want missing and not incorrect", s)
	}
}

func TestDomainChecksWithoutResolverReadNotChecked(t *testing.T) {
	// No resolver: no results, and every row says we did not look rather than
	// making a claim about the customer's DNS (design D4).
	out := domainChecks(expectedDomains(), nil)
	if len(out) != 3 {
		t.Fatalf("domains = %d, want 3", len(out))
	}
	for _, d := range out {
		if d.Delegation.Status != statusNotChecked {
			t.Errorf("%s: delegation = %q, want %q", d.Name, d.Delegation.Status, statusNotChecked)
		}
		if len(d.Delegation.Expected) != 1 {
			t.Errorf("%s: expected = %v, want the record to publish even unchecked", d.Name, d.Delegation.Expected)
		}
		if d.CAA == nil || d.CAA.Status != statusNotChecked {
			t.Errorf("%s: caa = %+v, want not-checked", d.Name, d.CAA)
		}
	}
}

func TestDomainChecksOmitCAAWhenNoneIsPlanned(t *testing.T) {
	// ACME_CAA_ISSUER unset: the plan emits no CAA row, so there is nothing to
	// authorize and nothing to say. Absent, not a status — "not-published"
	// would be a claim about the customer's zone we never made.
	for _, d := range domainChecks(withoutCAA(expectedDomains()), nil) {
		if d.CAA != nil {
			t.Errorf("%s: caa = %+v, want omitted", d.Name, d.CAA)
		}
	}
}

func TestRunChecksKeepsOrderAndHonoursCancellation(t *testing.T) {
	// The checks run in parallel now, so one unanswerable name cannot hold up
	// every other domain's row for a full timeout — but `results` is joined by
	// position with what was asked, so parallel must not mean reordered.
	//
	// Cancellation is the other half: the check runs on page load, so a
	// customer who navigates away must not leave a resolver being waited on.
	// A context that is already cancelled proves the lookups see it — without
	// it this test would sit here for the timeout, once per question.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := &resolvercheck.Checker{Resolver: "192.0.2.1:53"} // TEST-NET-1: never answers
	var checks []planCheck
	for _, name := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		checks = append(checks, planCheck{
			name: name, kind: "CNAME", expected: []string{"target.example.net"},
			cname: &resolvercheck.CNAMECheck{Name: name, Expected: "target.example.net."},
		})
	}

	done := make(chan []recordCheck, 1)
	go func() { done <- runChecks(ctx, checker, checks) }()
	select {
	case got := <-done:
		for i, r := range got {
			if r.Name != checks[i].name {
				t.Errorf("result %d is for %q, want %q — the answers moved", i, r.Name, checks[i].name)
			}
			if r.Status != string(resolvercheck.StatusUndetermined) {
				t.Errorf("%s: status = %q, want undetermined for a lookup that never ran", r.Name, r.Status)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runChecks ignored the cancelled context and waited on the resolver")
	}
}
