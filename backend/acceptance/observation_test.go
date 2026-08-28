//go:build acceptance

package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The certificate observer, against a real certificate on a real listener
// (change domain-diagnostics, task 12.15).
//
// This is the most valuable test in the change and it is achievable end to end
// only because the dev stack already holds the whole loop: traefik-lb terminates
// TLS on the published :443 with a certificate PEBBLE ISSUED THROUGH THIS
// SERVICE'S OWN CHALLENGE ANSWERING. So the thing being observed is not a
// fixture — it is the output of the arc cert_test.go asserts, looked at from the
// other end.

// --- the wire shapes -------------------------------------------------------

// certState is GET /accounts/{a}/domains/{id}/certificate: the dot, and the rows
// that explain it (5.2).
type certState struct {
	ServerName string           `json:"serverName"`
	Band       string           `json:"band"`
	Addresses  []observationRow `json:"addresses"`
}

// observationRow is one (node, family) pair's observation. It is the same object
// in the state and in the history, which is a property this suite asserts rather
// than assumes — a console renders one dialog from both.
//
// Everything from NotAfter down is a pointer because it is null unless Outcome
// is "observed": a handshake we could not complete has no certificate to
// describe, and a zero time would read as one that expired in year zero.
type observationRow struct {
	NodeID           string     `json:"nodeId"`
	NodeLabel        string     `json:"nodeLabel"`
	Family           string     `json:"family"`
	Address          string     `json:"address"`
	ObservedAt       time.Time  `json:"observedAt"`
	Outcome          string     `json:"outcome"`
	Band             string     `json:"band"`
	NotAfter         *time.Time `json:"notAfter"`
	Issuer           *string    `json:"issuer"`
	CoversServerName *bool      `json:"coversServerName"`
	ChainTrusted     *bool      `json:"chainTrusted"`
}

func (r observationRow) key() string { return r.NodeID + "/" + r.Family + "/" + r.Address }

