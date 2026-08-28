package certobserve

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Options is what one pass needs beyond the database.
//
// There is no Batch and no Parallelism here, and that is deliberate — see the
// two constants below.
type Options struct {
	// Interval is the observation cadence (ACME_OBSERVATION_INTERVAL, default
	// six hours). It is both how often the ticker comes round and the age at
	// which an address becomes due, which is why it is one setting and not two:
	// two would let a deployment configure a worker that wakes up hourly and
	// never finds anything to do.
	//
	// config.Validate refuses anything below one hour and names the setting
	// rather than clamping it. That floor is not decoration: observation is
	// unmetered (G17), so nothing on the customer's side bounds how much of it
	// we do, and the floor is the only thing between a mistyped value and this
	// service hammering somebody else's production load balancer.
	Interval time.Duration

	// HandshakeTimeout bounds ONE attempt, dial and handshake together
	// (ACME_OBSERVATION_HANDSHAKE_TIMEOUT, default five seconds). A node that
	// has started blackholing must cost one bounded pass, not an unbounded one.
	HandshakeTimeout time.Duration
}

// DefaultInterval and DefaultHandshakeTimeout are what Run and Pass fall back to
// when handed a non-positive value. They are NOT the configuration defaults —
// config supplies and validates those — only the floor that keeps an exported
// function from panicking on an argument nobody checked, the same role
// activation.DefaultInterval plays.
const (
	DefaultInterval         = 6 * time.Hour
	DefaultHandshakeTimeout = 5 * time.Second
)

// batch and parallelism are CONSTANTS, not configuration, for the same reason
// they are in activation: they bound the burst this process aims at somebody
// else's machine and how long one tick may run. They are not behaviour anybody
// tunes, and a deployment that wants less observation turns the cadence down or
// the worker off.
//
// activation spells the first one activation.DefaultBatch because it is
// overridable there; here nothing overrides it, so it is unexported and the name
// says what it is.
const (
	// batch is comfortably more handshakes than a tick of a healthy deployment
	// finds due — observation is opt-in per domain, so the population is the
	// handful of names somebody cares about rather than every domain we hold.
	batch = 256

	// parallelism is FOUR and not activation's eight. These are TLS handshakes
	// against a customer's production load balancer, not DNS queries against our
	// own recursor, and eight simultaneous connections from one source address is
	// the shape of something a rate limiter or an intrusion detector notices —
	// at which point we have taught the customer's infrastructure to treat us as
	// an attacker in order to draw a dot. It still keeps one unreachable address
	// from delaying every address behind it by a full timeout, which is the only
	// job it has.
	parallelism = 4
)

// port is 443 and is not configurable (G6). The question this check answers is
// "is the certificate a visitor would be served in good order", and a visitor
// arrives on 443. A configurable port would be a different question with the
// same dot.
const port = "443"

