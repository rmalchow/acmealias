package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", used for schema setup only
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// testDatabaseURLEnv names the variable every database-backed test in this package
// depends on. CI does not set it (design D13), so those tests skip there — visibly,
// never as a silent pass.
const testDatabaseURLEnv = "TEST_DATABASE_URL"

// freshSchemaURL skips the test when TEST_DATABASE_URL is unset. Otherwise it
// creates an empty, uniquely named schema in that database and returns a URL
// pinned to it with search_path, so tests are isolated from each other and from
// whatever else lives in the database. The schema is dropped when the test ends.
func freshSchemaURL(t *testing.T) string {
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
	return u.String()
}

// appRoleURL is freshSchemaURL plus the thing that makes the row-level security
// tests mean anything: a connection that is NOT a superuser and NOT the owner of
// the tables.
//
// This is not test scaffolding, it is the deployment contract. A superuser
// bypasses every policy unconditionally, and a table's owner bypasses its
// policies unless the table is forced. Connect as either and the isolation this
// package claims is decoration — so the tests connect the way production must,
// and store.CheckTenantIsolation refuses to start any process that does not.
func appRoleURL(t *testing.T, adminDSN, schema string) string {
	t.Helper()
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer admin.Close()

	var buf [4]byte
	rand.Read(buf[:])
	role := "app_" + hex.EncodeToString(buf[:])
	pw := "app_" + hex.EncodeToString(buf[:])

	stmts := []string{
		"create role " + role + " login password '" + pw + "'",
		"grant usage on schema " + schema + " to " + role,
		"grant select, insert, update, delete on all tables in schema " + schema + " to " + role,
		"grant usage, select on all sequences in schema " + schema + " to " + role,
		"grant execute on all functions in schema " + schema + " to " + role,
	}
	for _, stmt := range stmts {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		c, err := sql.Open("pgx", adminDSN)
		if err != nil {
			return
		}
		defer c.Close()
		for _, stmt := range []string{
			"revoke all on all tables in schema " + schema + " from " + role,
			"revoke all on all sequences in schema " + schema + " from " + role,
			"revoke all on all functions in schema " + schema + " from " + role,
			"revoke usage on schema " + schema + " from " + role,
			"drop role " + role,
		} {
			_, _ = c.Exec(stmt)
		}
	})

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.User = url.UserPassword(role, pw)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// isolatedDB migrates as the owner, then hands back a handle connected as an
// ordinary application role — the only arrangement in which the policies apply.
func isolatedDB(t *testing.T) (*gorm.DB, Scope) {
	t.Helper()
	dsn := freshSchemaURL(t)
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	schema := u.Query().Get("search_path")
	db, err := Open(Options{DatabaseURL: appRoleURL(t, dsn, schema), Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("open as application role: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	sc, err := ScopeForBootstrap(context.Background(), db)
	if err != nil {
		t.Fatalf("bootstrap scope: %v", err)
	}
	return db, sc
}

// migratedDB returns a schema with every migration applied and a GORM handle on it.
func migratedDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dsn := freshSchemaURL(t)
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := Open(Options{DatabaseURL: dsn, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db, dsn
}

// scopedDB is migratedDB plus the scope every inventory call now takes. The
// migration seeds exactly one account, so the bootstrap producer is the honest
// way for a test to obtain one — a test that could mint a Scope from an
// identifier would be testing a constructor that does not exist in production.
func scopedDB(t *testing.T) (*gorm.DB, Scope) {
	t.Helper()
	db, _ := migratedDB(t)
	sc, err := ScopeForBootstrap(context.Background(), db)
	if err != nil {
		t.Fatalf("bootstrap scope: %v", err)
	}
	return db, sc
}

// secondAccount creates another account and returns a scope confined to it, for
// the tests that assert one account cannot reach another's rows.
func secondAccount(t *testing.T, db *gorm.DB, slug string) Scope {
	t.Helper()
	a := &model.Account{ID: model.NewID(), Slug: slug, Name: slug}
	if err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Create(a).Error
	}); err != nil {
		t.Fatalf("create account %q: %v", slug, err)
	}
	return Scope{accountID: a.ID}
}
