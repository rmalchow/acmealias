package apikey

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// touchInterval is how often one key's last_used_at may be written. Anything
// finer would turn every authenticated request into a database write for a
// timestamp nobody reads more than once a day.
const touchInterval = time.Minute

// LastUsed rate-limits last_used_at writes to at most one per key per minute.
// The zero value is not usable; build one with NewLastUsed.
//
// ponytail: one mutex over one map, held only around a timestamp compare. The
// map holds an entry per key seen since startup — bounded by the number of keys
// in the database, so it needs no eviction until that stops being a small
// number. Upgrade path: a sync.Map, or an LRU if key counts ever grow.
type LastUsed struct {
	touch func(ctx context.Context, id model.ID) error
	log   *slog.Logger

	mu   sync.Mutex
	seen map[model.ID]time.Time
}

// NewLastUsed writes through store.TouchLastUsed.
func NewLastUsed(db *gorm.DB, log *slog.Logger) *LastUsed {
	return newLastUsed(func(ctx context.Context, id model.ID) error {
		return store.TouchLastUsed(ctx, db, id)
	}, log)
}

func newLastUsed(touch func(context.Context, model.ID) error, log *slog.Logger) *LastUsed {
	if log == nil {
		log = slog.Default()
	}
	return &LastUsed{touch: touch, log: log, seen: map[model.ID]time.Time{}}
}

// Touch records a use of key id, writing through at most once per minute. A
// failed write is logged and dropped: this is display bookkeeping, and losing a
// timestamp must never fail an otherwise valid request.
func (l *LastUsed) Touch(ctx context.Context, id model.ID) {
	now := time.Now()
	l.mu.Lock()
	if last, ok := l.seen[id]; ok && now.Sub(last) < touchInterval {
		l.mu.Unlock()
		return
	}
	l.seen[id] = now
	l.mu.Unlock()

	if err := l.touch(ctx, id); err != nil {
		l.log.WarnContext(ctx, "api key last_used_at not recorded",
			slog.String("api_key_id", id.String()), slog.Any("error", err))
	}
}
