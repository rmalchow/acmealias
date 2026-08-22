package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", used for schema setup only
	"gorm.io/gorm"
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
