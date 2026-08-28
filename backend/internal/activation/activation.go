// Package activation is the sixth background worker: it looks up customers'
// challenge delegations out of band and records what it saw, which is what
// makes a domain answerable at all.
//
// A domain is OFFLINE until a check has seen its `_acme-challenge` CNAME
// pointing at the alias we issued (domain-activation A1). The check cannot gate
// the write that creates the domain — the CNAME's target is the alias, and the
// alias does not exist until the domain does — so it gates the work afterwards.
//
// TWO THINGS THIS PACKAGE MUST NEVER BECOME:
//
//   - It never runs on a request path. /present is called by lego in the middle
//     of an issuance; a live resolver call there would make certificate issuance
//     depend on a third party's availability, which is a far worse failure than
//     the abuse it prevents. The worker observes and stores; the gate reads the
//     stored state and asks nobody.
//   - It never takes an activated domain offline (A3). See
//     store.RecordDelegationMissing, which is where that promise is kept.
package activation

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/resolvercheck"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Options is what one pass needs beyond the database and the resolver.
type Options struct {
	// Zone is the zone this service is authoritative for (ACME_ZONE). A
	// delegation must point at "<alias label>.<zone>", the same string
	// inventory.Plan puts in front of the customer.
	Zone string

	// FreshInterval and SettledInterval are the two cadences of A2, straight
	// from configuration (ACME_ACTIVATION_FRESH_INTERVAL and
	// ACME_ACTIVATION_SETTLED_INTERVAL, task 2.4).
	FreshInterval   time.Duration
	SettledInterval time.Duration

	// Batch caps how many delegations one pass looks at. Not configuration:
	// it is a bound on the burst this process aims at somebody's recursor and
	// on how long one tick can run, not a behaviour anyone tunes. The queue is
	// ordered so that a batch too small for the backlog delays settled
	// re-checks and never the domain somebody is waiting on — see
	// store.DueDelegations. Zero means DefaultBatch.
	Batch int
}

// DefaultBatch is Options.Batch's default: comfortably more delegations than a
// tick of a healthy deployment finds due, and few enough that a resolver which
// has started blackholing costs one bounded pass rather than an unbounded one.
const DefaultBatch = 256

// DefaultInterval is what Run falls back to when it is handed a non-positive
// FreshInterval. It is not a configuration default — config.Validate supplies
// and checks that — only the floor that keeps an exported function from
// panicking on an argument nobody validated.
const DefaultInterval = 5 * time.Minute

// parallelism bounds the questions one pass has outstanding, for the same
// reason httpapi bounds a plan verification: one unanswerable name must not
// delay every delegation behind it by a full timeout, and the fix for that must
// not be a burst of hundreds of simultaneous queries at somebody's recursor.
const parallelism = 8

