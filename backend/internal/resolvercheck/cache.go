package resolvercheck

import (
	"sync"
	"time"

	"github.com/miekg/dns"
)

// cacheTTL bounds how long a raw answer is reused before being asked again.
// Short on purpose: a check result is never authoritative state (design D5),
// so the cache exists only to spare a burst of identical lookups (e.g.
// checking several rows of the same plan back to back), not to stand in for
// a fresh answer.
const cacheTTL = 20 * time.Second

// sweepAbove is where expiry-on-write starts paying for itself: comfortably
// more than the questions one plan asks, so a single screen's burst never
// walks the map.
const sweepAbove = 256

type cacheKey struct {
	resolver string
	name     string
	qtype    uint16
}

type cacheEntry struct {
	msg *dns.Msg
	err error
	at  time.Time
}

// cache is a short-lived, in-process, never-persisted memo of raw resolver
// answers, keyed by exactly what was asked.
type cache struct {
	mu      sync.Mutex
	entries map[cacheKey]cacheEntry
}

func (c *cache) get(key cacheKey) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Since(e.at) > cacheTTL {
		return cacheEntry{}, false
	}
	return e, true
}

func (c *cache) put(key cacheKey, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[cacheKey]cacheEntry)
	}
	// A Checker now outlives the request that built it, so nothing else would
	// ever drop an entry: without this the map grows by one key per distinct
	// name for the life of the process. Sweeping on write, and only once the
	// map is big enough for the walk to be worth it, keeps the common path a
	// single insert. Rejected: a background goroutine — a twenty-second memo
	// does not deserve a lifecycle to manage.
	if len(c.entries) >= sweepAbove {
		now := time.Now()
		for k, v := range c.entries {
			if now.Sub(v.at) > cacheTTL {
				delete(c.entries, k)
			}
		}
	}
	e.at = time.Now()
	c.entries[key] = e
}
