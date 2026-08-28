package inventory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", for schema setup only
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

const testDatabaseURLEnv = "TEST_DATABASE_URL"

// migratedDB gives each test its own migrated schema, dropped afterwards.
// ponytail: a near-copy of the helper in internal/store, which lives in a
// _test.go file and so cannot be imported. Upgrade path if a third package
// needs it: an internal/storetest package.
func migratedDB(t *testing.T) *gorm.DB {
	t.Helper()
	base := os.Getenv(testDatabaseURLEnv)
	if base == "" {
		t.Skipf("skipping: %s is not set — this test needs a live PostgreSQL, e.g. "+
			`%s="postgres://user:pass@localhost:5432/acmeservice_test?sslmode=disable"`,
			testDatabaseURLEnv, testDatabaseURLEnv)
	}

	var buf [6]byte
	rand.Read(buf[:])
	name := "test_" + hex.EncodeToString(buf[:])

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open %s: %v", testDatabaseURLEnv, err)
	}
	defer admin.Close()
	if _, err := admin.Exec("create schema " + name); err != nil {
		t.Fatalf("create schema %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", base)
		if err != nil {
			t.Logf("drop schema %s: %v", name, err)
			return
		}
		defer cleanup.Close()
		if _, err := cleanup.Exec("drop schema " + name + " cascade"); err != nil {
			t.Logf("drop schema %s: %v", name, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", testDatabaseURLEnv, err)
	}
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	dsn := u.String()

	if err := store.MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := store.Open(store.Options{DatabaseURL: dsn, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// scopedDB is migratedDB plus the scope every call now takes.
//
// The account is CREATED, through the production path, rather than resolved out
// of a migration seed: design C10 deleted store.ScopeForBootstrap along with the
// single-account edition it served. The Valid check is not ceremony — row-level
// security is on, and a zero Scope reads no rows and raises no error, so a
// fixture that quietly produced one would make a whole file pass against empty
// result sets.
func scopedDB(t *testing.T) (*gorm.DB, store.Scope) {
	t.Helper()
	db := migratedDB(t)
	ctx := context.Background()
	owner, err := store.FindOrCreateUser(ctx, db, "inventory-fixture-owner", "Fixture Owner", "")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, "inventory-fixture", "Fixture", owner.ID, 25)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	sc, err := store.ScopeForSelection(ctx, db, acc.ID)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if !sc.Valid() {
		t.Fatal("fixture scope is the zero Scope; every read below would silently match nothing")
	}
	return db, sc
}

// testSubject is the authority the service-layer tests act with: unrestricted
// within the account under test. What each function does with a NARROWER
// subject is the evaluator's business and is tested in internal/authz; what
// these tests are about is the operations themselves.
func testSubject(sc store.Scope) authz.Subject {
	return authz.CLISubject(sc.AccountID())
}

// projectFixture creates a project with one dual-stack node.
func projectFixture(t *testing.T, db *gorm.DB, sc store.Scope, slug string) model.ID {
	t.Helper()
	ctx := context.Background()
	p, err := CreateProject(ctx, db, sc, testSubject(sc), Limits{}, slug, "Project "+slug)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := AddNode(ctx, db, sc, testSubject(sc), p.ID, "a", "203.0.113.10", "2001:db8:1::10"); err != nil {
		t.Fatalf("add node: %v", err)
	}
	return p.ID
}

func TestIntegrationWildcardAndApexShareOneAlias(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "acme")

	apex, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "Example.COM.", derived, false)
	if err != nil {
		t.Fatalf("create example.com: %v", err)
	}
	if apex.Domain.Name != "example.com" {
		t.Errorf("name stored as %q, want normalized example.com", apex.Domain.Name)
	}
	if !apex.AliasCreated || apex.Alias.Label != "example-com" {
		t.Errorf("alias = %+v, want a freshly created example-com", apex.Alias)
	}

	wild, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "*.example.com", derived, false)
	if err != nil {
		t.Fatalf("create *.example.com: %v", err)
	}
	if wild.AliasCreated || wild.Alias.ID != apex.Alias.ID {
		t.Fatalf("wildcard got alias %+v, want the apex's alias %d", wild.Alias, apex.Alias.ID)
	}

	aliases, err := store.ListAliasesForProject(ctx, db, sc, projectID)
	if err != nil || len(aliases) != 1 {
		t.Fatalf("aliases = %v (err %v), want exactly one", aliases, err)
	}

	es, err := Expectations(ctx, db, sc, testSubject(sc), projectID, PlanOptions{Zone: "z.example.net.", CAAIssuers: []string{"letsencrypt.org"}})
	if err != nil {
		t.Fatalf("expectations: %v", err)
	}
	if len(es) != 2 {
		t.Fatalf("expectations = %d, want one per domain", len(es))
	}
	// One delegation between the two, said twice — which is what lets a caller
	// deduplicate the lookup and what stops one row calling the other's record
	// absent.
	if es[0].ChallengeName != es[1].ChallengeName || es[0].Target != es[1].Target {
		t.Errorf("apex and wildcard do not share a delegation: %+v / %+v", es[0], es[1])
	}
	if es[0].Target != "example-com.z.example.net" {
		t.Errorf("target = %q", es[0].Target)
	}
	for _, e := range es {
		if len(e.ExpectedCAA) != 2 {
			t.Errorf("%s: expected CAA = %q, want an issue and an issuewild row", e.Name, e.ExpectedCAA)
		}
	}

	// Removing one of the two leaves the shared alias in place.
	removed, err := DeleteDomain(ctx, db, sc, testSubject(sc), wild.Domain.ID)
	if err != nil {
		t.Fatalf("delete wildcard: %v", err)
	}
	if removed {
		t.Error("alias removed while another domain still references it")
	}
	if aliases, _ := store.ListAliasesForProject(ctx, db, sc, projectID); len(aliases) != 1 {
		t.Errorf("aliases after partial removal = %v, want one", aliases)
	}

	// Removing the last one takes the alias with it.
	removed, err = DeleteDomain(ctx, db, sc, testSubject(sc), apex.Domain.ID)
	if err != nil {
		t.Fatalf("delete apex: %v", err)
	}
	if !removed {
		t.Error("last domain removed but alias survived")
	}
	if aliases, _ := store.ListAliasesForProject(ctx, db, sc, projectID); len(aliases) != 0 {
		t.Errorf("aliases after full removal = %v, want none", aliases)
	}
}

