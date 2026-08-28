package inventory

import (
	"reflect"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// fixture: three domains, two challenge names (the wildcard and the apex
// share one), one registrable domain — design D5's worked example.
func fixture() (*model.Project, []model.Domain, []model.Alias) {
	// Identifiers are minted rather than written as small integers: the
	// expectations are built from the graph these values describe, so what
	// matters is that the same value appears on both ends of each edge, not
	// what the value is.
	projectID := model.NewID()
	apexAlias, shopAlias := model.NewID(), model.NewID()
	project := &model.Project{ID: projectID, Slug: "acme-shop", Name: "Acme Shop"}
	aliases := []model.Alias{
		{ID: apexAlias, ProjectID: projectID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"},
		{ID: shopAlias, ProjectID: projectID, ChallengeFQDN: "_acme-challenge.shop.example.com", Label: "shop-example-com"},
	}
	domains := []model.Domain{
		{ID: model.NewID(), ProjectID: projectID, AliasID: apexAlias, Name: "*.example.com"},
		{ID: model.NewID(), ProjectID: projectID, AliasID: apexAlias, Name: "example.com"},
		{ID: model.NewID(), ProjectID: projectID, AliasID: shopAlias, Name: "shop.example.com"},
	}
	return project, domains, aliases
}

func byName(es []DomainExpectation) map[string]DomainExpectation {
	m := map[string]DomainExpectation{}
	for _, e := range es {
		m[e.Name] = e
	}
	return m
}

func TestExpectationsShareOneDelegation(t *testing.T) {
	// The join the per-domain verification depends on: the apex and its
	// wildcard must name the same delegation, or one of the two rows reports a
	// record the other one already covers as absent (design D1). This is what
	// lets a caller deduplicate lookups by challenge name now that there is no
	// deduplicated record set to read them off.
	_, domains, aliases := fixture()
	es := buildExpectations(domains, aliases, PlanOptions{
		Zone: "acmeservice.example.net.", CAAIssuers: []string{"letsencrypt.org"},
	})

	if len(es) != len(domains) {
		t.Fatalf("expectations = %d, want one per domain (%d)", len(es), len(domains))
	}
	m := byName(es)
	apex, wild := m["example.com"], m["*.example.com"]
	if apex.ChallengeName != wild.ChallengeName || apex.Target != wild.Target || apex.AliasID != wild.AliasID {
		t.Errorf("apex %+v and wildcard %+v do not share a delegation", apex, wild)
	}
	if apex.ChallengeName != "_acme-challenge.example.com" {
		t.Errorf("challenge name = %q, want the alias's own spelling", apex.ChallengeName)
	}
	if apex.Target != "example-com.acmeservice.example.net" {
		t.Errorf("target = %q", apex.Target)
	}
	if shop := m["shop.example.com"]; shop.ChallengeName == apex.ChallengeName {
		t.Errorf("an unrelated domain shares the apex delegation: %+v", shop)
	}

	// Every domain reads the one CAA set at the registrable domain, and reads
	// it identically — three hosts under example.com are two lookups' worth of
	// distinct values, not six.
	want := []string{`0 issue "letsencrypt.org"`, `0 issuewild "letsencrypt.org"`}
	for _, e := range es {
		if e.Registrable != "example.com" {
			t.Errorf("%s: registrable = %q, want example.com", e.Name, e.Registrable)
		}
		if !reflect.DeepEqual(e.ExpectedCAA, want) {
			t.Errorf("%s: expected CAA = %q, want %q", e.Name, e.ExpectedCAA, want)
		}
	}
}

func TestExpectationsDomainWithoutAliasStillAppears(t *testing.T) {
	// A domain whose alias has gone missing keeps its row and asks for
	// nothing, rather than dropping out of the verification silently.
	_, domains, _ := fixture()
	es := buildExpectations(domains, nil, PlanOptions{Zone: "z.example.net"})
	if len(es) != len(domains) {
		t.Fatalf("expectations = %d, want %d", len(es), len(domains))
	}
	for _, e := range es {
		if e.Target != "" {
			t.Errorf("%s: target = %q, want empty with no alias", e.Name, e.Target)
		}
		if e.ChallengeName == "" {
			t.Errorf("%s: no challenge name — the row must still say where the record goes", e.Name)
		}
	}
}

func TestExpectedCAAIsNilWithoutAnIssuer(t *testing.T) {
	// nil and not an empty slice: it is what the console reads to hide the CAA
	// dot altogether, rather than showing a grey one for a check nobody asked
	// for.
	_, domains, aliases := fixture()
	for _, e := range buildExpectations(domains, aliases, PlanOptions{Zone: "z.example.net"}) {
		if e.ExpectedCAA != nil {
			t.Errorf("%s: expected CAA = %q with no ACME_CAA_ISSUER, want nil", e.Name, e.ExpectedCAA)
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
	// shared delegation above is meaningless without it.
	if a, b := dnsname.ChallengeName("example.com"), dnsname.ChallengeName("*.example.com"); a != b {
		t.Fatalf("%q != %q", a, b)
	}
}
