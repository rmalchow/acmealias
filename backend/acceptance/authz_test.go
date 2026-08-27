//go:build acceptance

package acceptance

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// Task 2.4: the two refusals, plus the acceptance that keeps the second one
// honest.
//
// This is the one assertion that needs inventory development/scripts/seed.sh
// does not create: proving a key belonging to ANOTHER load balancer is refused
// needs another load balancer, and the seed has exactly one. It is created here
// through the service's own admin API under a real admin session and removed
// again — seed.sh is a human-facing fixture whose output a developer reads on
// every reset, and a load balancer that exists only to be refused is noise in
// it (design A3).

type keyCreated struct {
	Key struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	} `json:"key"`
	Secret string `json:"secret"`
}

// present posts one challenge to the challenge API as an API key would, and
// returns the status. extra carries header forgeries.
func present(t *testing.T, path, id, secret, fqdn, value string, extra map[string]string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"fqdn": fqdn, "value": value})
	req, err := http.NewRequest(http.MethodPost, challengeURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(id, secret)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)) //nolint:errcheck // draining
	return resp.StatusCode
}

// mintKey creates an API key on lb, restricted to networks (nil for none), and
// registers its revocation.
func mintKey(t *testing.T, s *session, lb string, note string, networks []string) keyCreated {
	t.Helper()
	var out keyCreated
	body := map[string]any{"note": note}
	if networks != nil {
		body["networks"] = networks
	}
	if err := s.json(http.MethodPost, s.acct("/loadbalancers/"+lb+"/keys"),
		body, http.StatusCreated, &out); err != nil {
		t.Fatalf("creating key %q: %v", note, err)
	}
	t.Cleanup(func() {
		_, _, _ = s.do(http.MethodDelete, s.acct("/keys/"+out.Key.ID), nil)
	})
	return out
}

func TestChallengeAuthorizationBoundaries(t *testing.T) {
	s := mustSignIn(t, adminUser, devPassword)
	project, lb := demoProd(t, s)

	// --- (a) a valid key belonging to a different load balancer ------------
	var other lbRef
	if err := s.json(http.MethodPost, s.acct("/projects/"+project+"/loadbalancers"),
		map[string]string{"slug": "acceptance-other", "name": "Acceptance (other LB)"},
		http.StatusCreated, &other); err != nil {
		t.Fatalf("creating the second load balancer: %v", err)
	}
	t.Cleanup(func() {
		_, _, _ = s.do(http.MethodDelete, s.acct("/loadbalancers/"+other.ID), nil)
	})
	foreign := mintKey(t, s, other.ID, "acceptance: foreign key", nil)

	const trespass = "acceptance-cross-lb-value-should-never-be-published"
	if got := present(t, "/present", foreign.Key.Identifier, foreign.Secret, challengeName, trespass, nil); got != http.StatusForbidden {
		t.Errorf("a key scoped to another load balancer published %s: got %d, want 403", challengeName, got)
	}
	vals, _, err := txtValues(serviceDNS, aliasFQDN)
	if err != nil {
		t.Fatalf("querying %s: %v", aliasFQDN, err)
	}
	for _, v := range vals {
		if v == trespass {
			t.Fatalf("%s answers a value published by a key from another load balancer", aliasFQDN)
		}
	}

	// --- calibrating the source restriction ---------------------------------
	//
	// The address the service judges a CIDR against is NOT the host's: requests
	// from here reach the challenge API through the dev Traefik overlay, so the
	// peer is a container-network address. Guessing it produces a test that
	// refuses everything and passes for the wrong reason forever, so it is
	// observed: publish once with an unrestricted key and read back the client
	// address the service recorded on the interaction.
	open := mintKey(t, s, lb, "acceptance: calibration", nil)
	if got := present(t, "/present", open.Key.Identifier, open.Secret, challengeName, "acceptance-calibration", nil); got != http.StatusOK {
		t.Fatalf("calibration publish: got %d, want 200", got)
	}
	peer := latestClientIP(t, s, lb)
	if got := present(t, "/cleanup", open.Key.Identifier, open.Secret, challengeName, "acceptance-calibration", nil); got != http.StatusOK {
		t.Fatalf("calibration cleanup: got %d, want 200", got)
	}
	t.Logf("the challenge API observes this test's requests as coming from %s", peer)

	// --- (b) the restriction, in BOTH directions ---------------------------
	inside := mintKey(t, s, lb, "acceptance: inside the range", []string{peer + "/32"})
	if got := present(t, "/present", inside.Key.Identifier, inside.Secret, challengeName, "acceptance-inside", nil); got != http.StatusOK {
		t.Errorf("a key permitting %s/32 was refused from %s: got %d, want 200", peer, peer, got)
	}
	if got := present(t, "/cleanup", inside.Key.Identifier, inside.Secret, challengeName, "acceptance-inside", nil); got != http.StatusOK {
		t.Errorf("cleanup with the in-range key: got %d, want 200", got)
	}

	// 10.99.0.0/16 is nowhere near the stack's 172.28.0.0/24.
	outside := mintKey(t, s, lb, "acceptance: outside the range", []string{"10.99.0.0/16"})
	if got := present(t, "/present", outside.Key.Identifier, outside.Secret, challengeName, "acceptance-outside", nil); got != http.StatusUnauthorized {
		t.Errorf("a key restricted to 10.99.0.0/16 was accepted from %s: got %d, want 401", peer, got)
	}
	// And the same key with the header a caller would forge to look like it is
	// inside the range. It must change nothing.
	if got := present(t, "/present", outside.Key.Identifier, outside.Secret, challengeName, "acceptance-outside",
		map[string]string{"X-Forwarded-For": "10.99.0.1"}); got != http.StatusUnauthorized {
		t.Errorf("a forged X-Forwarded-For defeated the CIDR restriction: got %d, want 401", got)
	}

	// Nothing the refused calls carried may have become answerable.
	vals, _, err = txtValues(serviceDNS, aliasFQDN)
	if err != nil {
		t.Fatalf("querying %s: %v", aliasFQDN, err)
	}
	if len(vals) != 0 {
		t.Fatalf("%s still answers %v after the refusals and cleanups", aliasFQDN, vals)
	}
}

// latestClientIP reads the client address the service recorded on the most
// recent interaction for lb. The interaction history is written by /present
// itself, so this is the address the CIDR check was actually made against.
func latestClientIP(t *testing.T, s *session, lb string) string {
	t.Helper()
	var ins []interactionRow
	if err := s.getJSON(s.acct("/loadbalancers/"+lb+"/interactions?limit=1"), &ins); err != nil {
		t.Fatalf("reading interactions: %v", err)
	}
	if len(ins) == 0 || ins[0].ClientIP == nil {
		t.Fatalf("no interaction with a client address to calibrate against")
	}
	return *ins[0].ClientIP
}