func TestIntegrationCrossProjectAliasConflictIsRefused(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	prodID := projectFixture(t, db, sc, "one")
	otherID := projectFixture(t, db, sc, "two")

	if _, err := CreateDomain(ctx, db, sc, testSubject(sc), prodID, "example.com", derived, false); err != nil {
		t.Fatalf("create example.com: %v", err)
	}
	_, err := CreateDomain(ctx, db, sc, testSubject(sc), otherID, "*.example.com", derived, false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), `"one"`) {
		t.Errorf("error %q does not name the project that owns the challenge name", err)
	}
}

func TestIntegrationDuplicateAndLabelRules(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "acme")

	if _, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "example.com", derived, false); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "example.com", derived, false); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate domain: err = %v, want ErrConflict", err)
	}
	if _, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "*.*.example.net", derived, false); !errors.Is(err, dnsname.ErrInvalid) {
		t.Errorf("bad domain: err = %v, want ErrInvalid", err)
	}

	// A colliding derived label is disambiguated with a numeric suffix.
	// example.com on this load balancer took "example-com"; a *different* challenge name
	// suggesting the same label must not collide with it.
	if err := db.Exec(`update aliases set challenge_fqdn = '_acme-challenge.example.com.test' where label = 'example-com'`).Error; err != nil {
		t.Fatalf("nudge alias: %v", err)
	}
	if err := db.Exec(`update domains set name = 'example.com.test' where name = 'example.com'`).Error; err != nil {
		t.Fatalf("nudge domain: %v", err)
	}
	c, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "example.com.", derived, false)
	if err != nil {
		t.Fatalf("create after label collision: %v", err)
	}
	if c.Alias.Label != "example-com-2" {
		t.Errorf("label = %q, want example-com-2", c.Alias.Label)
	}
}

func TestIntegrationRedundantHostWarnsButIsCreated(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "acme")

	if _, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "*.example.com", derived, false); err != nil {
		t.Fatalf("create wildcard: %v", err)
	}
	c, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "shop.example.com", derived, false)
	if err != nil {
		t.Fatalf("create covered host: %v — it must be permitted, only warned about", err)
	}
	if len(c.Warnings) != 1 || !strings.Contains(string(c.Warnings[0]), "*.example.com") {
		t.Errorf("warnings = %v, want one naming the covering wildcard", c.Warnings)
	}
}

func TestIntegrationNonEmptyParentIsNotDeleted(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "acme")
	if _, err := CreateDomain(ctx, db, sc, testSubject(sc), projectID, "example.com", derived, false); err != nil {
		t.Fatal(err)
	}

	if err := DeleteProject(ctx, db, sc, testSubject(sc), projectID); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("delete project with a domain: err = %v, want ErrNotEmpty", err)
	}
	if _, err := store.GetProject(ctx, db, sc, projectID); err != nil {
		t.Error("refused deletion still removed something")
	}
}

