package store

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // "postgres://" URLs
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// The binary carries its own schema: the migration files are compiled in and read
// through source/iofs, so no files or client binary are needed at runtime (design D8).
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrDatabaseAhead is returned when the database has been migrated by a newer
// binary than this one. golang-migrate does not check this itself — Up() would be a
// silent no-op and this binary would then query a schema it does not understand.
var ErrDatabaseAhead = errors.New("database schema is newer than this binary")

// EmbeddedVersion is the highest migration version compiled into this binary.
func EmbeddedVersion() (uint, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	var highest uint
	for _, e := range entries {
		m, err := source.Parse(e.Name())
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if m.Version > highest {
			highest = m.Version
		}
	}
	if highest == 0 {
		return 0, errors.New("no embedded migrations")
	}
	return highest, nil
}

func openMigrate(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open migrations: %w", err)
	}
	return m, nil
}

// checkVersion fails when the applied version is ahead of the embedded one, or when
// a previous migration left the database dirty.
func checkVersion(m *migrate.Migrate) error {
	embedded, err := EmbeddedVersion()
	if err != nil {
		return err
	}
	applied, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return nil // empty database
	}
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("database schema is dirty at version %d: a previous migration "+
			"failed halfway; repair it by hand and clear the dirty flag before starting", applied)
	}
	if applied > embedded {
		return fmt.Errorf("%w: database is at version %d, this binary carries %d — "+
			"it was migrated by a newer release; rolling back across a migration is a "+
			"restore-from-backup operation, not a redeploy", ErrDatabaseAhead, applied, embedded)
	}
	return nil
}

// CheckSchemaVersion verifies the database is not ahead of this binary, without
// applying anything. Startup calls this when migrating on start is disabled.
func CheckSchemaVersion(databaseURL string) error {
	m, err := openMigrate(databaseURL)
	if err != nil {
		return err
	}
	defer m.Close()
	return checkVersion(m)
}

// MigrateUp applies every embedded migration that has not been applied yet. It is
// idempotent, and safe to run from two instances at once: the postgres driver takes
// an advisory lock for the duration. It refuses to run against a database that is
// ahead of this binary.
func MigrateUp(databaseURL string) error {
	m, err := openMigrate(databaseURL)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := checkVersion(m); err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// MigrateDown reverses every migration. It is never run automatically — only by an
// explicit, supervised command, and by the migration tests.
func MigrateDown(databaseURL string) error {
	m, err := openMigrate(databaseURL)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("revert migrations: %w", err)
	}
	return nil
}
