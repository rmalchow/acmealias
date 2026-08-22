package inventory

import (
	"strings"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func ptr(s string) *string { return &s }

// fixture: three domains, two challenge names (the wildcard and the apex
// share one), one registrable domain — design D5's worked example.
func fixture(nodes []model.LBNode) (*model.Project, *model.LoadBalancer, []model.Domain, []model.Alias) {
	// Identifiers are minted rather than written as small integers: the plan is
	// built from the graph these values describe, so what matters is that the
	// same value appears on both ends of each edge, not what the value is.
	projectID, lbID := model.NewID(), model.NewID()
	apexAlias, shopAlias := model.NewID(), model.NewID()
	project := &model.Project{ID: projectID, Slug: "acme-shop", Name: "Acme Shop"}
	lb := &model.LoadBalancer{ID: lbID, ProjectID: projectID, Slug: "eu-prod", Name: "EU production"}
	aliases := []model.Alias{
		{ID: apexAlias, LoadBalancerID: lbID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"},
		{ID: shopAlias, LoadBalancerID: lbID, ChallengeFQDN: "_acme-challenge.shop.example.com", Label: "shop-example-com"},
	}
	domains := []model.Domain{
		{ID: model.NewID(), LoadBalancerID: lbID, AliasID: apexAlias, Name: "*.example.com"},
		{ID: model.NewID(), LoadBalancerID: lbID, AliasID: apexAlias, Name: "example.com"},
		{ID: model.NewID(), LoadBalancerID: lbID, AliasID: shopAlias, Name: "shop.example.com"},
	}
	return project, lb, domains, aliases
}

func count(p *DNSPlan, typ string) int {
	n := 0
	for _, r := range p.Records {
		if r.Type == typ {
			n++
		}
	}
	return n
}

func TestPlanCNAMEPerDistinctChallengeName(t *testing.T) {
	project, lb, domains, aliases := fixture(nil)
	nodes := []model.LBNode{{Label: "a", IPv4: ptr("203.0.113.10")}}
	p := buildPlan(project, lb, nodes, domains, aliases, PlanOptions{Zone: "acmeservice.example.net."})

	if got := count(p, "CNAME"); got != 2 {
		t.Fatalf("three domains sharing two challenge names: want 2 CNAMEs, got %d (%v)", got, p.Records)
	}
	var found bool
	for _, r := range p.Records {
		if r.Type == "CNAME" && r.Name == "_acme-challenge.example.com" {
			found = true
			if r.Value != "example-com.acmeservice.example.net" {
				t.Errorf("CNAME target = %q", r.Value)
			}
		}
	}
	if !found {
		t.Error("no CNAME for the shared challenge name")
	}
}

func TestPlanAddressFamilies(t *testing.T) {
	project, lb, domains, aliases := fixture(nil)
	dual := []model.LBNode{
		{Label: "a", IPv4: ptr("203.0.113.10"), IPv6: ptr("2001:db8:1::10")},
		{Label: "b", IPv4: ptr("203.0.113.11"), IPv6: ptr("2001:db8:1::11")},
	}
	p := buildPlan(project, lb, dual, domains, aliases, PlanOptions{Zone: "z.example.net"})
	// three domains x two nodes, per family.
	if got := count(p, "A"); got != 6 {
		t.Errorf("A records = %d, want 6", got)
	}
	if got := count(p, "AAAA"); got != 6 {
		t.Errorf("AAAA records = %d, want 6", got)
	}

	mixed := []model.LBNode{
		{Label: "a", IPv4: ptr("203.0.113.10"), IPv6: ptr("2001:db8:1::10")},
		{Label: "b", IPv4: ptr("203.0.113.11")}, // v4 only
	}
	p = buildPlan(project, lb, mixed, domains, aliases, PlanOptions{Zone: "z.example.net"})
	if got := count(p, "A"); got != 6 {
		t.Errorf("A records = %d, want 6", got)
	}
	if got := count(p, "AAAA"); got != 3 {
		t.Errorf("AAAA records = %d, want 3 (node b has no IPv6)", got)
	}
	for _, r := range p.Records {
		if r.Type == "AAAA" && r.Value == "203.0.113.11" {
			t.Fatal("v4 address emitted as AAAA")
		}
	}
}

func TestPlanCAAPerRegistrableDomain(t *testing.T) {
	project, lb, domains, aliases := fixture(nil)
	nodes := []model.LBNode{{Label: "a", IPv4: ptr("203.0.113.10")}}

	p := buildPlan(project, lb, nodes, domains, aliases, PlanOptions{
		Zone: "z.example.net", CAAIssuers: []string{"letsencrypt.org"},
	})
	var caa []Record
	for _, r := range p.Records {
		if r.Type == "CAA" {
			caa = append(caa, r)
		}
	}
	if len(caa) != 2 {
		t.Fatalf("three hosts under one registrable domain: want 2 CAA rows, got %d: %v", len(caa), caa)
	}
	for _, r := range caa {
		if r.Name != "example.com" {
			t.Errorf("CAA at %q, want example.com — per-host CAA shadows the parent", r.Name)
		}
	}
	if caa[0].Value != `0 issue "letsencrypt.org"` || caa[1].Value != `0 issuewild "letsencrypt.org"` {
		t.Errorf("want both an issue and an issuewild row, got %q and %q", caa[0].Value, caa[1].Value)
	}

	// Two registrable domains, two sets.
	domains = append(domains, model.Domain{ID: model.NewID(), LoadBalancerID: lb.ID, AliasID: aliases[1].ID, Name: "www.example.org"})
	p = buildPlan(project, lb, nodes, domains, aliases, PlanOptions{
		Zone: "z.example.net", CAAIssuers: []string{"letsencrypt.org"},
	})
	if got := count(p, "CAA"); got != 4 {
		t.Errorf("two registrable domains: CAA rows = %d, want 4", got)
	}

	// Unset issuer: no rows at all, never a guess.
	p = buildPlan(project, lb, nodes, domains, aliases, PlanOptions{Zone: "z.example.net"})
	if got := count(p, "CAA"); got != 0 {
		t.Errorf("no ACME_CAA_ISSUER: CAA rows = %d, want 0", got)
	}
}

func TestRenderZoneSnippet(t *testing.T) {
	project, lb, domains, aliases := fixture(nil)
	nodes := []model.LBNode{{Label: "a", IPv4: ptr("203.0.113.10"), IPv6: ptr("2001:db8:1::10")}}
	out := RenderZoneSnippet(buildPlan(project, lb, nodes, domains, aliases, PlanOptions{
		Zone: "acmeservice.example.net.", CAAIssuers: []string{"letsencrypt.org"},
	}))

	if !strings.HasPrefix(out, "; DNS records for project acme-shop / load balancer eu-prod\n") {
		t.Errorf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "; nodes: a: 203.0.113.10 / 2001:db8:1::10") {
		t.Errorf("header does not name the nodes:\n%s", out)
	}
	for _, want := range []string{
		"*.example.com.",
		"_acme-challenge.example.com.",
		"example-com.acmeservice.example.net.",
		`IN  CAA    0 issuewild "letsencrypt.org"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendering lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasSuffix(f[0], ".") || f[2] != "IN" {
			t.Errorf("not zone-file syntax with a fully-qualified name: %q", line)
		}
	}
}

func TestCoverageWarnings(t *testing.T) {
	siblings := []model.Domain{{Name: "*.example.com"}, {Name: "example.com"}}
	if got := coverageWarnings("shop.example.com", siblings); len(got) != 1 {
		t.Errorf("shop.example.com under *.example.com: want 1 warning, got %v", got)
	}
	// A wildcard matches exactly one label, and does not match the apex.
	for _, name := range []string{"example.com", "a.b.example.com", "*.example.com", "shop.example.net"} {
		if got := coverageWarnings(name, siblings); len(got) != 0 {
			t.Errorf("%s: want no warning, got %v", name, got)
		}
	}
}

func TestChallengeNameSharedByWildcardAndApex(t *testing.T) {
	// The premise the alias model rests on, asserted here too because the
	// plan's CNAME collapse is meaningless without it.
	if a, b := dnsname.ChallengeName("example.com"), dnsname.ChallengeName("*.example.com"); a != b {
		t.Fatalf("%q != %q", a, b)
	}
}