func TestIntegrationNodeAddressValidation(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "acme")

	for _, tc := range []struct{ v4, v6 string }{
		{"2001:db8::1", ""},   // v6 in the v4 column
		{"", "203.0.113.10"},  // v4 in the v6 column
		{"", ""},              // a node with no address at all
		{"203.0.113.999", ""}, // not an address
	} {
		if _, err := AddNode(ctx, db, sc, testSubject(sc), projectID, "b", tc.v4, tc.v6); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddNode(%q, %q): err = %v, want ErrInvalid", tc.v4, tc.v6, err)
		}
	}
	if _, err := AddNode(ctx, db, sc, testSubject(sc), projectID, "b", "203.0.113.11", ""); err != nil {
		t.Errorf("v4-only node: %v", err)
	}
}

// The same two rules on the UPDATE path, which TestIntegrationNodeAddressValidation
// only covers for AddNode. The update path is where "and the node is unchanged
// afterwards" is answerable at all: a create that is refused leaves nothing
// behind by definition, while a refused renumbering must leave the node still
// reachable at the addresses it had.
func TestIntegrationUpdateNodeAddressValidation(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	projectID := projectFixture(t, db, sc, "acme")

	node, err := AddNode(ctx, db, sc, testSubject(sc), projectID, "c", "203.0.113.10", "2001:db8:1::10")
	if err != nil {
		t.Fatalf("add node: %v", err)
	}

	for _, tc := range []struct {
		name, v4, v6 string
	}{
		{"both addresses cleared", "", ""},
		{"an IPv6 literal in the IPv4 field", "2001:db8::1", ""},
		{"an IPv4 literal in the IPv6 field", "", "203.0.113.10"},
		{"not an address at all", "203.0.113.999", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := UpdateNodeAddresses(ctx, db, sc, testSubject(sc), node.ID, tc.v4, tc.v6); !errors.Is(err, ErrInvalid) {
				t.Fatalf("UpdateNodeAddresses(%q, %q): err = %v, want ErrInvalid", tc.v4, tc.v6, err)
			}
			// The refusal must not have been a partial write: the node still
			// answers at both of the addresses it was created with, or the DNS
			// plan silently loses a record on a rejected edit.
			var got model.LBNode
			if err := db.WithContext(ctx).First(&got, node.ID).Error; err != nil {
				t.Fatalf("reload node: %v", err)
			}
			if got.IPv4 == nil || *got.IPv4 != "203.0.113.10" || got.IPv6 == nil || *got.IPv6 != "2001:db8:1::10" {
				t.Fatalf("after a refused update the node reads %v/%v, want it unchanged", got.IPv4, got.IPv6)
			}
		})
	}

	// A valid renumbering still goes through, and dropping ONE family is
	// allowed — a node may legitimately become v4-only.
	updated, err := UpdateNodeAddresses(ctx, db, sc, testSubject(sc), node.ID, "203.0.113.20", "")
	if err != nil {
		t.Fatalf("valid renumbering: %v", err)
	}
	if updated.IPv4 == nil || *updated.IPv4 != "203.0.113.20" || updated.IPv6 != nil {
		t.Fatalf("after renumbering the node reads %v/%v, want 203.0.113.20 and no v6", updated.IPv4, updated.IPv6)
	}
}

