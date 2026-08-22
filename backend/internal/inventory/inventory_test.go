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

// scopedDB is migratedDB plus the scope every call now takes. The migration
// seeds one account and the bootstrap producer is the only honest way to reach
// it: a test able to mint a Scope from an identifier would be exercising a
// constructor production code does not have.
func scopedDB(t *testing.T) (*gorm.DB, store.Scope) {
	t.Helper()
	db := migratedDB(t)
	sc, err := store.ScopeForBootstrap(context.Background(), db)
	if err != nil {
		t.Fatalf("bootstrap scope: %v", err)
	}
	return db, sc
}

// loadBalancerFixture creates a project with one load balancer and one dual-stack node.
func loadBalancerFixture(t *testing.T, db *gorm.DB, sc store.Scope, slug string) model.ID {
	t.Helper()
	ctx := context.Background()
	p, err := CreateProject(ctx, db, sc, slug, "Project "+slug)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb, err := CreateLoadBalancer(ctx, db, sc, p.ID, slug+"-prod", "Production")
	if err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	if _, err := AddNode(ctx, db, sc, lb.ID, "a", "203.0.113.10", "2001:db8:1::10"); err != nil {
		t.Fatalf("add node: %v", err)
	}
	return lb.ID
}

func TestIntegrationWildcardAndApexShareOneAlias(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	loadBalancerID := loadBalancerFixture(t, db, sc, "acme")

	apex, err := CreateDomain(ctx, db, sc, loadBalancerID, "Example.COM.", derived, false)
	if err != nil {
		t.Fatalf("create example.com: %v", err)
	}
	if apex.Domain.Name != "example.com" {
		t.Errorf("name stored as %q, want normalized example.com", apex.Domain.Name)
	}
	if !apex.AliasCreated || apex.Alias.Label != "example-com" {
		t.Errorf("alias = %+v, want a freshly created example-com", apex.Alias)
	}

	wild, err := CreateDomain(ctx, db, sc, loadBalancerID, "*.example.com", derived, false)
	if err != nil {
		t.Fatalf("create *.example.com: %v", err)
	}
	if wild.AliasCreated || wild.Alias.ID != apex.Alias.ID {
		t.Fatalf("wildcard got alias %+v, want the apex's alias %d", wild.Alias, apex.Alias.ID)
	}

	aliases, err := store.ListAliasesForLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil || len(aliases) != 1 {
		t.Fatalf("aliases = %v (err %v), want exactly one", aliases, err)
	}

	plan, err := Plan(ctx, db, sc, loadBalancerID, PlanOptions{Zone: "z.example.net.", CAAIssuers: []string{"letsencrypt.org"}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := count(plan, "CNAME"); got != 1 {
		t.Errorf("CNAMEs = %d, want 1", got)
	}
	if got := count(plan, "A"); got != 2 {
		t.Errorf("A records = %d, want 2 (two domains, one node)", got)
	}
	if got := count(plan, "CAA"); got != 2 {
		t.Errorf("CAA rows = %d, want 2", got)
	}

	// Removing one of the two leaves the shared alias in place.
	removed, err := DeleteDomain(ctx, db, sc, wild.Domain.ID)
	if err != nil {
		t.Fatalf("delete wildcard: %v", err)
	}
	if removed {
		t.Error("alias removed while another domain still references it")
	}
	if aliases, _ := store.ListAliasesForLoadBalancer(ctx, db, sc, loadBalancerID); len(aliases) != 1 {
		t.Errorf("aliases after partial removal = %v, want one", aliases)
	}

	// Removing the last one takes the alias with it.
	removed, err = DeleteDomain(ctx, db, sc, apex.Domain.ID)
	if err != nil {
		t.Fatalf("delete apex: %v", err)
	}
	if !removed {
		t.Error("last domain removed but alias survived")
	}
	if aliases, _ := store.ListAliasesForLoadBalancer(ctx, db, sc, loadBalancerID); len(aliases) != 0 {
		t.Errorf("aliases after full removal = %v, want none", aliases)
	}
}

func TestIntegrationCrossLoadBalancerAliasConflictIsRefused(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	prodID := loadBalancerFixture(t, db, sc, "one")
	otherID := loadBalancerFixture(t, db, sc, "two")

	if _, err := CreateDomain(ctx, db, sc, prodID, "example.com", derived, false); err != nil {
		t.Fatalf("create example.com: %v", err)
	}
	_, err := CreateDomain(ctx, db, sc, otherID, "*.example.com", derived, false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "one-prod") {
		t.Errorf("error %q does not name the owning lb", err)
	}
}

func TestIntegrationDuplicateAndLabelRules(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	loadBalancerID := loadBalancerFixture(t, db, sc, "acme")

	if _, err := CreateDomain(ctx, db, sc, loadBalancerID, "example.com", derived, false); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := CreateDomain(ctx, db, sc, loadBalancerID, "example.com", derived, false); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate domain: err = %v, want ErrConflict", err)
	}
	if _, err := CreateDomain(ctx, db, sc, loadBalancerID, "*.*.example.net", derived, false); !errors.Is(err, dnsname.ErrInvalid) {
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
	c, err := CreateDomain(ctx, db, sc, loadBalancerID, "example.com.", derived, false)
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
	loadBalancerID := loadBalancerFixture(t, db, sc, "acme")

	if _, err := CreateDomain(ctx, db, sc, loadBalancerID, "*.example.com", derived, false); err != nil {
		t.Fatalf("create wildcard: %v", err)
	}
	c, err := CreateDomain(ctx, db, sc, loadBalancerID, "shop.example.com", derived, false)
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
	loadBalancerID := loadBalancerFixture(t, db, sc, "acme")
	lb, err := store.GetLoadBalancer(ctx, db, sc, loadBalancerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDomain(ctx, db, sc, loadBalancerID, "example.com", derived, false); err != nil {
		t.Fatal(err)
	}

	if err := DeleteLoadBalancer(ctx, db, sc, loadBalancerID); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("delete lb with a domain: err = %v, want ErrNotEmpty", err)
	}
	if err := DeleteProject(ctx, db, sc, lb.ProjectID); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("delete project with a load balancer: err = %v, want ErrNotEmpty", err)
	}
	if lbs, _ := store.ListLoadBalancersForProject(ctx, db, sc, lb.ProjectID); len(lbs) != 1 {
		t.Error("refused deletion still removed something")
	}
}

func TestIntegrationNodeAddressValidation(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	loadBalancerID := loadBalancerFixture(t, db, sc, "acme")

	for _, tc := range []struct{ v4, v6 string }{
		{"2001:db8::1", ""},   // v6 in the v4 column
		{"", "203.0.113.10"},  // v4 in the v6 column
		{"", ""},              // a node with no address at all
		{"203.0.113.999", ""}, // not an address
	} {
		if _, err := AddNode(ctx, db, sc, loadBalancerID, "b", tc.v4, tc.v6); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddNode(%q, %q): err = %v, want ErrInvalid", tc.v4, tc.v6, err)
		}
	}
	if _, err := AddNode(ctx, db, sc, loadBalancerID, "b", "203.0.113.11", ""); err != nil {
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
	loadBalancerID := loadBalancerFixture(t, db, sc, "acme")

	node, err := AddNode(ctx, db, sc, loadBalancerID, "c", "203.0.113.10", "2001:db8:1::10")
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
			if _, err := UpdateNodeAddresses(ctx, db, sc, node.ID, tc.v4, tc.v6); !errors.Is(err, ErrInvalid) {
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
	updated, err := UpdateNodeAddresses(ctx, db, sc, node.ID, "203.0.113.20", "")
	if err != nil {
		t.Fatalf("valid renumbering: %v", err)
	}
	if updated.IPv4 == nil || *updated.IPv4 != "203.0.113.20" || updated.IPv6 != nil {
		t.Fatalf("after renumbering the node reads %v/%v, want 203.0.113.20 and no v6", updated.IPv4, updated.IPv6)
	}
}
