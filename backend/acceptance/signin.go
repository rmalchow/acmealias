//go:build acceptance

package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
)

// THIS FILE STILL SPEAKS KEYCLOAK, AND THE STACK NO LONGER RUNS IT.
//
// The whole acceptance suite fails at sign-in until this is rewritten, and it
// fails with "no login form at ..." — which is accurate and reads like a stack
// problem. It is not: authentik serves no server-rendered login form at all. Its
// sign-in is a browser application driven by a JSON flow executor, so the
// sequence below has to become:
//
//	GET  /api/v1/auth/login             -> 302 to authentik, carrying the flow slug
//	GET/POST /api/v3/flows/executor/... -> step through the identification and
//	                                       password components, one JSON body each
//	GET  (followed)                     -> 302 back to /api/v1/auth/callback
//
// This is task 4.1 of openspec/changes/authentik-and-self-signup (design K5),
// and it was known and named before the swap rather than discovered after it.
//
// Two things below survive the rewrite and must not be thrown away with the
// form parsing: laxJar, whose doc comment explains a trap that has nothing to do
// with which provider is running, and the account discovery in signIn.
//
// ---------------------------------------------------------------------------
//
// Browser-less sign-in against the stack's real Keycloak: discovery, PKCE, the
// authorization code exchange, ID-token verification against real JWKS, the
// groups claim in the shape the realm's mapper produces, the session cookie and
// CSRF. Nothing else in the suite touches any of it.
//
// The sequence is four hops and one parse:
//
//	GET  /api/v1/auth/login   -> 302 to Keycloak's authorization endpoint
//	GET  (followed)           -> Keycloak's login page
//	POST <the form's action>  -> 302 back to /api/v1/auth/callback
//	GET  (followed)           -> session cookie set, 302 to "/"
//
// The only parsing is the one `action` attribute on the login form.

// session is a signed-in console client plus the CSRF token its state-changing
// calls have to echo back.
type session struct {
	user   string
	client *http.Client
	jar    *laxJar
	csrf   string
	// account is the account this session acts in, as a UUID string.
	//
	// Every inventory route lives under /api/v1/accounts/{accountId}/… and the
	// service refuses a path naming an account other than the session's
	// selection (design P19), so the id is not decoration: it is part of every
	// address below. The internal edition selects its single account at sign-in
	// (internal/oidcauth/flow.go), so this is populated by the time signIn
	// returns and a session with an empty one is a bug rather than a state to
	// handle.
	account string
}

// acct addresses one path within the account this session is acting in.
//
//	s.acct("/loadbalancers/" + lb + "/keys")
//	  -> /api/v1/accounts/0199…-…/loadbalancers/0199…-…/keys
//
// Routes that are NOT account-scoped — /me, /accounts, the auth endpoints — are
// written out in full at their call sites, because there are four of them and
// hiding the distinction behind one helper is how a route ends up addressed in
// the wrong plane.
func (s *session) acct(path string) string {
	return "/api/v1/accounts/" + s.account + path
}

// laxJar is an http.CookieJar with RFC 6265's `Secure` rule removed.
//
// THE TRAP THIS EXISTS FOR: Go's net/http/cookiejar will not RETURN a cookie
// marked Secure over a plain http:// URL. This service marks both the session
// cookie (internal/oidcauth/session.go) and the CSRF cookie
// (internal/oidcauth/csrf.go) Secure, and the dev stack speaks plain HTTP on
// console.localhost. A browser is fine with that — `*.localhost` is a
// trustworthy origin under the Secure Contexts spec — but the standard library
// implements the plain RFC and is not. The symptom is a sign-in that looks like
// it worked followed by a 401 with no cookie on it.
//
// The alternative — making those cookies non-Secure in development — is
// rejected outright: it would delete in dev the exact attribute production
// depends on, and that internal/oidcauth's own tests assert.
//
// ponytail: a map keyed by host, ignoring Domain, Path and expiry. That is
// enough for one console origin plus one Keycloak origin and nothing else.
// Ceiling: it would mis-serve a cookie set with a Domain attribute for a parent
// domain, or two cookies of the same name at different paths. Upgrade path:
// wrap net/http/cookiejar and override Cookies() to re-add the Secure ones.
type laxJar struct {
	mu sync.Mutex
	c  map[string]map[string]*http.Cookie
}