// Run observes on a ticker until ctx is cancelled, so the caller runs it in its
// own goroutine and cancels ctx as part of shutdown — the same shape as the six
// workers that already exist.
//
// A pass runs IMMEDIATELY rather than after the first tick. A restart otherwise
// leaves somebody who switched observation on thirty seconds ago waiting out a
// full six hours for a service that is already running, staring at a grey dot
// that a redeploy caused. (sweeper.Run selects first and gets this wrong;
// activation.Run and reporting.RunSnapshots get it right, and this copies those.)
func Run(ctx context.Context, db *gorm.DB, opts Options, log *slog.Logger) {
	interval := opts.Interval
	if interval <= 0 {
		// NewTicker panics on a non-positive duration, and a worker that takes
		// the process down at boot over a misread setting is a worse answer than
		// one that falls back and says so. The production path cannot reach this
		// — config.Validate refuses it, and refuses anything under an hour — but
		// Run is exported and a test or an operator can call it.
		interval = DefaultInterval
		log.Warn("certificate observation interval is not positive; falling back",
			"configured", opts.Interval, "using", interval)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if seen, observed, err := Pass(ctx, db, opts, log); err != nil {
			log.Error("certificate observations failed", "error", err)
		} else if observed > 0 {
			log.Debug("certificate observations completed", "observations", observed, "certificates", seen)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Pass runs one round and reports how many attempts produced a certificate
// (seen) and how many observations were written (observed). Every attempt is
// written, successful or not, so observed is the size of the batch minus
// whatever failed to STORE — a failed handshake counts here, because the row
// exists (G11).
//
// It is exported, and split out of Run, for the reason activation's is: a test
// that had to start a ticker to assert one pass is a test with a sleep in it.
//
// Two instances running a pass at the same moment produce two observations of
// the same moment rather than a conflict. The work is selected without being
// claimed and every write is an append, so nothing here needs a lock and the
// worst a race costs is one duplicated handshake — which is also why nothing
// downstream believes a single observation (G14).
//
// NOTHING HERE READS THE ACCOUNT KIND, and that is on purpose. History is
// recorded for personal and team accounts alike, even though only a team account
// may read it back (G15): "only record what we show" is the optimisation
// somebody will propose, and taking it would mean that upgrading an account
// produces a feature with no data in it for weeks. The gate belongs on the read.
func Pass(ctx context.Context, db *gorm.DB, opts Options, log *slog.Logger) (seen, observed int, err error) {
	timeout := opts.HandshakeTimeout
	if timeout <= 0 {
		timeout = DefaultHandshakeTimeout
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}

	// The queue order — never observed first, then longest since observed — is
	// the query's and is not re-sorted here, so that a batch too small for the
	// backlog delays the settled work and never the person who just turned the
	// switch on and is watching the screen (G13). A domain with the switch off
	// never appears at all: the opt-in is enforced in the predicate, and this
	// worker probes whatever it is handed.
	due, err := store.DueObservations(ctx, db, time.Now().UTC(), interval, batch)
	if err != nil {
		return 0, 0, err
	}
	if len(due) == 0 {
		return 0, 0, nil
	}

	// Probe first, write second. The phases are kept apart so the writes happen
	// on one goroutine: a pass that interleaved them would hold a pool
	// connection per outstanding handshake, and a batch of blackholing addresses
	// against a pool of ten is not a slow pass, it is a database with no
	// connections left for the request path.
	probes := probeAll(ctx, due, timeout)

	at := time.Now().UTC()
	for i, d := range due {
		if err := store.RecordObservation(ctx, db, probes[i].observation(d, at)); err != nil {
			// One write failing must not abandon the rest of the batch. Nothing
			// was stamped, so the next pass picks this address up again — an
			// observation is an append and there is no partial state to undo.
			log.Warn("certificate observation not recorded",
				"domain", d.DomainID, "address", d.Address, "family", d.Family, "error", err)
			continue
		}
		// Counted after the write for the reason activation counts after its
		// own: both numbers are what this pass DID, and a "seen" that included
		// an observation whose insert failed would report history that is not
		// there.
		if probes[i].outcome == OutcomeObserved {
			seen++
		}
		observed++
		log.Debug("certificate observed", "domain", d.DomainID, "server_name", d.ServerName,
			"address", d.Address, "family", d.Family, "outcome", probes[i].outcome)
	}
	return seen, observed, nil
}

// probeAll runs the batch's handshakes with bounded parallelism and returns the
// results positionally, so the write phase can pair each with the queue entry
// that asked for it.
func probeAll(ctx context.Context, due []store.DueObservation, timeout time.Duration) []probe {
	out := make([]probe, len(due))
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i, d := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = observe(ctx, net.JoinHostPort(d.Address, port), d.ServerName, timeout)
		}()
	}
	wg.Wait()
	return out
}

// observation joins what the handshake produced to the identity of the queue
// entry that asked for it.
//
// The address, the family and the server name are stored on the row alongside
// the certificate, rather than left to a join: a row has to explain itself. A
// history entry that says "expired" without saying which listener it was read
// from and which name was asked for is not an answer to the question anybody
// opens the history to ask, and the node it names may have been deleted since.
//
// The ownership columns come off the queue entry, which the database itself
// materialized from the domain. The composite foreign keys refuse a row whose
// account disagrees with its domain's, so this is belt and braces.
func (p probe) observation(d store.DueObservation, at time.Time) model.CertificateObservation {
	return model.CertificateObservation{
		AccountID:  d.AccountID,
		ProjectID:  d.ProjectID,
		DomainID:   d.DomainID,
		NodeID:     d.NodeID,
		Family:     d.Family,
		Address:    d.Address,
		ServerName: d.ServerName,
		ObservedAt: at,

		Outcome:       string(p.outcome),
		FailureDetail: p.detail,

		NotBefore:        p.notBefore,
		NotAfter:         p.notAfter,
		Issuer:           p.issuer,
		Serial:           p.serial,
		SANs:             model.StringArray(p.sans),
		CoversServerName: p.coversServerName,
		ChainTrusted:     p.chainTrusted,
	}
}
