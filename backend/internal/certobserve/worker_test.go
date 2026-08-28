package certobserve

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", for schema setup only
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

const testDatabaseURLEnv = "TEST_DATABASE_URL"

// migratedDB gives each test its own migrated schema, dropped afterwards.
// ponytail: a near-copy of the helper in internal/store and internal/inventory,
// which both live in _test.go files and so cannot be imported. Upgrade path, now
// that this is the third copy: an internal/storetest package.
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

// fixture builds one account with one project and returns the scope every read
// below needs. The Valid check is not ceremony: row-level security is on, and a
// zero Scope reads no rows and raises no error, so a broken fixture would make
// the whole file pass against empty result sets.
func fixture(t *testing.T) (*gorm.DB, store.Scope, model.ID) {
	t.Helper()
	db := migratedDB(t)
	ctx := context.Background()

	owner, err := store.FindOrCreateUser(ctx, db, "certobserve-fixture-owner", "Fixture Owner", "")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, "certobserve-fixture", "Fixture", owner.ID, 25)
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
	p := model.Project{Slug: "demo", Name: "Demo"}
	if err := store.CreateProject(ctx, db, sc, &p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return db, sc, p.ID
}

func addNode(t *testing.T, db *gorm.DB, sc store.Scope, projectID model.ID, label string, v4, v6 *string) model.ID {
	t.Helper()
	n := model.LBNode{ProjectID: projectID, Label: label, IPv4: v4, IPv6: v6}
	if err := store.CreateLBNode(context.Background(), db, sc, &n); err != nil {
		t.Fatalf("create node %s: %v", label, err)
	}
	return n.ID
}

func addDomain(t *testing.T, db *gorm.DB, sc store.Scope, projectID model.ID, name string, observe bool) model.ID {
	t.Helper()
	ctx := context.Background()
	a := model.Alias{ProjectID: projectID, Label: "lbl" + name[:3], ChallengeFQDN: "_acme-challenge." + name}
	if err := store.CreateAlias(ctx, db, sc, &a); err != nil {
		t.Fatalf("create alias for %s: %v", name, err)
	}
	d := model.Domain{ProjectID: projectID, AliasID: a.ID, Name: name}
	if err := store.CreateDomain(ctx, db, sc, &d); err != nil {
		t.Fatalf("create domain %s: %v", name, err)
	}
	if observe {
		if _, err := store.UpdateDomainObservation(ctx, db, sc, d.ID, true, nil); err != nil {
			t.Fatalf("enable observation on %s: %v", name, err)
		}
	}
	return d.ID
}

func ptr(s string) *string { return &s }

// 12.3: a dual-stack node yields two observations and a v4-only node yields
// one — and the absent IPv6 address is NOT recorded as a failure. There was
// nothing to attempt, and a row saying "we could not reach the IPv6 address"
// for a node that has none would be a permanent grey line in the dialog for a
// deployment that is complete.
//
// The addresses are TEST-NET-1 and the documentation prefix, which nothing
// answers on, so every attempt fails — which is the point twice over: the
// failures are still written (G11), so the count is a count of ROWS and not of
// successes.
func TestIntegrationPassObservesEveryAddressOfEveryFamilyOnce(t *testing.T) {
	db, sc, projectID := fixture(t)
	ctx := context.Background()

	dual := addNode(t, db, sc, projectID, "a", ptr("192.0.2.10"), ptr("2001:db8::10"))
	v4only := addNode(t, db, sc, projectID, "b", ptr("192.0.2.11"), nil)
	domainID := addDomain(t, db, sc, projectID, "shop.example.com", true)

	log := slog.New(slog.DiscardHandler)
	seen, observed, err := Pass(ctx, db, Options{Interval: time.Hour, HandshakeTimeout: 200 * time.Millisecond}, log)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if seen != 0 {
		t.Errorf("seen = %d, want 0 — nothing answers on those addresses", seen)
	}
	if observed != 3 {
		t.Fatalf("observed = %d, want 3 (dual-stack node twice, v4-only node once)", observed)
	}

	got, err := store.LatestObservations(ctx, db, sc, domainID)
	if err != nil {
		t.Fatalf("latest observations: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("stored %d observations, want 3", len(got))
	}
	families := map[model.ID][]string{}
	for _, o := range got {
		families[o.NodeID] = append(families[o.NodeID], o.Family)
		if Outcome(o.Outcome) == OutcomeObserved {
			t.Errorf("outcome = observed for %s, want a failure classification", o.Address)
		}
		if o.ServerName != "shop.example.com" {
			t.Errorf("server name = %q, want the domain's own name", o.ServerName)
		}
	}
	if len(families[dual]) != 2 {
		t.Errorf("dual-stack node has %v, want both families", families[dual])
	}
	if len(families[v4only]) != 1 || families[v4only][0] != "A" {
		t.Errorf("v4-only node has %v, want exactly [A] — the absent family is not a failure", families[v4only])
	}
}

// 12.13: a domain with the switch off is never connected to, at all, from any
// pass. The assertion is on DueObservations' result rather than on a count of
// dials, because the predicate is where this can go wrong: a worker that probed
// what it was handed would be blameless and the connection would still have
// happened.
func TestIntegrationDomainWithTheSwitchOffIsNeverQueued(t *testing.T) {
	db, sc, projectID := fixture(t)
	ctx := context.Background()

	addNode(t, db, sc, projectID, "a", ptr("192.0.2.10"), ptr("2001:db8::10"))
	addDomain(t, db, sc, projectID, "off.example.com", false)
	onID := addDomain(t, db, sc, projectID, "onx.example.com", true)

	due, err := store.DueObservations(ctx, db, time.Now().UTC(), time.Hour, batch)
	if err != nil {
		t.Fatalf("due observations: %v", err)
	}
	if len(due) != 2 {
		t.Fatalf("queued %d attempts, want 2 — only the enabled domain's two addresses", len(due))
	}
	for _, d := range due {
		if d.DomainID != onID {
			t.Errorf("queued an attempt for domain %s, which has observation switched off", d.DomainID)
		}
	}
}
