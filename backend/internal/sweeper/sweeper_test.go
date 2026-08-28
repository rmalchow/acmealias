package sweeper

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

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", used for schema setup only
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// testDatabaseURLEnv mirrors internal/store's convention (see
// internal/store/testdb_test.go): every database-backed test in this package
// skips, visibly, when it is unset.
const testDatabaseURLEnv = "TEST_DATABASE_URL"

// migratedDB creates a uniquely named schema in TEST_DATABASE_URL, migrates
// it, and returns a connected GORM handle. It duplicates internal/store's
// unexported helper of the same shape rather than importing it — that helper
// isn't exported, and this is the only place outside internal/store that
// needs it.
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
	owner, err := store.FindOrCreateUser(ctx, db, "sweeper-fixture-owner", "Fixture Owner", "")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, "sweeper-fixture", "Fixture", owner.ID, 25)
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

// TestIntegrationExpiredValueIsUnansweredThenSwept covers the two-stage
// contract the service-operations spec describes: expiry is enforced at read
// time (the value is already unanswerable via store.ActiveByAlias the moment
// it expires), and the sweep only cleans up the row afterward.
func TestIntegrationExpiredValueIsUnansweredThenSwept(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	// Through the store, not around it: row-level security is forced, so a raw
	// insert with no account set is refused — which is the policy working.
	p := model.Project{Slug: "proj", Name: "Project"}
	if err := store.CreateProject(ctx, db, sc, &p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	alias := model.Alias{ProjectID: p.ID, Label: "a0", ChallengeFQDN: "a0.acme.example.net"}
	if err := store.CreateAlias(ctx, db, sc, &alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	// Publish with a lifetime so short it has already expired by the time we
	// look, rather than sleeping: no real clock dependency in the test.
	if _, err := store.Present(ctx, db, sc, alias.ID, "tok-1", "k_aaa", time.Nanosecond); err != nil {
		t.Fatalf("present: %v", err)
	}
	time.Sleep(time.Millisecond)

	// Stage 1: the row still exists, but read-time filtering already makes
	// it unanswerable — this is what keeps the zone view correct without
	// waiting on the sweep.
	if n := countChallenges(t, db, alias.ID); n != 1 {
		t.Fatalf("rows before sweep = %d, want 1 (expiry doesn't delete)", n)
	}
	active, err := store.ActiveByAlias(ctx, db)
	if err != nil {
		t.Fatalf("active by alias: %v", err)
	}
	if got := active[alias.Label]; len(got) != 0 {
		t.Fatalf("active values for %q = %v, want none: expired value must already be unanswerable", alias.Label, got)
	}

	// Stage 2: the sweep is housekeeping — it removes the now-useless row and
	// records the count on the metric.
	m := obs.New()
	Sweep(ctx, db, 0, m, log)

	if n := countChallenges(t, db, alias.ID); n != 0 {
		t.Fatalf("rows after sweep = %d, want 0", n)
	}
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var swept float64
	for _, fam := range families {
		if fam.GetName() != "acmeservice_sweep_removed_total" {
			continue
		}
		for _, metric := range fam.GetMetric() {
			swept = metric.GetCounter().GetValue()
		}
	}
	if swept != 1 {
		t.Fatalf("sweep_removed_total = %v, want 1", swept)
	}
}

func countChallenges(t *testing.T, db *gorm.DB, aliasID model.ID) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.Challenge{}).Where("alias_id = ?", aliasID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
