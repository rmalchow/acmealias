package oidcauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// startFlow runs Login and returns the flow cookie it set together with the
// state, nonce and verifier inside it.
func startFlow(t *testing.T, s *Service) (*http.Cookie, flowState, *url.URL) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := s.Login(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)); err != nil {
		t.Fatalf("login: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", rec.Code)
	}
	var c *http.Cookie
	for _, got := range rec.Result().Cookies() {
		if got.Name == flowCookieName {
			c = got
		}
	}
	if c == nil {
		t.Fatal("login set no flow cookie")
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		t.Fatalf("decode flow cookie: %v", err)
	}
	var fs flowState
	if err := json.Unmarshal(raw, &fs); err != nil {
		t.Fatalf("unmarshal flow cookie: %v", err)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	return c, fs, loc
}

func callbackRequest(c *http.Cookie, state string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?code=abc&state="+url.QueryEscape(state), nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestLoginRedirectsToProviderWithPKCE(t *testing.T) {
	idp := newStubIDP(t)
	c, fs, loc := startFlow(t, idp.service())

	q := loc.Query()
	for k, want := range map[string]string{
		"response_type":         "code",
		"client_id":             stubClientID,
		"redirect_uri":          "https://console.example.com/api/v1/auth/callback",
		"state":                 fs.State,
		"nonce":                 fs.Nonce,
		"code_challenge_method": "S256",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge") == fs.Verifier {
		t.Errorf("code_challenge = %q, want the S256 hash of the verifier", q.Get("code_challenge"))
	}
	if !strings.HasPrefix(loc.String(), idp.srv.URL+"/auth") {
		t.Errorf("redirect = %q, want the provider's authorization endpoint", loc)
	}

	// The verifier is a secret; the browser must not be able to read it back.
	assertCookieAttrs(t, c)
	if c.MaxAge <= 0 || time.Duration(c.MaxAge)*time.Second > flowCookieTTL {
		t.Errorf("flow cookie MaxAge = %d, want a short positive TTL", c.MaxAge)
	}
}

// TestCallbackRejectsBadTokens covers every way an identity token can fail
// validation. None of them may establish a session — the Service under test
// has a nil database handle, so reaching session creation would panic.
func TestCallbackRejectsBadTokens(t *testing.T) {
	idp := newStubIDP(t)
	other := newStubIDP(t) // a provider with a different signing key

	cases := map[string]struct {
		claims map[string]any
		signer *stubIDP
	}{
		"wrong issuer":   {claims: map[string]any{"iss": "https://evil.example.com"}},
		"wrong audience": {claims: map[string]any{"aud": "some-other-client"}},
		"expired":        {claims: map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}},
		"wrong nonce":    {claims: map[string]any{"nonce": "not-the-nonce-we-sent"}},
		"bad signature":  {signer: other},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := idp.service()
			c, fs, _ := startFlow(t, s)

			claims := map[string]any{"nonce": fs.Nonce}
			for k, v := range tc.claims {
				claims[k] = v
			}
			signer := idp
			if tc.signer != nil {
				signer = tc.signer
				claims["iss"] = idp.srv.URL // right issuer, wrong key
			}
			idp.token = signer.sign(t, claims)

			if _, err := s.verifyCallback(callbackRequest(c, fs.State)); !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("err = %v, want ErrAuthFailed", err)
			}
		})
	}
}

func TestCallbackRejectsStateProblems(t *testing.T) {
	idp := newStubIDP(t)
	s := idp.service()
	c, fs, _ := startFlow(t, s)
	idp.token = idp.sign(t, map[string]any{"nonce": fs.Nonce})

	t.Run("state mismatch", func(t *testing.T) {
		if _, err := s.verifyCallback(callbackRequest(c, "someone-elses-state")); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("err = %v, want ErrAuthFailed", err)
		}
	})
	t.Run("no flow cookie", func(t *testing.T) {
		if _, err := s.verifyCallback(callbackRequest(nil, fs.State)); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("err = %v, want ErrAuthFailed", err)
		}
	})
}

func TestCallbackAcceptsValidTokenAndReadsGroups(t *testing.T) {
	idp := newStubIDP(t)
	s := idp.service()
	c, fs, _ := startFlow(t, s)
	idp.token = idp.sign(t, map[string]any{
		"nonce":  fs.Nonce,
		"sub":    "f:1234:alice",
		"name":   "Alice Example",
		"groups": []string{"/acme/admins", "/acme/readers"},
	})

	id, err := s.verifyCallback(callbackRequest(c, fs.State))
	if err != nil {
		t.Fatalf("verifyCallback: %v", err)
	}
	if id.Subject != "f:1234:alice" || id.DisplayName != "Alice Example" {
		t.Errorf("identity = %+v, want subject f:1234:alice / name Alice Example", id)
	}
	if strings.Join(id.Groups, ",") != "admins,readers" {
		t.Errorf("groups = %v, want the configured prefix stripped", id.Groups)
	}
}

// Discovery must not happen before it is needed: a Service that has never
// signed anyone in has made no call to the provider.
func TestDiscoveryIsLazyAndRetried(t *testing.T) {
	idp := newStubIDP(t)
	s := idp.service()
	if s.prov != nil {
		t.Fatal("New discovered the provider; discovery must wait for the first sign-in")
	}

	// A provider that is down fails the sign-in without caching the failure.
	down := New(s.cfg, nil)
	down.cfg.OIDCIssuer = "http://127.0.0.1:1/nothing-here"
	if err := down.Login(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
		t.Fatal("login against an unreachable provider succeeded")
	}
	if down.prov != nil {
		t.Fatal("a failed discovery was cached; the next sign-in would never retry")
	}

	if _, _, _ = startFlow(t, s); s.prov == nil {
		t.Fatal("discovery result was not reused after a successful sign-in start")
	}
}
