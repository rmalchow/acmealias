package certobserve

import (
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// 12.8: every band edge in one table, not six tests. The edges are the thing
// most likely to be adjusted by somebody who has not read G9, and a table is
// where the whole rule is visible at once.
//
// Twenty-eight rather than thirty is deliberate: every ACME client renews at 30
// by default, so 29 days left means "told to renew, has not yet", which is
// exactly when amber starts being informative.
func TestBandEdges(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	for _, tc := range []struct {
		daysLeft float64
		want     Band
	}{
		{29, BandHealthy},     // just outside the renewal window every client uses
		{28.001, BandHealthy}, // the edge itself belongs to amber, so a hair over is green
		{28, BandApproaching},
		{8, BandApproaching},
		{7, BandUrgent},
		{6, BandUrgent},
		{0.001, BandUrgent}, // still valid, barely
		{0, BandExpired},
		{-1, BandExpired},
	} {
		notAfter := now.Add(time.Duration(tc.daysLeft * float64(day)))
		o := observedFixture(notAfter, true)
		if got := BandFor(o, now); got != tc.want {
			t.Errorf("%v days left: band = %q, want %q", tc.daysLeft, got, tc.want)
		}
	}
}

// Coverage outranks lifetime, in both directions: a long-lived certificate for
// the wrong names is red, and the band says WHICH red so the dialog can explain
// itself.
func TestBandUncoveredBeatsAnyRemainingLifetime(t *testing.T) {
	now := time.Now()
	if got := BandFor(observedFixture(now.Add(365*24*time.Hour), false), now); got != BandUncovered {
		t.Errorf("band = %q, want %q", got, BandUncovered)
	}
	// And an uncovered certificate that is ALSO expired reports uncovered
	// rather than expired: they rank the same, and coverage is the more
	// actionable of the two things to be told.
	if got := BandFor(observedFixture(now.Add(-24*time.Hour), false), now); got != BandUncovered {
		t.Errorf("expired and uncovered: band = %q, want %q", got, BandUncovered)
	}
}

// 12.7: the dot is the worst address — with the one exception that grey never
// outranks a colour.
func TestWorstAcrossAddresses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bands []Band
		want  Band
	}{
		{"nothing observed at all", nil, BandUnobserved},
		{"all healthy", []Band{BandHealthy, BandHealthy}, BandHealthy},
		{"one amber among green", []Band{BandHealthy, BandApproaching, BandHealthy}, BandApproaching},
		{"one red among amber", []Band{BandApproaching, BandUrgent}, BandUrgent},
		{"expired outranks urgent", []Band{BandUrgent, BandExpired}, BandExpired},
		{"uncovered ranks with expired", []Band{BandHealthy, BandUncovered}, BandUncovered},

		// THE EXCEPTION, and the reason Worst is a function rather than a max.
		// One address we could not reach beside three we could is a line in the
		// dialog, not a reason to grey out a dot that has real information in
		// it — otherwise a customer whose IPv6 listener is behind a firewall
		// that drops us sees a permanently grey dot for a domain that is fine,
		// and learns to ignore the dot.
		{"one unreachable beside three healthy", []Band{
			BandHealthy, BandHealthy, BandHealthy, BandUnexamined}, BandHealthy},
		{"one unreachable beside one red", []Band{BandUnexamined, BandExpired}, BandExpired},

		// Grey survives only when it is all there is, and the two greys are
		// distinct: "we have not looked yet" is not "we looked and could not
		// see".
		{"every address unreachable", []Band{BandUnexamined, BandUnexamined}, BandUnexamined},
	} {
		if got := Worst(tc.bands...); got != tc.want {
			t.Errorf("%s: Worst(%v) = %q, want %q", tc.name, tc.bands, got, tc.want)
		}
	}
}

// State bands the addresses and the dot from one input, which is what stops a
// read publishing a dot no address in its own list justifies. The unreachable
// address is still in the per-address list — the dialog shows every address,
// including the ones the dot ignores (G10).
func TestStateKeepsTheUnreachableAddressInTheList(t *testing.T) {
	now := time.Now()
	obs := []model.CertificateObservation{
		observedFixture(now.Add(90*24*time.Hour), true),
		{Outcome: string(OutcomeRefused)},
	}

	per, dot := State(obs, now)

	if dot != BandHealthy {
		t.Errorf("dot = %q, want %q", dot, BandHealthy)
	}
	if len(per) != 2 || per[1] != BandUnexamined {
		t.Errorf("per-address bands = %v, want the unreachable address kept as %q", per, BandUnexamined)
	}
}

// observedFixture is a successful observation of a certificate with the given
// expiry and coverage — the only two things banding looks at.
func observedFixture(notAfter time.Time, covers bool) model.CertificateObservation {
	return model.CertificateObservation{
		Outcome:          string(OutcomeObserved),
		NotAfter:         &notAfter,
		CoversServerName: &covers,
	}
}