func newLaxJar() *laxJar { return &laxJar{c: map[string]map[string]*http.Cookie{}} }

func (j *laxJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	host := u.Hostname()
	if j.c[host] == nil {
		j.c[host] = map[string]*http.Cookie{}
	}
	for _, ck := range cookies {
		if ck.Value == "" || ck.MaxAge < 0 {
			delete(j.c[host], ck.Name)
			continue
		}
		j.c[host][ck.Name] = ck
	}
}

func (j *laxJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []*http.Cookie
	for _, ck := range j.c[u.Hostname()] {
		out = append(out, &http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	return out
}

// has reports whether a cookie of that name is held for host — the regression
// pin that the jar above is actually doing its job.
func (j *laxJar) has(host, name string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	_, ok := j.c[host][name]
	return ok
}

// formAction pulls the `action` URL out of Keycloak's login form. Keycloak
// renders exactly one form on that page.
var formAction = regexp.MustCompile(`(?i)<form[^>]+action="([^"]+)"`)

func signIn(user, password string) (*session, error) {
	jar := newLaxJar()
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	resp, err := client.Get(consoleURL + "/api/v1/auth/login")
	if err != nil {
		return nil, fmt.Errorf("login redirect: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	m := formAction.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("no login form at %s (status %d)", resp.Request.URL, resp.StatusCode)
	}
	action := html.UnescapeString(string(m[1]))

	resp, err = client.PostForm(action, url.Values{
		"username": {user}, "password": {password}, "credentialId": {""},
	})
	if err != nil {
		return nil, fmt.Errorf("credential post: %w", err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)) //nolint:errcheck // draining
	resp.Body.Close()

	if !jar.has("console.localhost", oidcauth.SessionCookieName) {
		return nil, fmt.Errorf("no %s cookie after signing in as %s (last status %d, at %s) — "+
			"wrong password, or the flow failed before the callback",
			oidcauth.SessionCookieName, user, resp.StatusCode, resp.Request.URL)
	}

	s := &session{user: user, client: client, jar: jar}
	// Any GET through the management API mints the CSRF cookie; /me is the one
	// every role may call — and it also answers which account the session was
	// placed in, which every route below needs in its path.
	status, raw, err := s.do(http.MethodGet, "/api/v1/me", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /api/v1/me as %s: got %d: %s", user, status, strings.TrimSpace(string(raw)))
	}
	for _, ck := range jar.Cookies(mustURL(consoleURL)) {
		if ck.Name == oidcauth.CSRFCookieName {
			s.csrf = ck.Value
		}
	}
	if s.csrf == "" {
		return nil, fmt.Errorf("no %s cookie after GET /api/v1/me", oidcauth.CSRFCookieName)
	}

	var me struct {
		SelectedAccountID *string `json:"selectedAccountId"`
	}
	if err := json.Unmarshal(raw, &me); err != nil {
		return nil, fmt.Errorf("decoding /api/v1/me: %w", err)
	}
	if me.SelectedAccountID == nil || *me.SelectedAccountID == "" {
		// Not a state this edition reaches: one account exists and sign-in
		// selects it. Reaching here means either the bootstrap scope could not
		// be resolved (an empty accounts table) or this stack is no longer the
		// single-account edition the suite is written against.
		return nil, fmt.Errorf("signed in as %s but no account is selected — "+
			"the internal edition selects its single account at sign-in (design P19); "+
			"is the accounts table seeded?", user)
	}
	s.account = *me.SelectedAccountID
	return s, nil
}

// do issues one management API call and returns its status and body. body, when
// non-nil, is marshalled as JSON and the CSRF token is attached.
func (s *session) do(method, path string, body any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(enc)
	}
	req, err := http.NewRequest(method, consoleURL+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.csrf != "" {
		req.Header.Set(oidcauth.CSRFHeaderName, s.csrf)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, out, err
}

// getJSON is do + a status check + a decode, which is what most callers want.
func (s *session) getJSON(path string, into any) error {
	return s.json(http.MethodGet, path, nil, http.StatusOK, into)
}

func (s *session) json(method, path string, body any, wantStatus int, into any) error {
	status, raw, err := s.do(method, path, body)
	if err != nil {
		return err
	}
	if status != wantStatus {
		return fmt.Errorf("%s %s as %s: got %d, want %d: %s", method, path, s.user, status, wantStatus, strings.TrimSpace(string(raw)))
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(raw, into)
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
