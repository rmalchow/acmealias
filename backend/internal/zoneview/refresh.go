package zoneview

import (
	"context"
	"log/slog"
	"time"
)

// Refresh reloads the snapshot from the datastore. Callers invoke it
// synchronously after a successful write — a present, a cleanup, or an alias
// created or deleted — before the HTTP response is written, which is what makes
// "answerable by the time /present returns" true. Creating an alias needs it just
// as much as publishing a value does: without it a new label answers NXDOMAIN
// until the next timed refresh.
//
// On failure the previous snapshot is kept and served unchanged: a name whose
// value was known is never withdrawn because the database went away. The error is
// returned for the caller to log, and recorded for Status.
func (v *View) Refresh(ctx context.Context) error {
	aliases, err := v.load(ctx)
	if err != nil {
		v.mu.Lock()
		v.lastErr = err
		v.mu.Unlock()
		return err
	}
	v.replace(aliases)
	return nil
}

// Run refreshes once and then on every tick of interval until ctx is cancelled —
// the safety net against rows changing behind our back (another replica, a manual
// edit, the sweep). Refresh-on-write is what keeps the view current; this only
// bounds how long a missed write can stay missed. Errors are logged, not fatal.
//
// A non-positive interval means the initial load only, no ticker.
func (v *View) Run(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	if err := v.Refresh(ctx); err != nil {
		log.ErrorContext(ctx, "zone view initial load failed, serving empty view", "error", err)
	}
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := v.Refresh(ctx); err != nil {
				log.WarnContext(ctx, "zone view refresh failed, serving last known snapshot",
					"error", err, "snapshot_age", time.Since(v.LastRefresh()))
			}
		}
	}
}

// Status is what the readiness probe and the datastore-reachability metric report.
type Status struct {
	// Reachable is false when the most recent refresh failed. The view keeps
	// serving in that state; it is a warning, not an outage.
	Reachable bool
	// Loaded is false until the first successful load. A view that has never
	// loaded answers NXDOMAIN for everything and is not ready to serve.
	Loaded bool
	// LastRefresh is when the snapshot currently being served was loaded.
	LastRefresh time.Time
	// Labels is how many alias labels the snapshot holds.
	Labels int
	// Err is the most recent refresh failure, nil when Reachable.
	Err error
}

func (v *View) Status() Status {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return Status{
		Reachable:   v.lastErr == nil,
		Loaded:      !v.lastOK.IsZero(),
		LastRefresh: v.lastOK,
		Labels:      len(v.aliases),
		Err:         v.lastErr,
	}
}

// LastRefresh is when the snapshot currently being served was loaded.
func (v *View) LastRefresh() time.Time {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.lastOK
}
