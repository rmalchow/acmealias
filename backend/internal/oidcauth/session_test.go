package oidcauth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// assertCookieAttrs checks the attributes design D10 requires of every cookie
// this package sets: HttpOnly, Secure, SameSite=Lax and host-only.
func assertCookieAttrs(t *testing.T, c *http.Cookie) {
	t.Helper()
	if !c.HttpOnly {
		t.Errorf("%s: HttpOnly is not set", c.Name)
	}
	if !c.Secure {
		t.Errorf("%s: Secure is not set", c.Name)
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("%s: SameSite = %v, want Lax", c.Name, c.SameSite)
	}
	if c.Domain != "" {
		t.Errorf("%s: Domain = %q, want host-only", c.Name, c.Domain)
	}
	if c.Path != "/" {
		t.Errorf("%s: Path = %q, want /", c.Name, c.Path)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	assertCookieAttrs(t, newCookie(SessionCookieName, "value", time.Hour))

	cleared := clearCookie(SessionCookieName)
	assertCookieAttrs(t, cleared)
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Errorf("clearCookie = %+v, want an empty value and a negative MaxAge", cleared)
	}
}

func TestRandomTokenIsUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok := randomToken()
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token %q is not URL-safe base64: %v", tok, err)
		}
		if len(raw) != 32 {
			t.Fatalf("token carries %d bytes, want 32 (256 bits)", len(raw))
		}
		if seen[tok] {
			t.Fatalf("token %q repeated", tok)
		}
		seen[tok] = true
	}
}

func TestCheckSession(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Minute)
	const idle = time.Hour

	live := func() *model.Session {
		return &model.Session{
			ID:         "s1",
			CreatedAt:  now.Add(-2 * time.Hour),
			LastSeenAt: now.Add(-time.Minute),
			ExpiresAt:  now.Add(6 * time.Hour),
		}
	}

	cases := map[string]struct {
		mutate  func(*model.Session)
		wantErr bool
	}{
		"live":                   {mutate: func(*model.Session) {}},
		"revoked":                {mutate: func(s *model.Session) { s.RevokedAt = &revoked }, wantErr: true},
		"past absolute lifetime": {mutate: func(s *model.Session) { s.ExpiresAt = now.Add(-time.Second) }, wantErr: true},
		"expiring exactly now":   {mutate: func(s *model.Session) { s.ExpiresAt = now }, wantErr: true},
		"idle too long":          {mutate: func(s *model.Session) { s.LastSeenAt = now.Add(-idle - time.Second) }, wantErr: true},
		"idle just short of the timeout": {mutate: func(s *model.Session) {
			s.LastSeenAt = now.Add(-idle + time.Second)
		}},
		// A session may be inside its idle window and still be past its
		// absolute lifetime: the two limits are independent.
		"active but past absolute lifetime": {mutate: func(s *model.Session) {
			s.LastSeenAt = now
			s.ExpiresAt = now.Add(-time.Second)
		}, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := live()
			tc.mutate(s)
			err := checkSession(s, idle, now)
			if tc.wantErr && !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("err = %v, want ErrSessionInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

// --- RP-initiated logout ---------------------------------------------------

// TestEndSessionURLIsBuiltFromDiscovery pins the two things that make sign-out
// end the PROVIDER's session as well as ours: the endpoint comes from the
// discovery document rather than a guessed path, and the request carries no
// id_token_hint — we store no ID token, by design (D10), and must work without
// one.
func TestEndSessionURLIsBuiltFromDiscovery(t *testing.T) {
	idp := newStubIDP(t)
	s := idp.service()

	raw := s.EndSessionURL(context.Background(), "")
	if raw == "" {
		t.Fatal("no end-session URL, want one built from the provider metadata")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if want := idp.srv.URL + "/end-session"; u.Scheme+"://"+u.Host+u.Path != want {
		t.Errorf("endpoint = %q, want %q", u.Scheme+"://"+u.Host+u.Path, want)
	}
	q := u.Query()
	if got := q.Get("post_logout_redirect_uri"); got != s.cfg.PostLogoutRedirectURL {
		t.Errorf("post_logout_redirect_uri = %q, want %q", got, s.cfg.PostLogoutRedirectURL)
	}
	if got := q.Get("client_id"); got != stubClientID {
		t.Errorf("client_id = %q, want %q", got, stubClientID)
	}
	if q.Has("id_token_hint") {
		t.Error("an id_token_hint was sent; the session deliberately stores no ID token")
	}
}

// TestEndSessionURLIsEmptyWhenTheProviderHasNone is the fallback the caller
// turns back into today's behaviour, a redirect to "/". A provider without
// RP-initiated logout is a deployment choice, not an error, so nothing here
// fails.
func TestEndSessionURLIsEmptyWhenTheProviderHasNone(t *testing.T) {
	idp := newStubIDP(t)
	idp.noEndSession = true

	if got := idp.service().EndSessionURL(context.Background(), ""); got != "" {
		t.Errorf("end-session URL = %q, want empty", got)
	}
}

// TestEndSessionURLIsEmptyWithoutALandingPage covers the other half: a
// deployment that clears the setting keeps sign-out local rather than sending
// the browser to a provider that would have nowhere to return it to.
func TestEndSessionURLIsEmptyWithoutALandingPage(t *testing.T) {
	s := newStubIDP(t).service()
	s.cfg.PostLogoutRedirectURL = ""

	if got := s.EndSessionURL(context.Background(), ""); got != "" {
		t.Errorf("end-session URL = %q, want empty", got)
	}
}

// TestEndSessionURLCarriesTheIDTokenHint pins the parameter authentik 2026.8
// makes mandatory.
//
// It is worth a test of its own because its absence fails in a way that reads
// like our bug and is invisible here: the provider answers 400 "bad request" to
// a logout carrying a post_logout_redirect_uri and no hint, and on the version
// before it the same request was accepted and the redirect silently dropped.
// Neither symptom names the missing parameter.
func TestEndSessionURLCarriesTheIDTokenHint(t *testing.T) {
	idp := newStubIDP(t)
	s := idp.service()

	raw := s.EndSessionURL(context.Background(), "an.id.token")
	if raw == "" {
		t.Fatal("no end-session URL, want one built from the provider metadata")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	q := u.Query()
	if got := q.Get("id_token_hint"); got != "an.id.token" {
		t.Errorf("id_token_hint = %q, want the session's stored token", got)
	}
	// Both are still required alongside it: client_id resolves which
	// application's registered post-logout list the URI is validated against.
	if got := q.Get("client_id"); got != s.cfg.OIDCClientID {
		t.Errorf("client_id = %q, want %q", got, s.cfg.OIDCClientID)
	}
	if got := q.Get("post_logout_redirect_uri"); got != s.cfg.PostLogoutRedirectURL {
		t.Errorf("post_logout_redirect_uri = %q, want %q", got, s.cfg.PostLogoutRedirectURL)
	}
}
