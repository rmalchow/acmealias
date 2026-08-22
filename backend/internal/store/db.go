// Package store owns the database schema and the database handle. The schema is
// created by the embedded migrations in migrations/; GORM is a query layer only and
// AutoMigrate is never called (design D8).
package store

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Options are the connection settings. The caller (startup wiring) fills these from
// configuration; the store deliberately does not import the config package, so the
// two can be built and tested independently.
type Options struct {
	DatabaseURL     string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	SlowThreshold   time.Duration // queries slower than this are logged at warn level
	Logger          *slog.Logger
}

// Open connects to PostgreSQL and configures the pool. It does not migrate; call
// MigrateUp (or at least CheckSchemaVersion) before serving.
func Open(opts Options) (*gorm.DB, error) {
	if opts.DatabaseURL == "" {
		return nil, fmt.Errorf("store: database URL is empty")
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	if opts.SlowThreshold <= 0 {
		opts.SlowThreshold = 200 * time.Millisecond
	}

	db, err := gorm.Open(postgres.Open(opts.DatabaseURL), &gorm.Config{
		Logger: gormlogger.New(slogWriter{log}, gormlogger.Config{
			SlowThreshold:             opts.SlowThreshold,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if opts.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	}
	if opts.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	}
	if opts.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)
	}
	return db, nil
}

// slogWriter bridges GORM's Printf-shaped logger onto log/slog.
// ponytail: everything GORM emits lands at warn level, which is the only level it
// uses once LogLevel is Warn (slow queries and errors). Upgrade path if that stops
// being true: implement gormlogger.Interface directly and map each method.
type slogWriter struct{ log *slog.Logger }

func (w slogWriter) Printf(format string, args ...any) {
	w.log.Warn("gorm: " + fmt.Sprintf(format, args...))
}
