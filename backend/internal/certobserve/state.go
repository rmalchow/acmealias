package certobserve

import (
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Band is the state of one observation, or of a whole domain, in the vocabulary
// the console and adminctl both render.
//
// IT IS A STRING ON THE WIRE AND NOTHING DOWNSTREAM COMPUTES ONE. The bands are
// derived here, once, and put into the JSON verbatim, so that no client applies
// its own thresholds — the alternative is a console and an adminctl that disagree
// about the same certificate because one of them was updated and the other was
// not, which is the kind of bug a customer reports and nobody can reproduce.
type Band string

const (
	// BandHealthy is more than 28 days of remaining life. Green.
	BandHealthy Band = "healthy"
	// BandApproaching is 7 to 28 days. Amber.
	BandApproaching Band = "approaching"
	// BandUrgent is 7 days or fewer, and not yet expired. Red.
	BandUrgent Band = "urgent"
	// BandExpired is a certificate whose not-after has passed. Red.
	BandExpired Band = "expired"
	// BandUncovered is a certificate that does not cover the name we asked for,
	// whatever its remaining lifetime. Red, and it is the most valuable red in
	// the feature: it is the "issued and never deployed" failure, the "deployed
	// to two nodes out of four" failure and the "covers the names you had last
	// month" failure, every one of which the challenge history reports as a
	// success. It is a band of its own rather than folded into expired so that
	// the dialog can say which of the two it is, and it ranks with expired
	// because a visitor's browser refuses both (G9).
	BandUncovered Band = "uncovered"
	// BandUnexamined is a handshake we attempted and could not complete. Grey.
	//
	// Grey and not red. This is the one place where the failure of OUR probe
	// meets a colour scale, and red would claim the customer's node is broken
	// when we cannot tell that apart from our own transit, their firewall
	// dropping our source address, or a listener that is simply not on 443.
	// Grey says "we could not look", which is the only thing we know.
	BandUnexamined Band = "unexamined"
	// BandUnobserved is no observation at all: the switch was just turned on and
	// no cycle has run yet, or observation is off for the deployment. Grey, and
	// distinct from unexamined because "we have not looked yet" and "we looked
	// and could not see" are different things to be told.
	BandUnobserved Band = "unobserved"
)

// BandFor is the band of ONE observation, judged as of now.
//
// The freshness of the observation is deliberately not part of this. An
// observation is a fact about the moment it was taken, and a band that decayed
// on its own age would have the worker's cadence leaking into a statement about
// a certificate. What is stale is visible from ObservedAt, which every read
// carries beside the band.
func BandFor(o model.CertificateObservation, now time.Time) Band {
	if Outcome(o.Outcome) != OutcomeObserved {
		// Refused, timed out, rejected, unreadable — all four are "we could not
		// look", and the classification is what the dialog shows.
		return BandUnexamined
	}
	// A certificate that does not cover the name asked for is red whatever its
	// remaining lifetime, so coverage is judged before expiry: a freshly issued
	// certificate for the wrong names is a worse position than an old one for
	// the right ones, and reporting it as healthy would be the single most
	// misleading thing this feature could say.
	if o.CoversServerName == nil || !*o.CoversServerName {
		return BandUncovered
	}
	if o.NotAfter == nil {
		// Observed, and yet nothing to band against. Not reachable from this
		// package's own writes; a row edited or migrated into that state is
		// something we could not read rather than something healthy.
		return BandUnexamined
	}
	switch left := o.NotAfter.Sub(now); {
	case left <= 0:
		return BandExpired
	case left <= urgentWithin:
		return BandUrgent
	case left <= approachingWithin:
		return BandApproaching
	default:
		return BandHealthy
	}
}

// Worst folds the per-address bands of one domain into the single band its dot
// takes (G10).
//
// The rule is worst-first, with ONE exception that is the whole reason this is a
// function rather than a max: GREY NEVER OUTRANKS A COLOUR. An address we could
// not reach beside three we could is a line in the dialog, not a reason to grey
// out a dot that has real information in it — a customer whose IPv6 listener is
// behind a firewall that drops us would otherwise see a permanently grey dot for
// a domain whose certificates are fine, and would learn to ignore the dot.
//
// The exception falls out of the ranking rather than being a special case in the
// code: grey ranks BELOW green, so any colour present wins, and grey survives
// only when it is all there is.
//
// No observations at all is BandUnobserved — the switch was turned on and no
// cycle has run, which is not the same as having looked and failed.
func Worst(bands ...Band) Band {
	worst := BandUnobserved
	for _, b := range bands {
		// Strictly greater, so the FIRST band of a tied severity wins and the
		// answer is deterministic: the caller passes them in node order, family
		// within node, and expired and uncovered rank equally.
		if severity(b) > severity(worst) {
			worst = b
		}
	}
	return worst
}

// severity orders the bands for Worst. Expired and uncovered share a rank
// because both are red and neither is worse than the other.
//
// BOTH GREYS SIT BELOW EVERY COLOUR, which is what implements "grey never
// outranks a colour" without a special case. They are not equal to each other,
// though: an address we tried and could not reach outranks one we have not
// looked at yet, so a domain whose every address failed reads "we could not
// look" rather than "we have not looked", which are different things to be told.
func severity(b Band) int {
	switch b {
	case BandExpired, BandUncovered:
		return 5
	case BandUrgent:
		return 4
	case BandApproaching:
		return 3
	case BandHealthy:
		return 2
	case BandUnexamined:
		return 1
	default: // BandUnobserved, and anything unrecognised
		return 0
	}
}

// State bands every observation and returns both halves of what a read needs:
// the per-address bands in the order they were given, and the domain's dot.
//
// The two are returned together because they must be computed from the same
// input at the same moment — a caller that banded the addresses, then called
// Worst on a freshly read set, could publish a dot that no address in its own
// list justifies.
func State(obs []model.CertificateObservation, now time.Time) (per []Band, dot Band) {
	per = make([]Band, len(obs))
	for i, o := range obs {
		per[i] = BandFor(o, now)
	}
	return per, Worst(per...)
}
