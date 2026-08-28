//go:build acceptance

package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
)

// Browser-less sign-in against the stack's real authentik: discovery, PKCE, the
// authorization code exchange, ID-token verification against real JWKS, the
// groups claim in the shape the blueprint's scope mapping produces, the session
// cookie and CSRF. Nothing else in the suite touches any of it.
//
// The sequence is in signIn's own comment, because it is the interesting part.

// session is a signed-in console client plus the CSRF token its state-changing
// calls have to echo back.
type session struct {
	user   string
	client *http.Client
	jar    *laxJar
	csrf   string
	// subject is the identity-provider subject the service knows this person by.
	//
	// It is read from /me rather than assembled from the username, because the
	// provider is configured with sub_mode: hashed_user_id — the subject is a
	// hash, and nothing outside authentik can compute it. Every fixture that has
	// to name a person (a membership, a transfer, an invitation) needs it, which
	// is why signing in is the only way to get one.
	subject string
	// account is the account this session acts in, as a UUID string.
	//
	// Every inventory route lives under /api/v1/accounts/{accountId}/… and the
	// service refuses a path naming an account other than the session's
	// selection (design P19), so the id is not decoration: it is part of every
	// address below.
	//
	// It is EMPTY when signIn returns, and that is the front door rather than an
	// edge case (design C10, task 7.5). Sign-in used to select the deployment's
	// single account; there is no such account any more, so a session begins
	// having chosen nothing and something has to choose — see selectInto in
	// accounts_test.go.
	account string
}

// acct addresses one path within the account this session is acting in.
//
//	s.acct("/projects/" + project + "/keys")
//	  -> /api/v1/accounts/0199…-…/projects/0199…-…/keys
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

// The identity provider's own origin. Sign-in leaves the console for it and
// comes back, and every flow-executor call below is addressed here.
const authURL = "http://auth.localhost"

// flowPathMarker is how a URL announces that authentik wants a flow walked. The
// browser application is served at /if/flow/<slug>/ and reads its query string;
// the executor API answers for the same slug and query.
const flowPathMarker = "/if/flow/"

// flowStep is as much of the executor's answer as this suite needs.
//
// `component` names what the flow wants next — an identification stage, a
// password stage, a consent — and the sentinel `xak-flow-redirect` means it wants
// nothing and the caller should follow `to`.
type flowStep struct {
	Component      string                      `json:"component"`
	To             string                      `json:"to"`
	ResponseErrors map[string][]flowFieldError `json:"response_errors"`
}

type flowFieldError struct {
	String string `json:"string"`
	Code   string `json:"code"`
}

func (s flowStep) errorText() string {
	var out []string
	for field, errs := range s.ResponseErrors {
		for _, e := range errs {
			out = append(out, field+": "+e.String)
		}
	}
	return strings.Join(out, "; ")
}

// executeFlow asks one flow what it wants and answers it, returning the redirect
// it ends in.
//
// The GET first is not redundant: a flow that needs no input — the consent flow,
// under implicit consent — answers the GET with the redirect directly, and
// POSTing to it would submit an empty body to a stage that never asked for one.
//
// A stage that rejects what it was given re-renders ITSELF rather than returning
// an error status, which is deliberate on authentik's part: the identification
// stage must not reveal whether the account exists, so a wrong identifier and a
// wrong password fail identically. Correct, and it means a failed sign-in here
// looks like "the flow asked again" rather than like a 401.
func executeFlow(client *http.Client, flow *url.URL, payload map[string]string) (flowStep, error) {
	slug, _, ok := strings.Cut(strings.TrimPrefix(flow.Path, flowPathMarker), "/")
	if !ok || slug == "" {
		return flowStep{}, fmt.Errorf("no flow slug in %s", flow)
	}
	ex := fmt.Sprintf("%s/api/v3/flows/executor/%s/?query=%s", authURL, slug, url.QueryEscape(flow.RawQuery))

	step, err := flowCall(client, http.MethodGet, ex, nil)
	if err != nil {
		return flowStep{}, err
	}
	if step.Component == "xak-flow-redirect" {
		return step, nil
	}

	step, err = flowCall(client, http.MethodPost, ex, payload)
	if err != nil {
		return flowStep{}, err
	}
	if step.Component != "xak-flow-redirect" {
		msg := step.errorText()
		if msg == "" {
			// The anti-enumeration re-render: no error is reported because
			// reporting one would say whether the account exists.
			msg = "the stage re-rendered without saying why, which is what a wrong identifier or a wrong password looks like"
		}
		return flowStep{}, fmt.Errorf("flow %s stopped at %q: %s", slug, step.Component, msg)
	}
	return step, nil
}

