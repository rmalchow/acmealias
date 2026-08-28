package certobserve

import (
	"context"
	"testing"
	"time"
)

const probeTimeout = 2 * time.Second

// A healthy certificate is read, and the two judgements are made by us.
//
// The chain is self-signed, so it is NOT trusted — and it is still observed,
// still healthy, and still covers the name. That combination is the whole of
// G12 in one assertion: chain trust is recorded as a fact and does not colour
// anything, because "the certificate is valid and nobody trusts the issuer" is a
// real deployment mistake worth seeing rather than an error to abort on.
func TestObserveReadsACertificateWithoutTrustingIt(t *testing.T) {
	addr := serveTLS(t, time.Now().Add(90*24*time.Hour), "shop.example.com")

	p := observe(context.Background(), addr, "shop.example.com", probeTimeout)

	if p.outcome != OutcomeObserved {
		t.Fatalf("outcome = %q, want %q (detail %v)", p.outcome, OutcomeObserved, detail(p))
	}
	if !mustBool(t, "coversServerName", p.coversServerName) {
		t.Error("coversServerName = false, want true")
	}
	if mustBool(t, "chainTrusted", p.chainTrusted) {
		t.Error("chainTrusted = true for a self-signed certificate, want false")
	}
	if p.notAfter == nil || p.issuer == nil || p.serial == nil {
		t.Fatalf("not-after, issuer and serial must all be recorded: %+v", p)
	}
	if got := len(p.sans); got != 1 || p.sans[0] != "shop.example.com" {
		t.Errorf("sans = %v, want [shop.example.com]", p.sans)
	}
	if b := BandFor(p.observation(dueFixture(), time.Now()), time.Now()); b != BandHealthy {
		t.Errorf("band = %q, want %q — an untrusted chain must not colour the dot", b, BandHealthy)
	}
}

// 12.4: an expired certificate is an OBSERVATION, not a failure to observe.
// This is the case a verifying client turns into a handshake error, which is
// why the transport's verification is off (G12) — with it on, this test and the
// blackhole test below would be indistinguishable.
func TestObserveAnExpiredCertificateIsAnObservation(t *testing.T) {
	addr := serveTLS(t, time.Now().Add(-24*time.Hour), "shop.example.com")

	p := observe(context.Background(), addr, "shop.example.com", probeTimeout)

	if p.outcome != OutcomeObserved {
		t.Fatalf("outcome = %q, want %q — an expired certificate was READ, not missed (detail %v)",
			p.outcome, OutcomeObserved, detail(p))
	}
	if p.notAfter == nil || p.notAfter.After(time.Now()) {
		t.Fatalf("not-after = %v, want a past instant", p.notAfter)
	}
	if b := BandFor(p.observation(dueFixture(), time.Now()), time.Now()); b != BandExpired {
		t.Errorf("band = %q, want %q", b, BandExpired)
	}
}

// 12.6: a certificate that does not cover the name asked for bands with expired
// — red — whatever its remaining lifetime. Ninety days here, so nothing but
// coverage can be producing the band.
func TestObserveACertificateForOtherNamesIsUncovered(t *testing.T) {
	addr := serveTLS(t, time.Now().Add(90*24*time.Hour), "other.example.com")

	p := observe(context.Background(), addr, "shop.example.com", probeTimeout)

	if p.outcome != OutcomeObserved {
		t.Fatalf("outcome = %q, want %q (detail %v)", p.outcome, OutcomeObserved, detail(p))
	}
	if mustBool(t, "coversServerName", p.coversServerName) {
		t.Fatal("coversServerName = true for a certificate naming only other.example.com")
	}
	if b := BandFor(p.observation(dueFixture(), time.Now()), time.Now()); b != BandUncovered {
		t.Errorf("band = %q, want %q — coverage outranks a long remaining life", b, BandUncovered)
	}
}

// A wildcard's test hostname is matched the way a TLS client matches it, which
// is why coverage goes through VerifyHostname rather than a scan of the SAN
// list: one label beneath the wildcard is covered and two are not, and a
// hand-rolled matcher gets the second half wrong.
func TestObserveWildcardCoverageFollowsTheTLSRules(t *testing.T) {
	addr := serveTLS(t, time.Now().Add(90*24*time.Hour), "*.example.com")

	for _, tc := range []struct {
		serverName string
		want       bool
	}{
		{"www.example.com", true},
		{"a.b.example.com", false},
		{"example.com", false},
	} {
		p := observe(context.Background(), addr, tc.serverName, probeTimeout)
		if p.outcome != OutcomeObserved {
			t.Fatalf("%s: outcome = %q (detail %v)", tc.serverName, p.outcome, detail(p))
		}
		if got := mustBool(t, "coversServerName", p.coversServerName); got != tc.want {
			t.Errorf("%s: coversServerName = %v, want %v", tc.serverName, got, tc.want)
		}
	}
}

// 12.9: a refused connection and a timeout each produce a stored observation
// carrying a classification, and both read as "we could not look" rather than
// as "there is no certificate installed".
func TestObserveClassifiesTheTwoWaysAHandshakeDoesNotHappen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		addr    string
		timeout time.Duration
		want    Outcome
	}{
		{"refused", deadPort(t), probeTimeout, OutcomeRefused},
		// A short timeout, because the point is that the deadline fires: the
		// dial to silentPort SUCCEEDS and the handshake never completes, which
		// is the blackhole a dial-only timeout would not bound.
		{"timed out", silentPort(t), 200 * time.Millisecond, OutcomeTimedOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := observe(context.Background(), tc.addr, "shop.example.com", tc.timeout)

			if p.outcome != tc.want {
				t.Fatalf("outcome = %q, want %q (detail %v)", p.outcome, tc.want, detail(p))
			}
			if p.detail == nil || *p.detail == "" {
				t.Error("no failure detail recorded; the operator has nothing to read")
			}
			// Nothing may be invented about a certificate we never saw.
			if p.notAfter != nil || p.issuer != nil || p.coversServerName != nil || p.chainTrusted != nil {
				t.Errorf("certificate fields set on a failed handshake: %+v", p)
			}

			o := p.observation(dueFixture(), time.Now())
			if b := BandFor(o, time.Now()); b != BandUnexamined {
				t.Errorf("band = %q, want %q — a failed probe is grey, never red", b, BandUnexamined)
			}
			// The row exists (G11). A gap in the table would be
			// indistinguishable from a scheduler that did not run.
			if o.Outcome == "" || o.Address == "" || o.ServerName == "" {
				t.Errorf("failed handshake did not produce a self-explaining row: %+v", o)
			}
		})
	}
}

func detail(p probe) string {
	if p.detail == nil {
		return "<none>"
	}
	return *p.detail
}
