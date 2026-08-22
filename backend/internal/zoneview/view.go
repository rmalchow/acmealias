// Package zoneview holds the in-memory view of alias labels and their active
// challenge values that the DNS server answers from (design D6).
//
// The view holds *every* alias label, not only the labels that currently have
// values. A label present with an empty slice is a name that exists and has no
// records — NOERROR with an empty answer. A label absent from the map does not
// exist — NXDOMAIN. Keying the view by challenge values alone cannot tell those
// apart and would answer NXDOMAIN after every successful cleanup.
package zoneview

import (
	"context"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Loader reads the authoritative state: every alias label mapped to its
// non-expired values. store.ActiveByAlias is the implementation; the indirection
// exists so the view can be tested, and its failure behaviour exercised, without
// a database.
type Loader func(ctx context.Context) (map[string][]store.ChallengeValue, error)

// FromDB adapts the store query to a Loader.
func FromDB(db *gorm.DB) Loader {
	return func(ctx context.Context) (map[string][]store.ChallengeValue, error) {
		return store.ActiveByAlias(ctx, db)
	}
}

// View is a snapshot of the zone, replaced wholesale by each successful refresh
// and read under a shared lock in between.
type View struct {
	load Loader

	mu      sync.RWMutex
	aliases map[string][]store.ChallengeValue // label → active values, possibly empty
	lastOK  time.Time                         // when the snapshot was last loaded successfully
	lastErr error                             // failure of the most recent refresh, nil if it succeeded
}

// New returns an empty view. Nothing is loaded until Refresh or Run runs: until
// then every label is absent, so callers must complete one Refresh before
// serving, or serve NXDOMAIN for names that do exist.
func New(load Loader) *View {
	return &View{load: load, aliases: map[string][]store.ChallengeValue{}}
}

// Lookup returns the currently answerable TXT values at label. The second result
// reports whether the label exists at all — false means NXDOMAIN, true with no
// values means NOERROR with an empty answer.
//
// Expiry is applied here, at read time, so a value stops being answered the
// instant it expires no matter when the sweep or the next refresh runs.
func (v *View) Lookup(label string) ([]string, bool) {
	label = strings.ToLower(strings.TrimSuffix(label, "."))

	v.mu.RLock()
	defer v.mu.RUnlock()

	vals, ok := v.aliases[label]
	if !ok {
		return nil, false
	}
	now := time.Now()
	out := make([]string, 0, len(vals))
	for _, c := range vals {
		if c.ExpiresAt.After(now) {
			out = append(out, c.Value)
		}
	}
	return out, true
}

// Has reports whether the label exists, regardless of whether it has values —
// what a non-TXT query at an existing name needs to answer NOERROR rather than
// NXDOMAIN.
func (v *View) Has(label string) bool {
	label = strings.ToLower(strings.TrimSuffix(label, "."))
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, ok := v.aliases[label]
	return ok
}

// replace installs a new snapshot.
func (v *View) replace(aliases map[string][]store.ChallengeValue) {
	lowered := make(map[string][]store.ChallengeValue, len(aliases))
	for label, vals := range aliases {
		lowered[strings.ToLower(label)] = vals
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.aliases = lowered
	v.lastOK = time.Now()
	v.lastErr = nil
}
