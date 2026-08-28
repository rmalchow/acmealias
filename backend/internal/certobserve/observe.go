// Package certobserve is the seventh background worker: it completes one TLS
// handshake per (domain, node address, family) for the domains whose owner has
// switched observation on, reads the certificate that was presented, and stores
// what it saw (change domain-diagnostics, design G6, G11, G12, G13).
//
// THIS IS NOT internal/resolvercheck, AND MUST NOT BE MERGED WITH IT. The two
// packages both dial out from a background worker with a bounded timeout, which
// is enough of a resemblance that somebody will propose folding one into the
// other; the reason not to is that they produce different kinds of fact.
// resolvercheck asks a resolver what a customer's zone says right now, and its
// answers are ADVISORY and deliberately persisted nowhere: DNS is somebody
// else's mutable state, and a stored answer would be a claim about a thing we do
// not control that goes stale the moment it is written. An observation here is a
// fact about an exchange THIS SERVICE TOOK PART IN — at a moment, from an
// address, presenting a name — so it is stored, it is shown to the customer as
// history, and a run of them is the trend the whole feature exists to show.
// Same shape of outbound probe, different status; they must not be conflated in
// the code or in anybody's head.
//
// TWO THINGS THIS PACKAGE MUST NEVER BECOME:
//
//   - It never runs on a request path (task 4.6). /present is called by lego in
//     the middle of an issuance, and nothing in this change may be reachable
//     from it. Enforced by construction — certobserve is imported by main.go and
//     by the read path that renders a band, and by nothing in internal/dnsserver
//     — and asserted in deps_test.go, because "by construction" is a property
//     one import statement can quietly end.
//   - Nothing acts on an observation (G14). No answer changes, no request is
//     gated, no domain is deactivated. An observation is written to history and
//     shown to a human, which is what lets one observer be enough.
package certobserve

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// Outcome is this package's classification of what one handshake attempt did.
// It is the vocabulary spelled the way resolvercheck spells Status and
// CAAStatus, and it is a `check` constraint on certificate_observations.outcome
// as well, so a sixth value cannot be written without both halves being changed
// together.
//
// A FAILED HANDSHAKE IS AN OBSERVATION, never an omitted row (G11): "there is no
// certificate here" and "we could not look" are different sentences with
// different audiences, and a gap in a table is indistinguishable from a
// scheduler that did not run. The four failure values are what the dialog shows;
// the underlying error text is kept beside them for the log and is never shown
// to a customer, because it is a verdict on our probe rather than on their node.
type Outcome string

const (
	// OutcomeObserved means a certificate was presented and read. It says
	// nothing about whether that certificate is any good — an expired one and
	// one from an authority nobody trusts are both observed (G12).
	OutcomeObserved Outcome = "observed"
	// OutcomeRefused means the TCP connection did not come up: refused, reset,
	// or a host or network that could not be reached.
	OutcomeRefused Outcome = "refused"
	// OutcomeTimedOut means the attempt ran out its deadline, in the dial or in
	// the handshake. This is the blackhole case, which is why one deadline
	// covers both halves.
	OutcomeTimedOut Outcome = "timed-out"
	// OutcomeRejected means TCP came up and the TLS handshake did not complete:
	// an alert from the peer, a protocol version with nothing in common, or
	// something on 443 that does not speak TLS at all.
	OutcomeRejected Outcome = "rejected"
	// OutcomeUnreadable means the handshake completed and yet there was no
	// certificate to read. It is rare to the point of theoretical and it is a
	// value rather than a panic, because the alternative is a worker that dies
	// on somebody's exotic listener.
	OutcomeUnreadable Outcome = "unreadable"
)

// The two band edges are measured against these. Both are named rather than
// written inline because state.go bands against them and a customer is told the
// numbers in the dialog, so the two must be one definition.
const (
	// urgentWithin is the "renew this now" edge. Below it the dot is red.
	urgentWithin = 7 * 24 * time.Hour
	// approachingWithin is 28 days rather than 30, deliberately (G9). Every
	// ACME client renews at 30 by default, so a certificate with 29 days left
	// has just been TOLD to renew and has not yet — which is exactly when the
	// middle band starts being informative. Thirty would put the whole
	// population in amber for a day at every renewal cycle and mean nothing.
	approachingWithin = 28 * 24 * time.Hour
)

// probe is what one handshake produced, before it is joined to the identity of
// the queue entry that asked for it. Everything read off a certificate is a
// pointer for the same reason it is nullable in the table: on a failed
// handshake there is nothing to record and a zero value would read as a fact.
type probe struct {
	outcome Outcome
	// detail is the underlying error text, for the log. It never reaches a
	// customer — the classification above is what the dialog shows (G11).
	detail *string

	notBefore, notAfter *time.Time
	issuer, serial      *string
	sans                []string
	coversServerName    *bool
	chainTrusted        *bool
}