// TestTheServedCertificateIsObservedAndBanded is 12.15, and G12 for free.
//
// Two assertions carry it. The first is that the certificate traefik-lb is
// actually serving comes back read, banded and `coversServerName` true — the
// happy path, through a real handshake against a real load balancer.
//
// THE BAND ASSERTED IS DERIVED AND NOT WRITTEN DOWN, and that is not a hedge.
// Pebble issues certificates with a few days of life in them, so the seeded
// domain's dot in this stack is amber or red rather than green — asserting
// "healthy" would be asserting the fake CA's lifetime policy, and would fail the
// day it changed for a reason that has nothing to do with the observer. What is
// asserted instead is that the band is the one the certificate ON THE WIRE
// implies, at the edges the design fixes (28 days, 7 days): that catches a band
// computed from the wrong field, from the observation's own age, or from
// thresholds that have quietly moved.
//
// The second is the one that inverts the day somebody "fixes" the deliberate
// InsecureSkipVerify in internal/certobserve: `chainTrusted` FALSE beside a band
// that was read off a certificate we could plainly parse. Pebble's root is not in the api container's system pool, so an
// observer that verified would refuse this handshake and report "we could not
// look" for a certificate that is perfectly good. That the two facts are
// reported SEPARATELY — the band from the dates and the coverage, the trust as a
// flag beside it — is design 3.3 and G12, and this is the only place either is
// proved against a certificate nobody synthesised.
func TestTheServedCertificateIsObservedAndBanded(t *testing.T) {
	// The precondition, stated rather than assumed: what is on :443 is the fake
	// CA's certificate and not Traefik's self-signed fallback. Without it a
	// failure here would read as an observer bug when nothing was ever issued.
	if !issuedByPebble(demoDomain) {
		t.Fatalf("traefik-lb is not serving a pebble-issued certificate for %s; there is "+
			"nothing to observe (./scripts/dev.sh logs traefik-lb)", demoDomain)
	}

	s := mustSignInSeeded(t, adminUser)
	project := demoProject(t, s)
	domain := domainNamed(t, s, project, demoDomain)

	state := observeDomain(t, s, project, domain.ID)

	if state.ServerName != demoDomain {
		t.Errorf("the observation asked for %q, want %s — the name every address below was "+
			"asked is what makes an `uncovered` answer readable", state.ServerName, demoDomain)
	}

	// The row for the address that actually answers.
	var served *observationRow
	for i, a := range state.Addresses {
		if a.Address == lbNodeAddress {
			served = &state.Addresses[i]
		}
	}
	if served == nil {
		t.Fatalf("no observation of %s; the pass reached %d address(es): %+v",
			lbNodeAddress, len(state.Addresses), state.Addresses)
	}

	if served.Outcome != "observed" {
		t.Fatalf("the handshake with %s came back %q rather than a certificate — the observer "+
			"runs inside the api container and traefik-lb answers on the stack's own network, "+
			"so this is a real failure and not an unreachable fixture: %+v",
			lbNodeAddress, served.Outcome, *served)
	}
	if served.NotAfter == nil {
		t.Fatalf("the certificate was observed and carries no notAfter; there is then nothing "+
			"to band it against: %+v", *served)
	}
	if want := bandForRemainingLife(time.Until(*served.NotAfter)); served.Band != want {
		t.Errorf("the certificate expires %s — %s from now — and bands %q, want %q",
			served.NotAfter, time.Until(*served.NotAfter).Round(time.Hour), served.Band, want)
	}
	if served.CoversServerName == nil || !*served.CoversServerName {
		t.Errorf("the certificate served for %s does not cover the name it was asked for "+
			"(coversServerName %v) — that is the `issued and never deployed` failure, and "+
			"here it would mean the observation is of something other than traefik-lb",
			demoDomain, served.CoversServerName)
	}
	if served.Issuer == nil || !strings.Contains(strings.ToLower(*served.Issuer), "pebble") {
		t.Errorf("the observed issuer is %v, want pebble's — the observation is then not of "+
			"the certificate this stack issued", served.Issuer)
	}

	// G12, and the reason this test is worth its weight.
	if served.ChainTrusted == nil {
		t.Errorf("chainTrusted is null on an observed certificate; it is a fact about every " +
			"chain we read and is recorded whether or not it colours anything")
	} else if *served.ChainTrusted {
		t.Errorf("the observer reports pebble's chain as TRUSTED. Pebble's root is not in the " +
			"api container's system pool, so this can only mean the observer began verifying " +
			"— at which point every customer using a private CA, or serving a chain we cannot " +
			"build, reports `we could not look` instead of the certificate we can plainly " +
			"read. The InsecureSkipVerify in internal/certobserve is deliberate (3.3, G12) " +
			"and the untrusted chain is recorded beside the certificate rather than in place " +
			"of it")
	}

	// The dot itself, which is the worst-of fold — and its one exception (G10,
	// 12.7). The seeded nodes carry TEST-NET-3 addresses that answer nothing, so
	// this domain is one real certificate beside two addresses we could not
	// reach. The dot takes the CERTIFICATE'S band: grey never outranks a colour,
	// or a customer whose IPv6 listener sits behind a firewall that drops us sees
	// a permanently grey dot for a domain whose certificates are fine, and learns
	// to ignore the dot.
	if state.Band != served.Band {
		t.Errorf("the domain's dot is %q while its only reachable address reads %q: %+v — an "+
			"address we could not reach is a line in the dialog, not a reason to grey out a "+
			"dot that has real information in it (G10)", state.Band, served.Band, state.Addresses)
	}
	if len(state.Addresses) < 2 {
		t.Errorf("only %d address(es) were observed for %s; the addresses we could NOT reach "+
			"belong in the dialog too, or the dot says green and the list cannot explain why "+
			"a visitor was refused", len(state.Addresses), demoDomain)
	}
}

// bandForRemainingLife is what a certificate with this much life left must band
// as, at the edges design G9 fixes: more than 28 days green, 7 to 28 amber, 7 or
// fewer red, and past its date red for a different reason.
//
// It is written out here rather than imported from internal/certobserve on
// purpose. Importing would make this test agree with the implementation by
// construction and assert nothing about the thresholds; a second, independent
// statement of them is what makes a moved edge a failing test. The band edges
// themselves are covered exhaustively a level down, in certobserve's own table
// (12.8) — this is the one place they are checked against a real certificate.
func bandForRemainingLife(left time.Duration) string {
	switch {
	case left <= 0:
		return "expired"
	case left <= 7*24*time.Hour:
		return "urgent"
	case left <= 28*24*time.Hour:
		return "approaching"
	default:
		return "healthy"
	}
}

// --- driving the observer --------------------------------------------------

// lbNodeAddress is traefik-lb on the stack's own network
// (development/compose.yaml). The observer dials the ADDRESS and presents the
// domain's name as SNI, so this is what has to be in the inventory for anything
// to be observed — and it is reachable from the api container and from nowhere
// on the host but :443.
const lbNodeAddress = "172.28.0.30"