// TestIntegrationAnotherTenantsDomainIsRefusedWithoutNamingThem is the
// isolation break that opened the moment every user got a personal account.
//
// aliases.challenge_fqdn is unique across the deployment while domains are
// unique per account, so two accounts asking for example.com is a real conflict:
// the challenge name is answered by a CNAME into one zone, and it has one
// target. What must NOT happen is the second asker learning who the first was.
// CreateDomain resolved the alias cross-account and put the other tenant's load
// balancer in the refusal — telling account B both that account A exists and
// that it holds example.com. Unreachable while a deployment had one account,
// which is why it survived this long.
func TestIntegrationAnotherTenantsDomainIsRefusedWithoutNamingThem(t *testing.T) {
	db, first := scopedDB(t)
	ctx := context.Background()
	firstLB := projectFixture(t, db, first, "acme")
	if _, err := CreateDomain(ctx, db, first, testSubject(first), firstLB, "example.com", derived, false); err != nil {
		t.Fatalf("the first account claims example.com: %v", err)
	}

	owner, err := store.FindOrCreateUser(ctx, db, "second-tenant-owner", "Second", "")
	if err != nil {
		t.Fatalf("create the second owner: %v", err)
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, "second-tenant", "Second", owner.ID, 25)
	if err != nil {
		t.Fatalf("create the second account: %v", err)
	}
	second, err := store.ScopeForSelection(ctx, db, acc.ID)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	secondLB := projectFixture(t, db, second, "other")

	_, err = CreateDomain(ctx, db, second, testSubject(second), secondLB, "example.com", derived, false)
	if err == nil {
		t.Fatal("the second account was allowed to claim a challenge name the first already holds")
	}
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("refused with %v, want a conflict", err)
	}
	// Nothing about the other tenant: not its slug, not its project, not its
	// load balancer, and not the phrase that names one.
	for _, leak := range []string{"acme-prod", "inventory-fixture", firstLB.String(), "load balancer"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("the refusal %q names another account's inventory (%q)", err, leak)
		}
	}

	// And the same refusal inside ONE account still names the load balancer,
	// because there it is the whole value of the message: it is where the
	// operator goes to fix it. The name is the wildcard rather than the apex, so
	// that it is the ALIAS conflict that fires — `*.example.com` is a different
	// domain deriving the same challenge name, which is the case this branch is
	// about; the apex would be caught one check earlier as a duplicate domain.
	sibling := projectFixture(t, db, first, "sibling")
	_, err = CreateDomain(ctx, db, first, testSubject(first), sibling, "*.example.com", derived, false)
	if err == nil || !strings.Contains(err.Error(), "already owned by") {
		t.Fatalf("within one account the conflict was reported as %v; it must say which load balancer holds it", err)
	}
}

// TestIntegrationAProjectSlugIsUniquePerAccountAndNotGlobally covers
// collapse-project-and-loadbalancer task 1.5.
//
// Global uniqueness was inherited rather than chosen — it predates accounts
// existing — and it leaks across tenants in exactly the way the rest of this
// schema spends a row-level-security policy preventing: one customer calling a
// project `prod` stopped every other customer from doing so, over a name they
// could not see, could not discover, and could only work around by picking a
// second choice.
//
// Both halves are asserted, because only together do they say "per account":
// two accounts may each hold `prod`, and one account may not hold it twice.
func TestIntegrationAProjectSlugIsUniquePerAccountAndNotGlobally(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	if _, err := CreateProject(ctx, db, sc, testSubject(sc), Limits{}, "prod", "Production"); err != nil {
		t.Fatalf("create the first prod: %v", err)
	}

	// The same name again, in the SAME account, is still refused: within one
	// customer's inventory a slug names one thing.
	if _, err := CreateProject(ctx, db, sc, testSubject(sc), Limits{}, "prod", "Production Again"); err == nil {
		t.Error("a second `prod` in the same account was accepted, want a refusal — " +
			"within an account a slug names one project")
	}

	// The same name in ANOTHER account must be accepted. This is the half that
	// was broken, and it fails loudly against the old global constraint.
	other, err := store.FindOrCreateUser(ctx, db, "inventory-second-owner", "Second Owner", "")
	if err != nil {
		t.Fatalf("create second owner: %v", err)
	}
	acc2, err := store.CreateAccountWithOwner(ctx, db, "inventory-second", "Second", other.ID, 25)
	if err != nil {
		t.Fatalf("create second account: %v", err)
	}
	sc2, err := store.ScopeForSelection(ctx, db, acc2.ID)
	if err != nil {
		t.Fatalf("scope for the second account: %v", err)
	}
	if _, err := CreateProject(ctx, db, sc2, testSubject(sc2), Limits{}, "prod", "Production"); err != nil {
		t.Fatalf("a second account could not call its project `prod`: %v — "+
			"a slug is unique per account, and one customer must not consume a "+
			"name out of every other customer's namespace", err)
	}
}

// TestAProjectSlugCannotContainADot pins what makes a dotted composite such as
// `project.lb` parseable: the separator can never occur inside either half, so
// it needs no escaping and no longest-match rule.
//
// It is a property of the slug being validated as a DNS LABEL rather than a
// rule written for this purpose, which is why it is asserted here — a change to
// that validation would silently take the guarantee away.
func TestAProjectSlugCannotContainADot(t *testing.T) {
	if err := dnsname.ValidateLabel("project.lb"); err == nil {
		t.Error("`project.lb` was accepted as a slug, want a refusal — a slug is one " +
			"DNS label, and a dot inside one makes a dotted composite ambiguous")
	}
	if err := dnsname.ValidateLabel("prod"); err != nil {
		t.Errorf("`prod` was refused as a slug: %v", err)
	}
}
