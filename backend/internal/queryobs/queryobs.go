// Package queryobs counts DNS queries answered for an alias and folds those
// counts into the challenge-interaction history, without letting the datastore
// anywhere near the query path.
//
// The rule this package exists to keep (design D14, "DNS query observations are
// attributed without datastore access on the query path"): answering a query
// must not read or write the database. The DNS server is the part of this
// service that has to keep working when Postgres does not — it answers from the
// in-memory zone view, and a history write on that path would hand it a
// dependency it deliberately does not have.
//
// So the query path does one thing: increment a counter in a map under a mutex.
// A background flusher drains the map on an interval and applies the totals. If
// the datastore is down the counts simply accumulate and land later; if the
// process dies they are lost, which is the correct trade for a statistic that
// says "the CA came to look" rather than for a record anyone bills against.
package queryobs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Observation is what one alias label accumulated over a flush window: how many
// queries were answered, and when the first and last of them arrived.
//
// The timestamps are the real ones, taken as the queries came in, rather than
// the window's bounds. That distinction is small and it is the difference
// between "first queried at 14:02:07" and "first queried at 14:02:30, because
// that is when the flusher happened to run" — and an operator reading a history
// to work out whether a CA arrived before a record propagated needs the former.
type Observation struct {
	Count int64
	First time.Time
	Last  time.Time
}

// Flusher applies a drained batch accumulated over [from, to]. Both bounds are
// the counter's, not the moment the flush runs, so a slow or retried flush
// cannot shift the window a query is attributed to — and the window is what
// attribution uses: a DNS-01 validation is typically over long before the next
// flush, so "which rows are live right now" is the wrong question.
type Flusher func(ctx context.Context, obs map[string]Observation, from, to time.Time) error

// Counter accumulates per-alias-label query counts.
//
// Keyed by LABEL and not by alias id on purpose: the label is what the DNS
// handler already has in its hand, and turning it into an id is a datastore
// lookup — exactly what must not happen here. The flusher resolves labels to
// ids once per batch instead of once per query.
type Counter struct {
	mu     sync.Mutex
	counts map[string]Observation
	// Start of the window the current counts cover: the moment of the last
	// drain, or of construction. Carried so the flusher can attribute by
	// overlap rather than by "still live".
	since time.Time
}

func NewCounter() *Counter {
	return &Counter{counts: map[string]Observation{}, since: time.Now()}
}

// Observe records one answered query for label. Called from the DNS handler, on
// every query it answers with at least one value.
func (c *Counter) Observe(label string) {
	if c == nil || label == "" {
		return
	}
	now := time.Now()
	c.mu.Lock()
	o := c.counts[label]
	o.Count++
	if o.First.IsZero() {
		o.First = now
	}
	o.Last = now
	c.counts[label] = o
	c.mu.Unlock()
}

// Drain returns the accumulated counts, the start of the window they cover, and
// resets both.
func (c *Counter) Drain() (map[string]Observation, time.Time) {
	if c == nil {
		return nil, time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	since := c.since
	c.since = time.Now()
	if len(c.counts) == 0 {
		return nil, since
	}
	out := c.counts
	c.counts = make(map[string]Observation, len(out))
	return out, since
}

// merge puts a failed batch back, adding to whatever accumulated meanwhile, so a
// datastore outage delays observations rather than dropping them. The window
// start goes back with them: the retry must cover the period those queries
// actually happened in, not the one that began when they failed to land.
func (c *Counter) merge(obs map[string]Observation, since time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for label, o := range obs {
		cur := c.counts[label]
		cur.Count += o.Count
		if cur.First.IsZero() || (!o.First.IsZero() && o.First.Before(cur.First)) {
			cur.First = o.First
		}
		if o.Last.After(cur.Last) {
			cur.Last = o.Last
		}
		c.counts[label] = cur
	}
	if since.Before(c.since) {
		c.since = since
	}
}

// Run drains and applies on an interval until ctx is cancelled, then flushes one
// last time so a clean shutdown does not discard the final window.
func (c *Counter) Run(ctx context.Context, interval time.Duration, flush Flusher, log *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			// A cancelled context cannot be used for the write, and the process
			// is going away regardless, so this last flush gets a short one of
			// its own rather than skipping.
			last, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			c.flushOnce(last, flush, log)
			cancel()
			return
		case <-t.C:
			c.flushOnce(ctx, flush, log)
		}
	}
}

func (c *Counter) flushOnce(ctx context.Context, flush Flusher, log *slog.Logger) {
	obs, since := c.Drain()
	if len(obs) == 0 {
		return
	}
	to := time.Now()
	if err := flush(ctx, obs, since, to); err != nil {
		c.merge(obs, since)
		log.WarnContext(ctx, "query observations not applied, will retry",
			slog.Int("aliases", len(obs)), slog.Any("error", err))
	}
}
