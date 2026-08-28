package inventory

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The two values of CreateDomain's useDomainLabel parameter, i.e. of
// ACME_ALIAS_USE_DOMAIN, named so a call site says which mode it exercises.
const (
	derived   = true  // ACME_ALIAS_USE_DOMAIN=true: `demo-example-com`
	generated = false // the default: 16 random base32 characters
)

// TestIntegrationGeneratedLabelIsNotDerivableFromTheDomain covers the shipped
// default against a real database. The dev stack and the acceptance suite run
// that same default end to end — development/scripts/seed.sh generates the
// fake internet's delegation from whichever label the service picked — but
// they can only observe the label that was chosen, not that it is unguessable
// from the domain, which is what this asserts.
func TestIntegrationGeneratedLabelIsNotDerivableFromTheDomain(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	oneID := projectFixture(t, db, sc, "one")
	twoID := projectFixture(t, db, sc, "two")

	first, err := CreateDomain(ctx, db, sc, testSubject(sc), oneID, "demo.example.com", generated, false)
	if err != nil {
		t.Fatalf("create demo.example.com: %v", err)
	}
	label := first.Alias.Label
	if err := dnsname.ValidateLabel(label); err != nil {
		t.Errorf("generated label %q is not a valid DNS label: %v", label, err)
	}
	if label == dnsname.SuggestLabel(first.Alias.ChallengeFQDN) {
		t.Errorf("label %q is the label derived from the domain; it must not be computable from it", label)
	}
	for _, part := range []string{"demo", "example", "com"} {
		if strings.Contains(label, part) {
			t.Errorf("label %q contains %q from the domain name", label, part)
		}
	}

	// Same name, different load balancer: two draws from the same source, so
	// two different labels. A hash of the challenge name would produce one.
	second, err := CreateDomain(ctx, db, sc, testSubject(sc), twoID, "demo.example.net", generated, false)
	if err != nil {
		t.Fatalf("create demo.example.net: %v", err)
	}
	if second.Alias.Label == label {
		t.Errorf("both aliases are labelled %q; a generated label must be drawn fresh each time", label)
	}
}

// TestIntegrationExistingAliasKeepsItsLabelWhenTheSettingChanges is design A3:
// the setting decides how the *next* alias is labelled and never touches one
// that exists — the property that makes the change safe to deploy, because the
// label is half of a CNAME published in someone else's zone.
func TestIntegrationExistingAliasKeepsItsLabelWhenTheSettingChanges(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	loadBalancerID := projectFixture(t, db, sc, "acme")

	apex, err := CreateDomain(ctx, db, sc, testSubject(sc), loadBalancerID, "demo.example.com", derived, false)
	if err != nil {
		t.Fatalf("create demo.example.com: %v", err)
	}
	if apex.Alias.Label != "demo-example-com" {
		t.Fatalf("label = %q, want demo-example-com", apex.Alias.Label)
	}

	// The setting flips to the default; the wildcard shares the apex's
	// challenge name, so it must reuse the alias exactly as it stands.
	wild, err := CreateDomain(ctx, db, sc, testSubject(sc), loadBalancerID, "*.demo.example.com", generated, false)
	if err != nil {
		t.Fatalf("create *.demo.example.com: %v", err)
	}
	if wild.AliasCreated || wild.Alias.ID != apex.Alias.ID {
		t.Fatalf("wildcard got alias %+v, want the apex's alias %d", wild.Alias, apex.Alias.ID)
	}
	if wild.Alias.Label != "demo-example-com" {
		t.Errorf("label = %q after the setting changed, want the published demo-example-com", wild.Alias.Label)
	}
	stored, err := store.GetAlias(ctx, db, sc, apex.Alias.ID)
	if err != nil {
		t.Fatalf("reload alias: %v", err)
	}
	if stored.Label != "demo-example-com" {
		t.Errorf("stored label = %q, want the published demo-example-com", stored.Label)
	}

	// A new challenge name under the new setting does get a generated label.
	other, err := CreateDomain(ctx, db, sc, testSubject(sc), loadBalancerID, "shop.example.com", generated, false)
	if err != nil {
		t.Fatalf("create shop.example.com: %v", err)
	}
	if other.Alias.Label == "shop-example-com" {
		t.Errorf("label = %q, want a generated one now the setting is off", other.Alias.Label)
	}
}

// The two values of CreateDomain's includeCounterpart parameter, named so a
// call site says what it is asking for.
const (
	withCounterpart = true
	nameOnly        = false
)

