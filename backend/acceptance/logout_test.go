//go:build acceptance

package acceptance

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSignOutEndsBothSessions covers authentik-and-self-signup task 5b.5.
//
// SIGNING OUT IS TWO SESSIONS, and only one of them is ours. Revoking our own
// and leaving the provider's standing turns sign-out into "signed out until you
// press sign in": the next person at a shared machine is one click from being
// the previous one, with no password. That failure is invisible from our side —
// our cookie is gone, our /me answers 401, everything we own looks correct — so
// it is asserted here or nowhere.
//
// Three separate faults produced it on 2026.8, and each is pinned below:
//
//   - The provider's invalidation flow was `default-provider-invalidation-flow`,
//     which ships with NO stages: it ends the application session, leaves
//     authentik's own, and redirects. Correct for an SSO estate, wrong here.
//   - authentik refuses a logout carrying a post_logout_redirect_uri without an
//     `id_token_hint` and does not accept client_id in its place, citing OIDC
//     certification. Without the hint the request is a 400 — a visible "bad
//     request" rather than a silent wrong landing.
//   - The post-logout address must be registered as a LOGOUT redirect URI.
//     Registered as an authorization one it is not a valid destination.
func TestSignOutEndsBothSessions(t *testing.T) {
	s, err := signIn(adminUser, devPassword)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	// Our session works before signing out, so a 401 afterwards means something.
	if status, _, err := s.do(http.MethodGet, "/api/v1/me", nil); err != nil || status != http.StatusOK {
		t.Fatalf("/me before sign-out = %d, %v; want 200", status, err)
	}

	// The browser form of sign-out: a GET that answers a redirect rather than
	// following it, so the destination can be read.
	noFollow := *s.client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequest(http.MethodGet, consoleURL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatalf("build logout request: %v", err)
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("sign-out returned no redirect; the provider's session is only ended by visiting its end-session endpoint")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parsing the sign-out redirect %q: %v", loc, err)
	}
	if !strings.Contains(u.Path, "end-session") {
		t.Errorf("sign-out redirects to %q, want the provider's end-session endpoint — "+
			"revoking our own session alone leaves the provider's standing", u.Path)
	}
	q := u.Query()
	if q.Get("id_token_hint") == "" {
		t.Error("no id_token_hint on the end-session request. authentik requires it whenever a " +
			"post_logout_redirect_uri is present (OIDC certification) and refuses the request " +
			"outright without it — the symptom is a bare \"bad request\" page")
	}
	if q.Get("post_logout_redirect_uri") == "" {
		t.Error("no post_logout_redirect_uri; sign-out would land on the provider's own page")
	}

	// Our session is gone regardless of what the provider does next.
	if status, _, err := s.do(http.MethodGet, "/api/v1/me", nil); err != nil || status != http.StatusUnauthorized {
		t.Errorf("/me after sign-out = %d, %v; want 401", status, err)
	}

	// AND THE PROVIDER ACTUALLY ACCEPTS IT. This is the half that was a 400 for
	// most of the afternoon: the request being well-formed on our side says
	// nothing about whether authentik will honour it.
	endResp, err := noFollow.Do(mustRequest(t, http.MethodGet, loc))
	if err != nil {
		t.Fatalf("visiting end-session: %v", err)
	}
	defer endResp.Body.Close()
	if endResp.StatusCode >= 400 {
		t.Errorf("the provider refused the sign-out with %d. A 400 here is the shape of a missing "+
			"or unregistered parameter — check that the post-logout address is registered as a "+
			"LOGOUT redirect URI and not an authorization one", endResp.StatusCode)
	}
}

func mustRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	return req
}