// observeDomain gives a project an address that actually answers TLS, switches
// observation on for one of its domains, runs one pass, and reads the state
// back. Both the node and the switch are undone on cleanup.
//
// It is here rather than in each test because three tests want it — the seeded
// project's, the personal account's and the team account's — and the sequence
// has one non-obvious step in it, which is how a pass is triggered at all.
func observeDomain(t *testing.T, s *session, project, domainID string) certState {
	t.Helper()

	var node struct {
		ID string `json:"id"`
	}
	if err := s.json(http.MethodPost, s.acct("/projects/"+project+"/nodes"),
		map[string]string{"label": unique("tls"), "ipv4": lbNodeAddress},
		http.StatusCreated, &node); err != nil {
		t.Fatalf("adding the load balancer as a node: %v", err)
	}
	t.Cleanup(func() { _, _, _ = s.do(http.MethodDelete, s.acct("/nodes/"+node.ID), nil) })

	setObservation(t, s, domainID, true)
	t.Cleanup(func() { setObservation(t, s, domainID, false) })

	observationPass(t)

	var state certState
	eventually(t, 2*time.Minute, "an observation of "+lbNodeAddress, func() error {
		state = certificateState(t, s, domainID)
		for _, a := range state.Addresses {
			if a.Address == lbNodeAddress {
				return nil
			}
		}
		return fmt.Errorf("the pass has observed %d address(es), none of them %s",
			len(state.Addresses), lbNodeAddress)
	})
	return state
}

// observationPass makes one observation pass happen NOW, by restarting the api.
//
// THE LEVER IS A RESTART AND NOT A SETTING, which is worth stating because the
// obvious alternative does not exist: certobserve.Run does a pass immediately
// rather than after its first tick, and the cadence is floored at one hour by
// config.Validate — which REFUSES a shorter value rather than clamping it (4.5b,
// 12.14). Observation is unmetered, so that floor is the only thing bounding
// what this service aims at somebody else's machine, and a test that wanted it
// lowered would be a test asking for the bound to be removable.
//
// The restart is seconds: the api container runs `air` against a build cache
// that is already warm, and the sessions this suite holds live in the database
// rather than in the process.
func observationPass(t *testing.T) {
	t.Helper()
	compose(t, "restart", "api")
	eventually(t, 2*time.Minute, "the api answering again after a restart", func() error {
		return expectStatus(consoleURL+"/api/v1/me", http.StatusUnauthorized)
	})
}

// setObservation is PATCH /domains/{id}, the one write this change adds to a
// domain (2.7, 2.8). No tlsTestHost: every domain observed in this suite is a
// specific host, and a hostname is never derived from one (2.4).
func setObservation(t *testing.T, s *session, domainID string, on bool) {
	t.Helper()
	if err := s.json(http.MethodPatch, s.acct("/domains/"+domainID),
		map[string]any{"observeTls": on}, http.StatusOK, nil); err != nil {
		t.Fatalf("setting observeTls=%v on %s: %v", on, domainID, err)
	}
}

func certificateState(t *testing.T, s *session, domainID string) certState {
	t.Helper()
	var out certState
	if err := s.getJSON(s.acct("/domains/"+domainID+"/certificate"), &out); err != nil {
		t.Fatalf("reading the certificate state of %s: %v", domainID, err)
	}
	if out.Addresses == nil {
		t.Errorf("the certificate state answers a null `addresses` for %s; it is [] when there "+
			"is nothing, because a console that has to guard for null renders a crash instead "+
			"of an empty dialog", domainID)
	}
	return out
}

// domainNamed resolves one domain of a project by name to the object the API
// addresses it by.
func domainNamed(t *testing.T, s *session, project, name string) domainDetail {
	t.Helper()
	for _, d := range projectDomains(t, s, project) {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("the project %s holds no domain named %s", project, name)
	return domainDetail{}
}

// historyRows reads GET …/certificate/history and insists it is a BARE ARRAY.
//
// Not an object with a `rows` key: the history is the same objects as the state
// carries, newest first, and nothing else (5.3). Decoding it into a slice is
// what asserts that — a wrapper object would fail here rather than being
// silently tolerated by a struct that ignores what it does not know.
func historyRows(t *testing.T, s *session, domainID string) []observationRow {
	t.Helper()
	status, raw, err := s.do(http.MethodGet, s.acct("/domains/"+domainID+"/certificate/history"), nil)
	if err != nil {
		t.Fatalf("reading the observation history of %s: %v", domainID, err)
	}
	if status != http.StatusOK {
		t.Fatalf("reading the observation history of %s: got %d, want 200: %s", domainID, status, raw)
	}
	var out []observationRow
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the observation history is not a bare array of observations: %v\n%s", err, raw)
	}
	// Newest first. One pass writes every row of a batch at one instant, so with
	// a single pass this is an ordering the data cannot contradict; what it
	// catches is the sort being turned around, which is the change somebody makes
	// while "fixing" a dialog that scrolls the wrong way. The ordering itself is
	// pinned where it can be: store.ObservationHistory's own test.
	for i := 1; i < len(out); i++ {
		if out[i].ObservedAt.After(out[i-1].ObservedAt) {
			t.Errorf("the history runs oldest-first at row %d (%s after %s); the dialog opens "+
				"on what is true NOW", i, out[i].ObservedAt, out[i-1].ObservedAt)
			break
		}
	}
	return out
}