// names lists a creation's domains in creation order.
func names(c *DomainCreation) []string {
	out := make([]string, 0, len(c.Domains))
	for _, d := range c.Domains {
		out = append(out, d.Name)
	}
	return out
}

// domainNames lists what the project actually holds, which is what
// "neither domain exists" has to be checked against.
func domainNames(t *testing.T, db *gorm.DB, sc store.Scope, projectID model.ID) []string {
	t.Helper()
	rows, err := store.ListDomainsForProject(context.Background(), db, sc, projectID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, d := range rows {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

// TestIntegrationCounterpartCreatesThePair is design B1 and B2: one action,
// two domain rows, one alias, one challenge name — and the mirror, so an
// operator who typed the wildcard first gets the apex rather than a second
// wildcard.
func TestIntegrationCounterpartCreatesThePair(t *testing.T) {
	for _, tc := range []struct {
		submitted string
		want      []string
	}{
		{"example.com", []string{"example.com", "*.example.com"}},
		{"*.example.com", []string{"*.example.com", "example.com"}},
		{"shop.example.com", []string{"shop.example.com", "*.shop.example.com"}},
	} {
		t.Run(tc.submitted, func(t *testing.T) {
			db, sc := scopedDB(t)
			ctx := context.Background()
			projectID := projectFixture(t, db, sc, "pair")

			c, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, tc.submitted, generated, withCounterpart)
			if err != nil {
				t.Fatalf("create %q with its counterpart: %v", tc.submitted, err)
			}
			if got := names(c); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("created %v, want %v", got, tc.want)
			}
			if c.Domain != c.Domains[0] {
				t.Errorf("Domain is %q, want the submitted name %q", c.Domain.Name, tc.want[0])
			}
			// One alias, referenced by both rows, for the one challenge name.
			for _, d := range c.Domains {
				if d.AliasID != c.Alias.ID {
					t.Errorf("domain %q points at alias %d, want the shared %d", d.Name, d.AliasID, c.Alias.ID)
				}
				if got := dnsname.ChallengeName(d.Name); got != c.Alias.ChallengeFQDN {
					t.Errorf("challenge name of %q is %q, want %q", d.Name, got, c.Alias.ChallengeFQDN)
				}
			}
			aliases, err := store.ListAliasesForProject(ctx, db, sc, projectID)
			if err != nil {
				t.Fatalf("list aliases: %v", err)
			}
			if len(aliases) != 1 {
				t.Errorf("%d aliases exist, want exactly 1 shared by the pair", len(aliases))
			}
			// The pair must not warn about itself: `*.x` does not "cover" `x`.
			if len(c.Warnings) != 0 {
				t.Errorf("warnings = %v, want none for a freshly created pair", c.Warnings)
			}
		})
	}
}

// TestIntegrationCounterpartIsOptional is decision B4 at the service layer:
// absent flag, one row, exactly today's behaviour.
func TestIntegrationCounterpartIsOptional(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "single")

	c, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "example.com", generated, nameOnly)
	if err != nil {
		t.Fatalf("create example.com: %v", err)
	}
	if got := names(c); !reflect.DeepEqual(got, []string{"example.com"}) {
		t.Fatalf("created %v, want only example.com", got)
	}
	if got := domainNames(t, db, sc, projectID); !reflect.DeepEqual(got, []string{"example.com"}) {
		t.Errorf("inventory holds %v, want only example.com", got)
	}
}

// TestIntegrationRefusedCounterpartCreatesNothing is decision B3. `co.uk` is a
// perfectly valid plain name; `*.co.uk` is refused at the public suffix. One
// form field, one valid name, one invalid one — and the whole request is
// refused rather than half-applied.
func TestIntegrationRefusedCounterpartCreatesNothing(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "suffix")

	// The premise: the submitted name is fine on its own.
	if _, _, err := dnsname.ValidateDomain("co.uk"); err != nil {
		t.Fatalf("co.uk should be a valid plain name: %v", err)
	}

	_, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "co.uk", generated, withCounterpart)
	if !errors.Is(err, dnsname.ErrInvalid) {
		t.Fatalf("error = %v, want a validation error", err)
	}
	if !strings.Contains(err.Error(), "*.co.uk") {
		t.Errorf("error %q does not name the refused counterpart *.co.uk", err)
	}
	if got := domainNames(t, db, sc, projectID); len(got) != 0 {
		t.Errorf("inventory holds %v, want nothing created", got)
	}
}