// flowCall is one request to the executor. `Accept: application/json` is what
// separates the API from the browser application: without it the same URL serves
// the HTML shell and the decode fails on a `<`.
func flowCall(client *http.Client, method, ex string, payload map[string]string) (flowStep, error) {
	var body io.Reader
	if payload != nil {
		enc, err := json.Marshal(payload)
		if err != nil {
			return flowStep{}, err
		}
		body = bytes.NewReader(enc)
	}
	req, err := http.NewRequest(method, ex, body)
	if err != nil {
		return flowStep{}, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return flowStep{}, fmt.Errorf("%s %s: %w", method, ex, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return flowStep{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return flowStep{}, fmt.Errorf("%s %s: got %d: %s", method, ex, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var step flowStep
	if err := json.Unmarshal(raw, &step); err != nil {
		return flowStep{}, fmt.Errorf("decoding the flow step from %s: %w", ex, err)
	}
	return step, nil
}

// signIn completes a browser-less sign-in against authentik and returns a session
// the management API accepts.
//
// THE SHAPE OF THIS IS NOT A FORM POST, AND THAT IS THE WHOLE REASON IT IS LONG.
// authentik serves no server-rendered login form: its sign-in is a browser
// application driven by a JSON flow executor, so the only way in without a
// browser is to walk the flows the way that application does.
//
//	GET  /api/v1/auth/login          -> 302 chain, landing on /if/flow/<slug>/?…
//	GET  /api/v3/flows/executor/…    -> what does this flow want?
//	POST /api/v3/flows/executor/…    -> identifier and password, TOGETHER
//	     (the identification stage renders the password field itself, so this is
//	     one submission rather than two — see the blueprint, which sets
//	     password_stage to close a username oracle)
//	follow `to`                      -> which may land on ANOTHER flow
//	…                                -> until it lands somewhere that is not one
//	                                    (the callback), and the session exists
//
// The second flow is the part that is easy to miss: authorization hands off to a
// consent flow, which auto-approves under the blueprint's implicit-consent
// setting but must still be EXECUTED. A client that stops after the password is
// accepted gets a session cookie for authentik and none for us, and the symptom
// is a 401 from /api/v1/me immediately after a sign-in that plainly worked.
func signIn(user, password string) (*session, error) {
	jar := newLaxJar()
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	resp, err := client.Get(consoleURL + "/api/v1/auth/login")
	if err != nil {
		return nil, fmt.Errorf("login redirect: %w", err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)) //nolint:errcheck // draining
	resp.Body.Close()

	// Where the redirect chain stopped. Go follows 302s, so this is the flow page
	// the browser would be looking at.
	landed := resp.Request.URL
	if !strings.Contains(landed.Path, flowPathMarker) {
		return nil, fmt.Errorf("expected to land on an authentik flow, got %s (status %d)", landed, resp.StatusCode)
	}

	// Only the FIRST flow is answered with credentials. Everything after it —
	// consent, and anything a future blueprint adds — is walked with an empty
	// body, which is what the browser sends for a stage that needs no input.
	payload := map[string]string{"uid_field": user, "password": password}
	for hop := 0; hop < 8; hop++ {
		if !strings.Contains(landed.Path, flowPathMarker) {
			break // no longer in a flow: this is the callback, and we are done
		}
		step, err := executeFlow(client, landed, payload)
		if err != nil {
			return nil, err
		}
		payload = nil

		next, err := landed.Parse(step.To)
		if err != nil {
			return nil, fmt.Errorf("flow redirect %q: %w", step.To, err)
		}
		r, err := client.Get(next.String())
		if err != nil {
			return nil, fmt.Errorf("following the flow to %s: %w", next, err)
		}
		io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20)) //nolint:errcheck // draining
		r.Body.Close()
		landed = r.Request.URL
	}
	if strings.Contains(landed.Path, flowPathMarker) {
		return nil, fmt.Errorf("still walking flows after 8 hops, last at %s — a stage is asking for something this suite does not answer", landed)
	}

	if !jar.has("console.localhost", oidcauth.SessionCookieName) {
		return nil, fmt.Errorf("no %s cookie after signing in as %s (landed on %s) — "+
			"the flows completed but our callback did not establish a session",
			oidcauth.SessionCookieName, user, landed)
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
		Subject           string  `json:"subject"`
		SelectedAccountID *string `json:"selectedAccountId"`
	}
	if err := json.Unmarshal(raw, &me); err != nil {
		return nil, fmt.Errorf("decoding /api/v1/me: %w", err)
	}
	// A fresh session has selected NOTHING, and this asserts it rather than
	// tolerating it (design C10, task 7.5). Sign-in used to select the single
	// account a deployment had; a selection appearing here again would mean
	// something is choosing on the user's behalf, which is the behaviour that
	// made "no account selected" unreachable and left the console's picker — now
	// the front door — exercised by nothing.
	if me.SelectedAccountID != nil {
		return nil, fmt.Errorf("signed in as %s and an account (%s) was already selected; "+
			"sign-in must select nothing (design P19, task 2.2)", user, *me.SelectedAccountID)
	}
	if me.Subject == "" {
		return nil, fmt.Errorf("GET /api/v1/me as %s answered no subject", user)
	}
	s.subject = me.Subject
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