// observe completes one handshake against addr — a "host:port" whose host is a
// literal ADDRESS — presenting serverName as SNI, and reads what came back.
//
// The address and the name are two arguments and never one. Dialling the name
// would resolve it and observe whatever the customer's DNS points at today,
// which may be a CDN, an old host, or nothing; the question this check exists to
// answer is whether the certificate reached THE NODES CONFIGURED HERE (G6).
//
// One deadline covers the dial AND the handshake, not two. A dial that succeeds
// and a handshake that never completes is precisely the blackhole case, and a
// timeout that only bounds the dial does not bound it.
func observe(ctx context.Context, addr, serverName string, timeout time.Duration) probe {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return failed(classifyDial(err), err)
	}
	defer conn.Close()

	// THE VERIFICATION IS OFF ON PURPOSE AND MUST STAY OFF (G12). Do not
	// "fix" this line. A verifying client ABORTS the handshake on an expired
	// certificate or an untrusted chain and hands back an error — which would
	// collapse the two most interesting findings in this whole feature into the
	// same "we could not look" as a firewall drop, and would make the
	// expired-certificate dot indistinguishable from the blackhole one.
	// Observing means reading what is there, including what a browser would
	// refuse. Expiry, name coverage and chain trust are therefore judged below,
	// by us, on the certificate that was actually presented.
	//
	// This is safe to confine here because this client holds no credentials,
	// sends no request and reads no response body: it completes a handshake and
	// hangs up. Nothing about it puts this service in anybody's trust chain.
	tc := tls.Client(conn, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, //nolint:gosec // G12: see above, this is the point of the package
		MinVersion:         tls.VersionTLS12,
	})
	if err := tc.HandshakeContext(ctx); err != nil {
		if isTimeout(ctx, err) {
			return failed(OutcomeTimedOut, err)
		}
		return failed(OutcomeRejected, err)
	}

	chain := tc.ConnectionState().PeerCertificates
	if len(chain) == 0 {
		return failed(OutcomeUnreadable, errors.New("handshake completed with no certificate"))
	}
	return read(chain, serverName, time.Now().UTC())
}

// read turns a presented chain into the fields that are stored. It is separate
// from the handshake so the judgements can be tested against a certificate built
// in memory, without a listener.
func read(chain []*x509.Certificate, serverName string, now time.Time) probe {
	leaf := chain[0]

	// VerifyHostname rather than a scan of leaf.DNSNames, which is the obvious
	// hand-rolled version and gets wildcards wrong: it is the same matcher a TLS
	// client applies, so "covered" here means covered for the customer's
	// browser too, including the one-label-only rule for *.example.com.
	covers := leaf.VerifyHostname(serverName) == nil

	// Chain trust, judged separately and recorded as a field. It does NOT
	// colour the dot in this change — the dot is about expiry and coverage —
	// and it is recorded because "the certificate is valid and nobody trusts the
	// issuer" is a real deployment mistake and this is where it would be seen.
	//
	// ponytail: judged at `now` against the system pool, so an expired
	// certificate reads as untrusted here as well as expired. Ceiling: the two
	// findings are not fully independent in this field. Nothing is lost by it —
	// the expiry band is computed from NotAfter alone and never from this — and
	// the upgrade path, if a dialog ever wants to say "trusted issuer, expired
	// certificate", is a second Verify at leaf.NotAfter.
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}
	_, verr := leaf.Verify(x509.VerifyOptions{
		Intermediates: intermediates,
		CurrentTime:   now,
		// No DNSName: coverage is the judgement above, and asking Verify for it
		// too would fold a name mismatch into the trust field.
	})
	trusted := verr == nil

	// Hexadecimal, which is how every certificate viewer and every CA's own
	// portal prints a serial, so a customer can compare ours against theirs
	// without converting anything.
	serial := fmt.Sprintf("%x", leaf.SerialNumber)
	issuer := leaf.Issuer.String()
	notBefore, notAfter := leaf.NotBefore.UTC(), leaf.NotAfter.UTC()

	return probe{
		outcome:          OutcomeObserved,
		notBefore:        &notBefore,
		notAfter:         &notAfter,
		issuer:           &issuer,
		serial:           &serial,
		sans:             leaf.DNSNames,
		coversServerName: &covers,
		chainTrusted:     &trusted,
	}
}

// failed builds the observation for an attempt that produced no certificate.
// The error text is kept because an operator reading a run of failures needs it;
// it is kept in one field so that nothing downstream has to decide whether the
// string is safe to show.
func failed(o Outcome, err error) probe {
	detail := err.Error()
	return probe{outcome: o, detail: &detail}
}

// classifyDial separates "we ran out of time" from "the connection was
// actively refused or the network said no". Both are grey to the customer, and
// they are different sentences in the dialog and different shapes of problem to
// an operator: a refusal is a listener that is not there, a timeout is a
// firewall silently dropping us or a node that has started blackholing.
func classifyDial(err error) Outcome {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return OutcomeTimedOut
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimedOut
	}
	// ECONNREFUSED, EHOSTUNREACH, ENETUNREACH and friends all land here, as
	// does a DNS-free dial to an address that simply has no route. There is no
	// value in a fifth outcome per errno: what the customer is told is "we
	// could not connect", and the errno is in detail for whoever cares.
	return OutcomeRefused
}

// isTimeout decides whether a handshake error was the deadline. The context is
// consulted as well as the error because HandshakeContext reports a cancelled
// context as its own error rather than as a net.Error, and the deadline is the
// only reason this package ever cancels one.
func isTimeout(ctx context.Context, err error) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