// TestIntegrationConflictingCounterpartCreatesNothing is the other half of
// B3: a conflict on the counterpart refuses the whole request, whether the
// conflict is a name this load balancer already has or an alias another load
// balancer owns.
func TestIntegrationConflictingCounterpartCreatesNothing(t *testing.T) {
	t.Run("counterpart already exists here", func(t *testing.T) {
		db, sc := scopedDB(t)
		ctx := context.Background()
		projectID := projectFixture(t, db, sc, "dup")

		if _, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "*.example.com", generated, nameOnly); err != nil {
			t.Fatalf("seed *.example.com: %v", err)
		}
		_, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "example.com", generated, withCounterpart)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("error = %v, want a conflict", err)
		}
		if got := domainNames(t, db, sc, projectID); !reflect.DeepEqual(got, []string{"*.example.com"}) {
			t.Errorf("inventory holds %v, want only the seeded wildcard — example.com must not have been created", got)
		}
	})

	t.Run("counterpart's alias belongs to another load balancer", func(t *testing.T) {
		db, sc := scopedDB(t)
		ctx := context.Background()
		mine := projectFixture(t, db, sc, "mine")
		theirs := projectFixture(t, db, sc, "theirs")

		if _, err := CreateDomain(ctx, db, sc, testSubject(sc), theirs, "*.example.com", generated, nameOnly); err != nil {
			t.Fatalf("seed *.example.com on the other load balancer: %v", err)
		}
		_, err := CreateDomain(ctx, db, sc, testSubject(sc), mine, "example.com", generated, withCounterpart)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("error = %v, want a conflict", err)
		}
		if !strings.Contains(err.Error(), "theirs") {
			t.Errorf("error %q does not name the owning load balancer", err)
		}
		if got := domainNames(t, db, sc, mine); len(got) != 0 {
			t.Errorf("inventory holds %v, want nothing created", got)
		}
	})
}

// TestObservationHost is the service half of task 2.3: the covering test, and
// the refusal a person reads. It needs no database — the rules are about the
// domain's own name and the hostname submitted with it — which is why the
// validation is a function of its own rather than inline in UpdateDomain.
func TestObservationHost(t *testing.T) {
	host := func(s string) *string { return &s }
	wild := model.Domain{Name: "*.example.com"}
	stored := model.Domain{Name: "*.example.com", TLSTestHost: host("shop.example.com")}
	apex := model.Domain{Name: "example.com"}

	for _, tc := range []struct {
		what     string
		domain   model.Domain
		observe  bool
		testHost *string
		want     string // the stored hostname after this write, "" for "leave it alone"
		wantErr  bool
	}{
		{"a plain domain is asked for by its own name", apex, true, nil, "", false},
		{"and cannot carry a test hostname at all", apex, true, host("www.example.com"), "", true},

		{"a wildcard cannot be observed without one", wild, true, nil, "", true},
		{"but may sit switched off without one", wild, false, nil, "", false},
		{"one label beneath is covered", wild, true, host("www.example.com"), "www.example.com", false},
		{"and is normalized on the way in", wild, true, host("WWW.Example.Com."), "www.example.com", false},
		{"two labels beneath are not", wild, true, host("a.b.example.com"), "", true},
		{"the apex is not covered by its own wildcard", wild, true, host("example.com"), "", true},
		{"nor is another wildcard", wild, true, host("*.example.com"), "", true},
		{"an unrelated name is not covered", wild, true, host("www.example.net"), "", true},
		{"an empty hostname is refused, not stored", wild, true, host(""), "", true},

		// Refused even with the switch off: storing it would leave a row that
		// turning the switch on later could never observe.
		{"a hostname outside the wildcard is refused even when off", wild, false, host("a.b.example.com"), "", true},

		// Task 2.5, from this side: nothing is sent, the stored hostname is what
		// the switch is turned on against, and it is not rewritten.
		{"a stored hostname satisfies the switch on its own", stored, true, nil, "", false},
	} {
		got, err := observationHost(tc.domain, tc.observe, tc.testHost)
		switch {
		case tc.wantErr && err == nil:
			t.Errorf("%s: want a refusal, got %v", tc.what, got)
		case tc.wantErr && !errors.Is(err, ErrInvalid):
			t.Errorf("%s: refusal is not ErrInvalid, so it will not map to 400: %v", tc.what, err)
		case !tc.wantErr && err != nil:
			t.Errorf("%s: %v", tc.what, err)
		case !tc.wantErr && tc.want == "" && got != nil:
			t.Errorf("%s: want the stored hostname left alone, got %q", tc.what, *got)
		case !tc.wantErr && tc.want != "" && (got == nil || *got != tc.want):
			t.Errorf("%s: hostname = %v, want %q", tc.what, got, tc.want)
		}
	}
}