// Run checks delegations on a ticker until ctx is cancelled, so the caller runs
// it in its own goroutine and cancels ctx as part of shutdown — the same shape
// as the five workers that already exist (A5).
//
// The ticker runs at the FRESH interval, which is the finer of the two: it is
// how often the worker looks for work, not how often any one domain is checked.
// Which domains are due is decided per row by their own timestamps, so a settled
// domain simply is not selected until its own day is up.
//
// A pass runs immediately rather than after the first tick. A restart otherwise
// leaves a customer who published their CNAME thirty seconds ago waiting out a
// full interval for a service that is already running.
func Run(ctx context.Context, db *gorm.DB, checker *resolvercheck.Checker, opts Options, log *slog.Logger) {
	// Run and Pass are exported so a test or an operator-triggered check can
	// drive one round, which means the interval can arrive here unvalidated even
	// though config.Validate rejects it on the production path. NewTicker panics
	// on a non-positive duration, and a worker that takes the process down at
	// boot over a misread setting is a worse answer than one that falls back and
	// says so.
	interval := opts.FreshInterval
	if interval <= 0 {
		interval = DefaultInterval
		log.Warn("activation check interval is not positive; falling back",
			"configured", opts.FreshInterval, "using", interval)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if seen, checked, err := Pass(ctx, db, checker, opts, log); err != nil {
			log.Error("delegation checks failed", "error", err)
		} else if checked > 0 {
			log.Debug("delegation checks completed", "delegations", checked, "seen", seen)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Pass runs one round of checks and reports how many delegations were seen and
// how many were looked at. It is exported so a test, or an operator-triggered
// check, can run a round outside Run's ticker.
//
// Two instances running a pass at the same moment produce the same answer twice
// rather than a conflict (A5): the work is selected without claiming it, the
// lookup is a question with no side effect, and both writes are assignments of
// a timestamp rather than increments — so nothing here needs a lock, and the
// worst a race costs is one duplicated DNS question.
func Pass(ctx context.Context, db *gorm.DB, checker *resolvercheck.Checker, opts Options, log *slog.Logger) (seen, checked int, err error) {
	batch := opts.Batch
	if batch <= 0 {
		batch = DefaultBatch
	}
	due, err := store.DueDelegations(ctx, db, time.Now().UTC(), opts.FreshInterval, opts.SettledInterval, batch)
	if err != nil {
		return 0, 0, err
	}
	if len(due) == 0 {
		return 0, 0, nil
	}

	// Resolve first, write second. The two phases are kept apart so the writes
	// happen on one goroutine in a known order: a pass that interleaved them
	// would hold a pool connection per outstanding lookup, which is how a slow
	// resolver turns into a database that has run out of connections for the
	// request path.
	results := lookup(ctx, checker, opts.Zone, due)

	at := time.Now().UTC()
	for i, d := range due {
		var domains int64
		var werr error
		observed := results[i] == resolvercheck.StatusCorrect
		if observed {
			domains, werr = store.RecordDelegationSeen(ctx, db, d.AliasID, at)
		} else {
			// Missing, incorrect and undetermined alike. The distinction is
			// the console's to draw from a live check (domain-setup-feedback
			// D1); here all three mean the same thing — we looked, and we
			// cannot publish for this domain yet.
			domains, werr = store.RecordDelegationMissing(ctx, db, d.AliasID, at)
		}
		if werr != nil {
			// One delegation's write failing must not abandon the rest of the
			// batch: the next pass will pick this one up again, unchanged,
			// because nothing was stamped.
			log.Warn("delegation check not recorded", "delegation", d.ChallengeFQDN, "error", werr)
			continue
		}
		// Counted after the write, not before it: both numbers are what this
		// pass DID, and a "seen" that included a delegation whose stamp failed
		// would report an activation that did not happen.
		if observed {
			seen++
		}
		checked++
		log.Debug("delegation checked", "delegation", d.ChallengeFQDN,
			"target", target(d.Label, opts.Zone), "status", results[i], "domains", domains)
	}
	return seen, checked, nil
}

// lookup asks the resolver about every delegation in the batch, bounded, and
// returns the answers in the order they were asked.
func lookup(ctx context.Context, checker *resolvercheck.Checker, zone string, due []store.DueDelegation) []resolvercheck.Status {
	out := make([]resolvercheck.Status, len(due))
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i, d := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = checker.CheckCNAME(ctx, resolvercheck.CNAMECheck{
				Name:     d.ChallengeFQDN,
				Expected: target(d.Label, zone),
			}).Status
		}()
	}
	wg.Wait()
	return out
}

// target is the CNAME target the delegation must point at. It is assembled the
// same way inventory.buildPlan assembles it, because the customer was shown
// that string and a check asking for any other one would refuse a delegation
// that is in fact correct. The zone is trimmed the same way for the same
// reason: ACME_ZONE set with a trailing dot would otherwise ask for a name with
// two, which resolves to nothing and would leave every domain in the deployment
// offline for ever.
func target(label, zone string) string {
	return label + "." + strings.TrimSuffix(strings.ToLower(zone), ".")
}
