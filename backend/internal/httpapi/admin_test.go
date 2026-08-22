package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The role tests below call every route directly, without the console. That is
// the point of them: the Angular guard hides controls, it does not enforce
// anything, so what is asserted here is that curl gets the same answer.

// --- test doubles ----------------------------------------------------------

// testAuth stands in for *oidcauth.Service on the guard. It resolves a real
// session row named by a test header, applying the same revoked/expired rules
// oidcauth.Authenticate does — which is what makes the revocation test
// meaningful rather than a test of a map.
type testAuth struct{ db *gorm.DB }

func (t *testAuth) Authenticate(ctx context.Context, r *http.Request) (*model.Session, error) {
	id := r.Header.Get("X-Test-Session")
	if id == "" {
		return nil, oidcauth.ErrNoSession
	}
	s, err := store.FindSession(ctx, t.db, id)
	if err != nil {
		return nil, oidcauth.ErrSessionInvalid
	}
	if s.RevokedAt != nil || !time.Now().Before(s.ExpiresAt) {
		return nil, oidcauth.ErrSessionInvalid
	}
	return s, nil
}

// testConsoleAuth supplies the sign-in routes and the CSRF middleware. The
// CSRF middleware is the real one, configured with oidcauth's names, so the
// mount point is exercised rather than stubbed out.
type testConsoleAuth struct{}

func (testConsoleAuth) Login(w http.ResponseWriter, r *http.Request) error {
	http.Redirect(w, r, "https://idp.example/authorize", http.StatusFound)
	return nil
}
func (testConsoleAuth) Callback(http.ResponseWriter, *http.Request) error { return nil }
func (testConsoleAuth) Logout(context.Context, http.ResponseWriter, *http.Request) error {
	return nil
}
func (testConsoleAuth) CSRF() echo.MiddlewareFunc {
	return middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:  "header:" + oidcauth.CSRFHeaderName,
		CookieName:   oidcauth.CSRFCookieName,
		CookiePath:   "/",
		CookieSecure: false,
	})
}

// --- fixture ---------------------------------------------------------------

const (
	groupAdmin  = "g-admin"
	groupConfig = "g-config"
	groupKey    = "g-key"
	groupRead   = "g-read"
)

type adminEnv struct {
	t      *testing.T
	db     *gorm.DB
	e      *echo.Echo
	lb     *model.LoadBalancer
	projID model.ID
	sc     store.Scope
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	db := migratedDB(t)
	ctx := t.Context()
	sc, err := store.ScopeForBootstrap(ctx, db)
	if err != nil {
		t.Fatalf("bootstrap scope: %v", err)
	}

	p, err := inventory.CreateProject(ctx, db, sc, "acme", "Acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb, err := inventory.CreateLoadBalancer(ctx, db, sc, p.ID, "eu-prod", "EU production")
	if err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	if _, err := inventory.AddNode(ctx, db, sc, lb.ID, "a", "203.0.113.10", "2001:db8:1::10"); err != nil {
		t.Fatalf("add node: %v", err)
	}

	cfg := config.Config{
		Zone:               "acmeservice.example.",
		CAAIssuers:         []string{"letsencrypt.org"},
		SessionIdleTimeout: time.Hour,
		SessionMaxLifetime: 8 * time.Hour,
		AdminGroup:         groupAdmin,
		ConfigAdminGroup:   groupConfig,
		KeyRotationGroup:   groupKey,
		ReadOnlyGroup:      groupRead,
		OIDCClientSecret:   "super-secret-value",
		DatabaseURL:        "postgres://user:hunter2@db:5432/acme",
	}

	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	err = RegisterAdmin(e, AdminOptions{
		DB:     db,
		Guard:  &authz.Guard{Auth: &testAuth{db: db}, Mapping: authz.MappingFromConfig(cfg), Log: discard()},
		Auth:   testConsoleAuth{},
		Config: cfg,
		Log:    discard(),
	})
	if err != nil {
		t.Fatalf("RegisterAdmin: %v", err)
	}
	// The console's history fallback, as group 16 mounts it. Registering it
	// here is what makes the "unknown API paths stay JSON" test a test of the
	// real arrangement rather than of an empty router.
	e.GET("/*", func(c echo.Context) error { return c.HTML(http.StatusOK, "<html>console</html>") })

	return &adminEnv{t: t, db: db, sc: sc, e: e, lb: lb, projID: p.ID}
}

// session inserts a session row in the given groups and returns a client
// authenticated as it.
func (v *adminEnv) session(subject string, groups ...string) *client {
	v.t.Helper()
	now := time.Now().UTC()
	s := &model.Session{
		ID: subject + "-session-" + hexish(), Subject: subject, DisplayName: strings.ToUpper(subject),
		Groups: model.StringArray(groups), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateSession(v.t.Context(), v.db, s); err != nil {
		v.t.Fatalf("create session: %v", err)
	}
	return &client{t: v.t, e: v.e, sessionID: s.ID}
}

var hexCounter int

func hexish() string {
	hexCounter++
	return time.Now().Format("150405.000000000") + "-" + string(rune('a'+hexCounter%26))
}

type client struct {
	t         *testing.T
	e         *echo.Echo
	sessionID string
	csrf      string
}

func (c *client) do(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	if c.csrf == "" && method != http.MethodGet {
		c.token()
	}
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if c.sessionID != "" {
		req.Header.Set("X-Test-Session", c.sessionID)
	}
	if c.csrf != "" {
		req.Header.Set(oidcauth.CSRFHeaderName, c.csrf)
		req.AddCookie(&http.Cookie{Name: oidcauth.CSRFCookieName, Value: c.csrf})
	}
	rec := httptest.NewRecorder()
	c.e.ServeHTTP(rec, req)
	return rec
}

// token performs one GET so the CSRF middleware issues a cookie, and keeps it.
func (c *client) token() {
	c.t.Helper()
	rec := c.do(http.MethodGet, "/api/v1/me", "")
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == oidcauth.CSRFCookieName {
			c.csrf = ck.Value
			return
		}
	}
	c.t.Fatalf("no %s cookie was issued (status %d)", oidcauth.CSRFCookieName, rec.Code)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

// assertEveryRouteHasARow fails when a registered management route is missing
// from the matrix above. Without it the matrix is a list someone has to
// remember to extend: a new route ships, every existing row still passes, and
// the route nobody added is exactly the one whose permission was never
// checked.
//
// covered holds "METHOD /route/:pattern" keys, resolved from each row's
// concrete path by the same router that serves it — so the rows stay readable
// ("/api/v1/projects/7/usage", not "/api/v1/projects/:id/usage").
func assertEveryRouteHasARow(t *testing.T, e *echo.Echo, covered map[string]bool) {
	t.Helper()
	for _, r := range e.Routes() {
		// Sign-in is deliberately outside the guard, and the catch-alls match
		// no endpoint of their own.
		if !strings.HasPrefix(r.Path, "/api/v1/") || strings.HasPrefix(r.Path, "/api/v1/auth/") || strings.HasSuffix(r.Path, "*") {
			continue
		}
		if !covered[r.Method+" "+r.Path] {
			t.Errorf("%s %s is registered but has no row in the role matrix — add one saying which roles may reach it",
				r.Method, r.Path)
		}
	}
}

// --- the role matrix -------------------------------------------------------

// TestEveryRouteEnforcesItsRole calls every route as each of the four roles.
//
// Two rows carry the weight: KEY_ROTATION creating a key but not a domain is
// the only lb that distinguishes the CONFIG_ADMIN/KEY_ROTATION boundary from
// a naive "any role may write", and the session routes are the only ones
// ADMIN alone may reach. The rest would pass under a much weaker
// implementation, and are here so a future edit cannot quietly widen one.
func TestEveryRouteEnforcesItsRole(t *testing.T) {
	v := newAdminEnv(t)

	clients := map[string]*client{
		"ADMIN":        v.session("admin", groupAdmin),
		"CONFIG_ADMIN": v.session("config", groupConfig),
		"KEY_ROTATION": v.session("keys", groupKey),
		"READ_ONLY":    v.session("reader", groupRead),
	}

	lb := "/api/v1/loadbalancers/" + itoa(v.lb.ID)
	proj := "/api/v1/projects/" + itoa(v.projID)

	cases := []struct {
		method, path, body string
		allowed            []string
	}{
		// Reads: every role.
		{"GET", "/api/v1/me", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", "/api/v1/projects", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", proj + "/loadbalancers", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb, "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb + "/domains", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb + "/keys", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb + "/plan", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb + "/plan/zone", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", "/api/v1/challenges", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", "/api/v1/audit", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", "/api/v1/config", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", proj, "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", proj + "/usage", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", proj + "/inventory-history", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb + "/interactions", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},
		{"GET", lb + "/verification", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}},

		// Projects: ADMIN only.
		{"POST", "/api/v1/projects", `{"slug":"other","name":"Other"}`, []string{"ADMIN"}},
		{"PATCH", proj, `{"name":"Renamed"}`, []string{"ADMIN"}},
		{"DELETE", "/api/v1/projects/00000000-0000-7000-8000-000000000999", "", []string{"ADMIN"}},

		// Inventory: ADMIN and CONFIG_ADMIN. KEY_ROTATION must not pass here.
		{"POST", proj + "/loadbalancers", `{"slug":"x","name":"X"}`, []string{"ADMIN", "CONFIG_ADMIN"}},
		{"PATCH", lb, `{"name":"Renamed"}`, []string{"ADMIN", "CONFIG_ADMIN"}},
		{"DELETE", "/api/v1/loadbalancers/00000000-0000-7000-8000-000000000999", "", []string{"ADMIN", "CONFIG_ADMIN"}},
		{"POST", lb + "/nodes", `{"label":"z","ipv4":"203.0.113.99"}`, []string{"ADMIN", "CONFIG_ADMIN"}},
		{"PATCH", "/api/v1/nodes/00000000-0000-7000-8000-000000000999", `{"ipv4":"203.0.113.1"}`, []string{"ADMIN", "CONFIG_ADMIN"}},
		{"DELETE", "/api/v1/nodes/00000000-0000-7000-8000-000000000999", "", []string{"ADMIN", "CONFIG_ADMIN"}},
		{"POST", lb + "/domains", `{"name":"shop.example.com"}`, []string{"ADMIN", "CONFIG_ADMIN"}},
		{"DELETE", "/api/v1/domains/00000000-0000-7000-8000-000000000999", "", []string{"ADMIN", "CONFIG_ADMIN"}},

		// Keys: ADMIN, CONFIG_ADMIN and KEY_ROTATION.
		{"POST", lb + "/keys", `{"note":"traefik"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}},
		{"PUT", "/api/v1/keys/00000000-0000-7000-8000-000000000999/networks", `{"networks":[]}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}},
		{"PUT", "/api/v1/keys/00000000-0000-7000-8000-000000000999/expiry", `{"expiresIn":"720h"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}},
		{"POST", "/api/v1/keys/00000000-0000-7000-8000-000000000999/rotate", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}},
		{"DELETE", "/api/v1/keys/00000000-0000-7000-8000-000000000999", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}},

		// Sessions: ADMIN only.
		{"GET", "/api/v1/sessions", "", []string{"ADMIN"}},
		{"DELETE", "/api/v1/sessions?subject=nobody", "", []string{"ADMIN"}},
		{"DELETE", "/api/v1/sessions/" + strings.Repeat("0", 64), "", []string{"ADMIN"}},
	}

	covered := map[string]bool{}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		c := v.e.NewContext(req, httptest.NewRecorder())
		v.e.Router().Find(tc.method, req.URL.Path, c)
		covered[tc.method+" "+c.Path()] = true
	}
	assertEveryRouteHasARow(t, v.e, covered)

	for _, tc := range cases {
		for role, cl := range clients {
			t.Run(tc.method+" "+tc.path+" as "+role, func(t *testing.T) {
				rec := cl.do(tc.method, tc.path, tc.body)
				if contains(tc.allowed, role) {
					if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
						t.Fatalf("%s %s as %s: %d %s, want the role to be accepted",
							tc.method, tc.path, role, rec.Code, rec.Body.String())
					}
					return
				}
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s %s as %s: %d %s, want 403", tc.method, tc.path, role, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

// A refusal must never depend on a role the caller happens to also hold: a
// user in the key-rotation group and nothing else may mint a key and must not
// touch the inventory. This is the load balancer that pins the boundary, so it is
// asserted end to end rather than only through the matrix above.
func TestKeyRotationMayMintAKeyButNotADomain(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("keys", groupKey)
	lb := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	rec := cl.do(http.MethodPost, lb+"/keys", `{"note":"traefik"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s, want 201", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if created.Secret == "" {
		t.Fatal("key creation returned no secret; the one time it is ever available is now")
	}

	rec = cl.do(http.MethodPost, lb+"/domains", `{"name":"shop.example.com"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create domain as KEY_ROTATION: %d %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestUnauthenticatedManagementRequestIs401(t *testing.T) {
	v := newAdminEnv(t)
	cl := &client{t: t, e: v.e}
	rec := cl.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	var body ErrorBody
	decode(t, rec, &body)
	if body.Message == "" {
		t.Errorf("401 body %s carries no message", rec.Body.String())
	}
}

// A user who authenticates but maps to no role is told which groups were
// actually seen — the only thing that distinguishes a typo in
// ACME_ADMIN_GROUP from Keycloak's "Full group path" being left on.
func TestAuthenticatedWithNoMappedGroupIsRefusedAndTold(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("stranger", "some-other-group")
	rec := cl.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	var body authz.NoRoleBody
	decode(t, rec, &body)
	if body.Message == "" || !contains(body.ObservedGroups, "some-other-group") {
		t.Fatalf("403 body %s does not report the observed groups", rec.Body.String())
	}
}

// --- /me and the error shape -----------------------------------------------

func TestMeCarriesDisplayNameGroupsAndRoles(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin, groupRead)
	rec := cl.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var me meView
	decode(t, rec, &me)
	if me.DisplayName != "ADMIN" {
		t.Errorf("displayName %q", me.DisplayName)
	}
	if !contains(me.Groups, groupAdmin) || !contains(me.Groups, groupRead) {
		t.Errorf("groups %v do not carry the session's claim", me.Groups)
	}
	if !contains(me.Roles, "ADMIN") || !contains(me.Roles, "READ_ONLY") {
		t.Errorf("roles %v: roles are additive, both groups were mapped", me.Roles)
	}
}

func TestDomainErrorsMapToStatusesAndOneJSONShape(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	lb := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	if rec := cl.do(http.MethodPost, lb+"/domains", `{"name":"shop.example.com"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed domain: %d %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"invalid wildcard", "POST", lb + "/domains", `{"name":"*.*.example.com"}`, http.StatusBadRequest},
		{"duplicate domain", "POST", lb + "/domains", `{"name":"shop.example.com"}`, http.StatusConflict},
		{"unknown lb", "GET", "/api/v1/loadbalancers/00000000-0000-7000-8000-000000000999", "", http.StatusNotFound},
		{"non-empty project", "DELETE", "/api/v1/projects/" + itoa(v.projID), "", http.StatusConflict},
		{"non-numeric id", "GET", "/api/v1/loadbalancers/banana", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := cl.do(tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if ct := rec.Header().Get(echo.HeaderContentType); !strings.Contains(ct, echo.MIMEApplicationJSON) {
				t.Fatalf("content type %q, want JSON", ct)
			}
			var body ErrorBody
			decode(t, rec, &body)
			if body.Message == "" {
				t.Fatalf("error body %s carries no message", rec.Body.String())
			}
		})
	}
}

// An unknown path under the API prefix must stay JSON even though the console
// claims everything else: a typo'd endpoint that answers with index.html turns
// into a bug report about the console.
func TestUnknownAPIPathIsJSONNotTheConsole(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)

	for _, path := range []string{"/api/v1/nope", "/api/v2/projects", "/api/"} {
		rec := cl.do(http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "<html>") {
			t.Errorf("%s: answered with the console: %s", path, rec.Body.String())
		}
		var body ErrorBody
		decode(t, rec, &body)
		if body.Message == "" {
			t.Errorf("%s: 404 body %s carries no message", path, rec.Body.String())
		}
	}

	// The fallback still serves everything that is not the API.
	if rec := cl.do(http.MethodGet, "/loadbalancers/1", ""); !strings.Contains(rec.Body.String(), "<html>") {
		t.Errorf("the console fallback stopped working: %d %s", rec.Code, rec.Body.String())
	}
}

// --- keys ------------------------------------------------------------------

func TestKeySecretIsReturnedOnceAndNeverRead(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	lb := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	rec := cl.do(http.MethodPost, lb+"/keys", `{"note":"traefik","networks":["203.0.113.5/24"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if created.Secret == "" {
		t.Fatal("no secret in the creation response")
	}
	// The CIDR is stored masked: PostgreSQL's cidr type refuses host bits.
	if len(created.Key.Networks) != 1 || created.Key.Networks[0] != "203.0.113.0/24" {
		t.Fatalf("networks %v, want the masked prefix", created.Key.Networks)
	}

	rec = cl.do(http.MethodGet, lb+"/keys", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), created.Secret) {
		t.Fatal("the key listing returned the secret")
	}
	var listed []map[string]any
	decode(t, rec, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed %d keys, want 1", len(listed))
	}
	for _, forbidden := range []string{"secret", "secretHash", "SecretHash"} {
		if _, ok := listed[0][forbidden]; ok {
			t.Errorf("the key listing carries a %q field", forbidden)
		}
	}

	// Rotation mints a successor and leaves the old key working, which is what
	// makes a zero-downtime swap possible.
	rec = cl.do(http.MethodPost, "/api/v1/keys/"+itoa(created.Key.ID)+"/rotate", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rotated keyCreated
	decode(t, rec, &rotated)
	if rotated.Secret == created.Secret || rotated.Key.ID == created.Key.ID {
		t.Fatal("rotation returned the same key")
	}
	if len(rotated.Key.Networks) != 1 {
		t.Errorf("rotation dropped the address restrictions: %v", rotated.Key.Networks)
	}

	rec = cl.do(http.MethodDelete, "/api/v1/keys/"+itoa(created.Key.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	var revoked keyView
	decode(t, rec, &revoked)
	if revoked.Active || revoked.DisabledAt == nil {
		t.Errorf("revoked key still reads as active: %+v", revoked)
	}
}

func TestKeyRestrictionRejectsGarbageCIDR(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	rec := cl.do(http.MethodPost, "/api/v1/loadbalancers/"+itoa(v.lb.ID)+"/keys", `{"networks":["not-a-cidr"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// --- sessions --------------------------------------------------------------

func TestSessionRevocationIsImmediateAndConfined(t *testing.T) {
	v := newAdminEnv(t)
	admin := v.session("admin", groupAdmin)
	alice := v.session("alice", groupRead)
	bob := v.session("bob", groupRead)

	rec := admin.do(http.MethodGet, "/api/v1/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions: %d %s", rec.Code, rec.Body.String())
	}
	var listed []sessionView
	decode(t, rec, &listed)
	if len(listed) < 3 {
		t.Fatalf("listed %d sessions, want at least 3", len(listed))
	}
	// The listing must not carry anything usable as a credential.
	for _, s := range listed {
		if strings.Contains(rec.Body.String(), bob.sessionID) {
			t.Fatal("the sessions listing contains a raw session id")
		}
		if s.Subject == "" || s.CreatedAt.IsZero() || s.ExpiresAt.IsZero() {
			t.Fatalf("session row is missing subject/start/expiry: %+v", s)
		}
	}

	var bobHandle string
	for _, s := range listed {
		if s.Subject == "bob" {
			bobHandle = s.Handle
		}
	}
	if bobHandle == "" {
		t.Fatal("bob's session was not listed")
	}

	if rec := admin.do(http.MethodDelete, "/api/v1/sessions/"+bobHandle, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := bob.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bob after revocation: %d, want 401", rec.Code)
	}
	if rec := alice.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("alice after bob's revocation: %d %s, want 200", rec.Code, rec.Body.String())
	}

	// The revocation is attributable: the administrator and the subject whose
	// access was ended are both on the record.
	rec = admin.do(http.MethodGet, "/api/v1/audit?objectType=session", "")
	var entries []auditView
	decode(t, rec, &entries)
	if len(entries) == 0 {
		t.Fatal("no audit entry for the revocation")
	}
	e := entries[0]
	if e.ActorSubject == nil || *e.ActorSubject != "admin" || e.ObjectLabel != "bob" {
		t.Fatalf("audit entry does not name the administrator and the subject: %+v", e)
	}
	if !contains(e.ActorRoles, "ADMIN") {
		t.Fatalf("audit entry lost the roles held at the time: %v", e.ActorRoles)
	}
}

func TestRevokingEverySessionOfOneSubject(t *testing.T) {
	v := newAdminEnv(t)
	admin := v.session("admin", groupAdmin)
	first := v.session("carol", groupRead)
	second := v.session("carol", groupRead)
	other := v.session("dave", groupRead)

	if rec := admin.do(http.MethodDelete, "/api/v1/sessions?subject=carol", ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke by subject: %d %s", rec.Code, rec.Body.String())
	}
	for _, cl := range []*client{first, second} {
		if rec := cl.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("a session of carol survived: %d", rec.Code)
		}
	}
	if rec := other.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusOK {
		t.Errorf("dave was signed out too: %d %s", rec.Code, rec.Body.String())
	}
}

// --- CSRF, audit, plan -----------------------------------------------------

func TestStateChangingRequestWithoutTheTokenIsRejected(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	// A client that never fetches a token: a valid session cookie alone must
	// not be enough.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{"slug":"x","name":"X"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Test-Session", cl.sessionID)
	rec := httptest.NewRecorder()
	v.e.ServeHTTP(rec, req)

	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("status %d, want a 4xx: %s", rec.Code, rec.Body.String())
	}
	var count int64
	v.db.Model(&model.Project{}).Where("slug = ?", "x").Count(&count)
	if count != 0 {
		t.Fatal("the project was created despite the missing CSRF token")
	}
}

// wantEntry is one expected audit entry: the action, the object label it must
// carry after the object itself is gone, and the project it must be filterable
// by.
type wantEntry struct {
	action    string
	label     string
	projectID *model.ID
}

// anything stands in for a value the test cannot know before the request:
// a key identifier is minted during it, and a project id does not exist until
// the project has been inserted. Both are asserted separately, right after.
const anything = "\x00any"

// anyProject is a sentinel pointer, never a real identifier: the assertion
// compares against the pointer itself, so the value behind it is irrelevant and
// deliberately not a value any row could hold.
var anyProject = &[]model.ID{model.NewID()}[0]

// auditRecorder drives one mutating route at a time and asserts exactly which
// entries it produced. It reads through GET /api/v1/audit — the surface an
// operator actually has — rather than the table, so a mutation that writes a
// row nobody can find still fails here.
type auditRecorder struct {
	cl *client
	// seen is every entry this recorder has already accounted for. Identifiers
	// are UUIDs now, so "newer than the last one" is no longer a comparison —
	// v7 values do sort by time, but as a [16]byte they are not ordered in Go,
	// and a set says what is meant without relying on that.
	seen map[model.ID]bool
}

// step issues one mutating request and asserts that it produced exactly want,
// in the order written. Passing no wantEntry asserts the route wrote nothing.
func (r *auditRecorder) step(t *testing.T, method, path, body string, wantStatus int, want ...wantEntry) *httptest.ResponseRecorder {
	t.Helper()
	rec := r.cl.do(method, path, body)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: %d %s, want %d", method, path, rec.Code, rec.Body.String(), wantStatus)
	}

	listing := r.cl.do(http.MethodGet, "/api/v1/audit?limit=1000", "")
	var all []auditView
	decode(t, listing, &all)

	// Newest first, so walk backwards to get them in the order they were
	// written, and keep only what this request added.
	var got []auditView
	for i := len(all) - 1; i >= 0; i-- {
		if !r.seen[all[i].ID] {
			got = append(got, all[i])
		}
	}
	if r.seen == nil {
		r.seen = map[model.ID]bool{}
	}
	for _, e := range got {
		r.seen[e.ID] = true
	}

	if len(got) != len(want) {
		t.Fatalf("%s %s wrote %d audit entries, want %d: %+v", method, path, len(got), len(want), got)
	}
	for i, w := range want {
		e := got[i]
		if e.Action != w.action {
			t.Errorf("%s %s entry %d: action %q, want %q", method, path, i, e.Action, w.action)
		}
		if w.label != anything && e.ObjectLabel != w.label {
			t.Errorf("%s %s entry %d: objectLabel %q, want %q", method, path, i, e.ObjectLabel, w.label)
		}
		switch {
		case w.projectID == anyProject:
		case w.projectID == nil && e.ProjectID != nil:
			t.Errorf("%s %s entry %d: projectId %d, want none", method, path, i, *e.ProjectID)
		case w.projectID != nil && (e.ProjectID == nil || *e.ProjectID != *w.projectID):
			t.Errorf("%s %s entry %d: projectId %v, want %d", method, path, i, e.ProjectID, *w.projectID)
		}
		if e.ActorSource != authz.SourceConsole || !contains(e.ActorRoles, "ADMIN") {
			t.Errorf("%s %s entry %d: actor %q roles %v, want a console ADMIN",
				method, path, i, e.ActorSource, e.ActorRoles)
		}
		if e.ObjectType == "" {
			t.Errorf("%s %s entry %d: no objectType", method, path, i)
		}
	}
	return rec
}

// TestMutationsAreAudited walks every mutating route on the management API and
// asserts that each writes exactly the entries it should — no route silently
// skipping the trail, and no route writing two rows for one change.
//
// It is one sequential scenario rather than a table because most of these
// routes need an object that an earlier one created, and because "exactly
// these entries and no others" is only meaningful if nothing else is running.
func TestMutationsAreAudited(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	r := &auditRecorder{cl: cl}

	// --- projects ---
	rec := r.step(t, http.MethodPost, "/api/v1/projects", `{"slug":"beta","name":"Beta"}`,
		http.StatusCreated, wantEntry{authz.ActionProjectCreate, "beta", anyProject})
	var proj projectView
	decode(t, rec, &proj)
	// Every entry below belongs to this project, and stays filterable by it.
	pid := &proj.ID
	// The create entry could not know its own project id before the insert, so
	// it is checked here instead of in the step above.
	assertProjectID(t, cl, authz.ActionProjectCreate, proj.ID)

	projPath := "/api/v1/projects/" + itoa(proj.ID)
	r.step(t, http.MethodPatch, projPath, `{"name":"Beta Renamed"}`,
		http.StatusOK, wantEntry{authz.ActionProjectUpdate, "beta", pid})

	// --- load balancers ---
	rec = r.step(t, http.MethodPost, projPath+"/loadbalancers", `{"slug":"eu","name":"EU"}`,
		http.StatusCreated, wantEntry{authz.ActionLoadBalancerCreate, "eu", pid})
	var lb loadBalancerView
	decode(t, rec, &lb)
	lbPath := "/api/v1/loadbalancers/" + itoa(lb.ID)

	r.step(t, http.MethodPatch, lbPath, `{"name":"EU Renamed"}`,
		http.StatusOK, wantEntry{authz.ActionLoadBalancerUpdate, "eu", pid})

	// --- nodes ---
	rec = r.step(t, http.MethodPost, lbPath+"/nodes", `{"label":"n1","ipv4":"203.0.113.20"}`,
		http.StatusCreated, wantEntry{authz.ActionNodeCreate, "n1", pid})
	var n nodeView
	decode(t, rec, &n)
	nodePath := "/api/v1/nodes/" + itoa(n.ID)

	r.step(t, http.MethodPatch, nodePath, `{"ipv4":"203.0.113.21"}`,
		http.StatusOK, wantEntry{authz.ActionNodeUpdate, "n1", pid})

	// --- domains, and the alias that appears and disappears with them ---
	// The alias entry's label is whatever the service generated — the request
	// cannot ask for one (design A5), so the test cannot know it either.
	rec = r.step(t, http.MethodPost, lbPath+"/domains", `{"name":"shop.example.com"}`,
		http.StatusCreated,
		wantEntry{authz.ActionDomainCreate, "shop.example.com", pid},
		wantEntry{authz.ActionAliasCreate, anything, pid})
	var dom domainCreated
	decode(t, rec, &dom)

	// A second domain on the same challenge name reuses the alias, so no
	// alias entry this time — the wildcard-and-apex case.
	rec = r.step(t, http.MethodPost, lbPath+"/domains", `{"name":"*.shop.example.com"}`,
		http.StatusCreated, wantEntry{authz.ActionDomainCreate, "*.shop.example.com", pid})
	var wildcard domainCreated
	decode(t, rec, &wildcard)

	r.step(t, http.MethodDelete, "/api/v1/domains/"+itoa(wildcard.ID), "",
		http.StatusOK, wantEntry{authz.ActionDomainDelete, "*.shop.example.com", pid})
	// Now the last domain goes, and the alias goes with it: the cascade is its
	// own entry, because losing the alias is what stops the customer's
	// published CNAME from validating.
	r.step(t, http.MethodDelete, "/api/v1/domains/"+itoa(dom.ID), "",
		http.StatusOK,
		wantEntry{authz.ActionDomainDelete, "shop.example.com", pid},
		wantEntry{authz.ActionAliasDelete, anything, pid})

	// --- api keys ---
	//
	// Key entries carry no project. A key is anchored to the account and reaches
	// whatever its grants name, which may be several projects or none, so there
	// is no single project the entry belongs to — and inventing one would make
	// the trail's project filter lie.
	rec = r.step(t, http.MethodPost, lbPath+"/keys", `{"note":"traefik","networks":["203.0.113.0/24"]}`,
		http.StatusCreated, wantEntry{authz.ActionKeyCreate, anything, nil})
	var created keyCreated
	decode(t, rec, &created)
	keyPath := "/api/v1/keys/" + itoa(created.Key.ID)
	assertLabel(t, cl, authz.ActionKeyCreate, created.Key.Identifier)

	r.step(t, http.MethodPut, keyPath+"/networks", `{"networks":["198.51.100.0/24"]}`,
		http.StatusOK, wantEntry{authz.ActionKeyRestrict, created.Key.Identifier, nil})
	r.step(t, http.MethodPut, keyPath+"/expiry", `{"expiresIn":"720h"}`,
		http.StatusOK, wantEntry{authz.ActionKeyExpiryChange, created.Key.Identifier, nil})

	rec = r.step(t, http.MethodPost, keyPath+"/rotate", "",
		http.StatusCreated, wantEntry{authz.ActionKeyRotate, anything, nil})
	var rotated keyCreated
	decode(t, rec, &rotated)
	assertLabel(t, cl, authz.ActionKeyRotate, rotated.Key.Identifier)

	r.step(t, http.MethodDelete, keyPath, "",
		http.StatusOK, wantEntry{authz.ActionKeyRevoke, created.Key.Identifier, nil})

	// The entries outlive what they describe: the project is gone, and its
	// trail is still there and still filterable by it.
	listing := cl.do(http.MethodGet, "/api/v1/audit?projectId="+itoa(proj.ID), "")
	var survivors []auditView
	decode(t, listing, &survivors)
	if len(survivors) < 10 {
		t.Fatalf("%d entries survive the deleted project, want the whole trail", len(survivors))
	}
}

// assertLabel checks the most recent entry for an action carries label. Used
// where the label is only known from the response — a key identifier is minted
// during the request.
func assertLabel(t *testing.T, cl *client, action, label string) {
	t.Helper()
	var es []auditView
	decode(t, cl.do(http.MethodGet, "/api/v1/audit?action="+action, ""), &es)
	if len(es) == 0 || es[0].ObjectLabel != label {
		t.Fatalf("%s: objectLabel %+v, want %q", action, es, label)
	}
}

func assertProjectID(t *testing.T, cl *client, action string, projectID model.ID) {
	t.Helper()
	var es []auditView
	decode(t, cl.do(http.MethodGet, "/api/v1/audit?action="+action, ""), &es)
	if len(es) == 0 || es[0].ProjectID == nil || *es[0].ProjectID != projectID {
		t.Fatalf("%s: projectId %+v, want %d", action, es, projectID)
	}
}

// TestActionClassFilterNarrowsTheTrail covers the config/credential filter:
// the two questions an audit trail is actually opened for.
func TestActionClassFilterNarrowsTheTrail(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	lbPath := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	if rec := cl.do(http.MethodPost, "/api/v1/projects", `{"slug":"gamma","name":"Gamma"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}
	if rec := cl.do(http.MethodPost, lbPath+"/keys", `{"note":"k"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct{ class, wantAction string }{
		{authz.ClassConfig, authz.ActionProjectCreate},
		{authz.ClassCredential, authz.ActionKeyCreate},
	} {
		var es []auditView
		decode(t, cl.do(http.MethodGet, "/api/v1/audit?actionClass="+tc.class, ""), &es)
		if len(es) == 0 {
			t.Fatalf("actionClass=%s returned nothing", tc.class)
		}
		for _, e := range es {
			if authz.ActionClass(e.Action) != tc.class {
				t.Errorf("actionClass=%s returned %q, which is %q", tc.class, e.Action, authz.ActionClass(e.Action))
			}
		}
		if es[0].Action != tc.wantAction {
			t.Errorf("actionClass=%s newest entry is %q, want %q", tc.class, es[0].Action, tc.wantAction)
		}
	}

	// A class nobody defined is a mistake, not an empty trail.
	if rec := cl.do(http.MethodGet, "/api/v1/audit?actionClass=nonsense", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("actionClass=nonsense: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// TestRolledBackChangeLeavesNoAuditEntry is the first half of "the entry is
// written atomically with the change": the entry is written, the change then
// fails before commit, and nothing at all persists — not the change, and not
// the record of it.
func TestRolledBackChangeLeavesNoAuditEntry(t *testing.T) {
	v := newAdminEnv(t)
	ctx := t.Context()

	boom := errors.New("the change failed after its audit entry was written")
	err := v.sc.Run(ctx, v.db, func(tx *gorm.DB) error {
		p, err := inventory.CreateProject(ctx, tx, v.sc, "doomed", "Doomed")
		if err != nil {
			return err
		}
		err = authz.Record(ctx, tx, nil, authz.SystemActor().InAccount(v.sc.AccountID()), authz.Event{
			Action: authz.ActionProjectCreate, ObjectType: authz.ObjectProject,
			ObjectID: &p.ID, ObjectLabel: p.Slug, ProjectID: &p.ID,
		})
		if err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("transaction error = %v, want %v", err, boom)
	}

	var projects, entries int64
	v.db.Model(&model.Project{}).Where("slug = ?", "doomed").Count(&projects)
	v.db.Model(&model.AuditEntry{}).Where("object_label = ?", "doomed").Count(&entries)
	if projects != 0 {
		t.Error("the rolled-back project persisted")
	}
	if entries != 0 {
		t.Error("an audit entry persisted for a change that was rolled back")
	}
}

// TestFailedAuditWriteRollsBackTheChange is the second half: if the entry
// cannot be written, the change must not be applied. The failure is injected
// in the database rather than faked in Go, so what is under test is the real
// path a full disk or a revoked grant would take.
func TestFailedAuditWriteRollsBackTheChange(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)

	stmts := []string{
		`create function refuse_audit() returns trigger language plpgsql as $$
		 begin raise exception 'audit store unavailable'; end $$`,
		`create trigger refuse_audit before insert on audit_entries
		 for each row execute function refuse_audit()`,
	}
	for _, q := range stmts {
		if err := v.db.Exec(q).Error; err != nil {
			t.Fatalf("install the failing audit writer: %v", err)
		}
	}

	rec := cl.do(http.MethodPost, "/api/v1/projects", `{"slug":"unrecorded","name":"Unrecorded"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create project with a broken audit store: %d %s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "nothing was applied") {
		t.Errorf("the error does not say the change was not applied: %s", rec.Body.String())
	}

	var projects int64
	v.db.Model(&model.Project{}).Where("slug = ?", "unrecorded").Count(&projects)
	if projects != 0 {
		t.Fatal("the project was created even though its audit entry could not be written")
	}
}

func TestPlanIsServedStructuredAndAsAZoneSnippet(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("reader", groupRead)
	lb := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	admin := v.session("admin", groupAdmin)
	for _, name := range []string{"example.com", "*.example.com"} {
		if rec := admin.do(http.MethodPost, lb+"/domains", `{"name":"`+name+`"}`); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	rec := cl.do(http.MethodGet, lb+"/plan", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body.String())
	}
	var plan planView
	decode(t, rec, &plan)
	cnames := 0
	for _, r := range plan.Records {
		if r.Type == "CNAME" {
			cnames++
		}
	}
	// The apex and its wildcard share one challenge name, so one CNAME.
	if cnames != 1 {
		t.Errorf("%d CNAME rows, want 1: %+v", cnames, plan.Records)
	}

	rec = cl.do(http.MethodGet, lb+"/plan/zone", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "IN  CNAME") {
		t.Fatalf("zone snippet: %d %s", rec.Code, rec.Body.String())
	}
}

// --- the plan-to-checks adapter (no database) ------------------------------

// The plan emits one row per address; resolvercheck compares a name's whole
// answer set at once. Getting that regrouping wrong reports a correct
// dual-stack load balancer as four mismatches, so it is checked directly.
func TestGroupForChecksCollapsesRowsPerNameAndFamily(t *testing.T) {
	records := []inventory.Record{
		{Name: "shop.example.com", Type: "A", Value: "203.0.113.10"},
		{Name: "shop.example.com", Type: "A", Value: "203.0.113.11"},
		{Name: "shop.example.com", Type: "AAAA", Value: "2001:db8:1::10"},
		{Name: "_acme-challenge.example.com", Type: "CNAME", Value: "example-com.acmeservice.example"},
		{Name: "example.com", Type: "CAA", Value: `0 issue "letsencrypt.org"`},
		{Name: "example.com", Type: "CAA", Value: `0 issuewild "letsencrypt.org"`},
	}
	checks := groupForChecks(records, []string{"letsencrypt.org"})
	if len(checks) != 4 {
		t.Fatalf("%d checks, want 4 (A, AAAA, CNAME, CAA): %+v", len(checks), checks)
	}

	byKind := map[string]planCheck{}
	for _, c := range checks {
		byKind[c.kind] = c
	}
	if a := byKind["A"]; a.address == nil || len(a.address.Want) != 2 {
		t.Errorf("the two A rows did not become one check with two addresses: %+v", a.address)
	}
	if aaaa := byKind["AAAA"]; aaaa.address == nil || len(aaaa.address.Want) != 1 {
		t.Errorf("AAAA check: %+v", aaaa.address)
	}
	if cn := byKind["CNAME"]; cn.cname == nil || !strings.HasSuffix(cn.cname.Expected, ".") {
		t.Errorf("the CNAME target must be fully qualified before it is compared: %+v", cn.cname)
	}
	if caa := byKind["CAA"]; caa.caa == nil || len(caa.caa.Issuers) != 1 {
		t.Errorf("the two CAA rows did not become one check: %+v", caa.caa)
	}
}

func TestVerificationResolverPrefersConfigurationAndAddsAPort(t *testing.T) {
	got := verificationResolver(config.Config{VerificationResolver: "1.1.1.1"}, discard())
	if got != "1.1.1.1:53" {
		t.Errorf("resolver %q, want 1.1.1.1:53", got)
	}
	if got := verificationResolver(config.Config{VerificationResolver: "1.1.1.1:5353"}, discard()); got != "1.1.1.1:5353" {
		t.Errorf("resolver %q, want the configured port kept", got)
	}
}

func TestParseCIDRsMasksHostBits(t *testing.T) {
	got, err := parseCIDRs([]string{"203.0.113.5/24", "2001:db8::1/32"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"203.0.113.0/24", "2001:db8::/32"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %q, want %q", got[i], want[i])
		}
	}
	if _, err := netip.ParsePrefix(got[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCIDRs([]string{"nope"}); err == nil {
		t.Error("a malformed CIDR was accepted")
	}
}

// The effective configuration is what an operator checks when someone cannot
// do what they should be able to — so it must carry the role mapping, and it
// must not carry the secrets.
func TestEffectiveConfigurationIsRedactedAndCarriesTheRoleMapping(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("reader", groupRead)
	rec := cl.do(http.MethodGet, "/api/v1/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{"super-secret-value", "hunter2"} {
		if strings.Contains(body, secret) {
			t.Errorf("the effective configuration leaked %q: %s", secret, body)
		}
	}
	var cfg struct {
		RoleGroups []authz.RoleGroup `json:"roleGroups"`
	}
	decode(t, rec, &cfg)
	if len(cfg.RoleGroups) != 4 {
		t.Fatalf("%d role rows, want all four: %+v", len(cfg.RoleGroups), cfg.RoleGroups)
	}
	for _, rg := range cfg.RoleGroups {
		if rg.Role == authz.RoleAdmin && rg.Group != groupAdmin {
			t.Errorf("ADMIN maps to %q, want %q", rg.Group, groupAdmin)
		}
	}
}

func itoa(n model.ID) string { return n.String() }

// --- paging, filtering and the time range ----------------------------------

// seedProjects creates n projects whose slugs sort in creation order, plus
// returns the ids, so a test can talk about "a row on a later page".
func (v *adminEnv) seedProjects(cl *client, slugs ...string) []model.ID {
	v.t.Helper()
	ids := make([]model.ID, 0, len(slugs))
	for _, slug := range slugs {
		rec := cl.do(http.MethodPost, "/api/v1/projects", `{"slug":"`+slug+`","name":"`+slug+`"}`)
		if rec.Code != http.StatusCreated {
			v.t.Fatalf("seed project %s: %d %s", slug, rec.Code, rec.Body.String())
		}
		var p projectView
		decode(v.t, rec, &p)
		ids = append(ids, p.ID)
	}
	return ids
}

func totalCount(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	v := rec.Header().Get(totalCountHeader)
	if v == "" {
		t.Fatalf("no %s header on %s", totalCountHeader, rec.Body.String())
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s = %q: %v", totalCountHeader, v, err)
	}
	return n
}

// The four properties a paged, filtered list screen is only honest if it has:
// a refused over-large limit (a silent clamp leaves the caller with a short
// list and no way to know), a total counted before paging (a pager cannot say
// "51-100 of 4,213" otherwise), a filter that runs in SQL rather than over the
// page, and a `_` that means an underscore.
func TestListPagingFilteringAndTotals(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	// "acme" already exists from the fixture; these make six.
	v.seedProjects(cl, "aa-first", "bb-second", "cc-third", "dd-fourth", "zz-last")

	// A limit above the maximum is refused, and the refusal names the bound.
	rec := cl.do(http.MethodGet, "/api/v1/projects?limit=501", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=501: %d %s, want 400", rec.Code, rec.Body.String())
	}
	var body ErrorBody
	decode(t, rec, &body)
	if !strings.Contains(body.Message, "500") {
		t.Errorf("the refusal %q does not name the maximum", body.Message)
	}
	for _, bad := range []string{"limit=0", "limit=-1", "limit=banana", "offset=-1", "offset=x"} {
		if rec := cl.do(http.MethodGet, "/api/v1/projects?"+bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", bad, rec.Code, rec.Body.String())
		}
	}

	// The total is the count of everything matching the filter, not the page.
	rec = cl.do(http.MethodGet, "/api/v1/projects?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var page []projectView
	decode(t, rec, &page)
	if len(page) != 2 {
		t.Fatalf("%d rows on a limit=2 page, want 2", len(page))
	}
	if got := totalCount(t, rec); got != 6 {
		t.Errorf("%s = %d on a two-row page, want the pre-paging count 6", totalCountHeader, got)
	}

	// zz-last is ordered by slug and so falls outside the first page, but the
	// filter runs in SQL: it comes back on page one of the FILTERED result.
	rec = cl.do(http.MethodGet, "/api/v1/projects?q=zz-last&limit=2", "")
	decode(t, rec, &page)
	if len(page) != 1 || page[0].Slug != "zz-last" {
		t.Fatalf("q=zz-last on a two-row page returned %+v, want the one matching row", page)
	}
	if got := totalCount(t, rec); got != 1 {
		t.Errorf("%s = %d for a filter matching one project, want 1", totalCountHeader, got)
	}

	// `_` is a LIKE wildcard, and a filter containing one must match it
	// literally — otherwise a search for "shop_eu" quietly returns everything.
	if rec := cl.do(http.MethodPatch, "/api/v1/projects/"+itoa(v.projID), `{"name":"Shop_EU"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	rec = cl.do(http.MethodGet, "/api/v1/projects?q=Shop_EU", "")
	decode(t, rec, &page)
	if len(page) != 1 || page[0].Name != "Shop_EU" {
		t.Fatalf("q=Shop_EU returned %+v, want the one project actually named that", page)
	}
	rec = cl.do(http.MethodGet, "/api/v1/projects?q=Shop_E", "")
	decode(t, rec, &page)
	if len(page) != 1 {
		t.Errorf("q=Shop_E returned %d projects; _ was treated as a wildcard", len(page))
	}
	rec = cl.do(http.MethodGet, "/api/v1/projects?q=ShopXEU", "")
	decode(t, rec, &page)
	if len(page) != 0 {
		t.Errorf("q=ShopXEU returned %+v; _ matched an arbitrary character", page)
	}
}

// The audit range picker submits bare dates, and a bare `to` means the END of
// the day named. Midnight would silently exclude everything recorded during the
// day the operator asked for — the most recent entries, which are the ones they
// opened the screen for.
func TestAuditTimeRangeCoversTheWholeDayNamed(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)

	if rec := cl.do(http.MethodPost, "/api/v1/projects", `{"slug":"today","name":"Today"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}
	// The bounds are parsed as UTC, so the day is named in UTC too.
	day := time.Now().UTC().Format("2006-01-02")

	var entries []auditView
	rec := cl.do(http.MethodGet, "/api/v1/audit?from="+day+"&to="+day, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("range: %d %s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &entries)
	if len(entries) == 0 {
		t.Fatalf("to=%s excluded an entry recorded during that day", day)
	}
	if totalCount(t, rec) != len(entries) {
		t.Errorf("%s = %d for %d entries", totalCountHeader, totalCount(t, rec), len(entries))
	}

	// A day before the entry excludes it, which is what makes the assertion
	// above a test of the bound rather than of an unfiltered listing.
	before := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	decode(t, cl.do(http.MethodGet, "/api/v1/audit?to="+before, ""), &entries)
	if len(entries) != 0 {
		t.Errorf("to=%s returned %d entries recorded after it", before, len(entries))
	}

	// An inverted range is a mistake, not an empty trail: an audit screen must
	// never answer "nothing happened" by accident.
	rec = cl.do(http.MethodGet, "/api/v1/audit?from="+day+"&to="+before, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("from=%s&to=%s: %d %s, want 400", day, before, rec.Code, rec.Body.String())
	}
	if rec := cl.do(http.MethodGet, "/api/v1/audit?from=yesterday", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("from=yesterday: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// The console loads a project by id rather than searching the list for it: with
// paging on, the list is one page and the project being opened may be on
// another. This is the regression that read as "project not found" for anyone
// past page one.
func TestProjectOutsideTheFirstPageIsStillReadableByID(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	ids := v.seedProjects(cl, "zz-last")
	last := ids[0]

	var page []projectView
	rec := cl.do(http.MethodGet, "/api/v1/projects?limit=1", "")
	decode(t, rec, &page)
	if len(page) != 1 || page[0].ID == last {
		t.Fatalf("first page %+v already contains project %d; the test proves nothing", page, last)
	}

	rec = cl.do(http.MethodGet, "/api/v1/projects/"+itoa(last), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get project %d by id: %d %s, want 200", last, rec.Code, rec.Body.String())
	}
	var got projectView
	decode(t, rec, &got)
	if got.ID != last || got.Slug != "zz-last" {
		t.Fatalf("got %+v, want project %d", got, last)
	}
	if rec := cl.do(http.MethodGet, "/api/v1/projects/00000000-0000-7000-8000-000000000999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project by id: %d, want 404", rec.Code)
	}
}

// --- the interaction history endpoint --------------------------------------

// seedInteraction inserts one history row for the fixture's load balancer.
// Written straight to the table rather than through the challenge API, because
// what is under test here is the READING: three of the five outcomes cannot be
// produced through /present and /cleanup at all without waiting for a sweep.
func (v *adminEnv) seedInteraction(label string, withdrawn bool, abandoned bool, queries int64) {
	v.t.Helper()
	in := &model.ChallengeInteraction{
		LoadBalancerID: &v.lb.ID,
		AliasID:        &v.lb.ID, // no alias is needed: the history keeps no foreign keys
		AliasLabel:     label,
		ChallengeFQDN:  "_acme-challenge." + label + ".example.com",
		DomainNames:    model.StringArray{label + ".example.com"},
		FQDNForm:       "alias",
		ValueDigest:    store.ValueDigest(label),
		PublishedAt:    time.Now(),
		Abandoned:      abandoned,
		QueryCount:     queries,
	}
	if withdrawn {
		now := time.Now()
		in.WithdrawnAt = &now
	}
	// challenge_interactions carries no account and no policy, so a direct
	// insert is still the honest fixture here — unlike every inventory table,
	// where one would be refused by the database.
	if err := v.db.Create(in).Error; err != nil {
		v.t.Fatalf("seed interaction %s: %v", label, err)
	}
}

// All five outcomes, the filter an operator actually acts on, the total, and
// the fact that a READ_ONLY user can open the screen at all.
//
// The outcome is derived on read rather than stored, so these five are the
// whole vocabulary — and `abandoned` is checked FIRST, which is why a swept row
// with no queries must read "abandoned" and not "unqueried": the two point at
// different people. One says the client vanished mid-validation, the other says
// the customer's CNAME is not published.
func TestInteractionOutcomesFilterAndTotal(t *testing.T) {
	v := newAdminEnv(t)
	reader := v.session("reader", groupRead)
	path := "/api/v1/loadbalancers/" + itoa(v.lb.ID) + "/interactions"

	v.seedInteraction("validated-one", true, false, 4)
	v.seedInteraction("unqueried-one", true, false, 0)
	v.seedInteraction("abandoned-one", true, true, 0)
	v.seedInteraction("live-one", false, false, 2)
	v.seedInteraction("pending-one", false, false, 0)
	// A swept row that WAS queried still reads as abandoned: the sweep is the
	// stronger fact about what happened to it.
	v.seedInteraction("abandoned-queried", true, true, 3)

	rec := reader.do(http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("READ_ONLY reading the history: %d %s, want 200", rec.Code, rec.Body.String())
	}
	var all []interactionView
	decode(t, rec, &all)
	if got := totalCount(t, rec); got != 6 {
		t.Errorf("%s = %d, want 6", totalCountHeader, got)
	}

	outcomes := map[string]string{}
	for _, in := range all {
		outcomes[in.AliasLabel] = in.Outcome
	}
	for label, want := range map[string]string{
		"validated-one":     "validated",
		"unqueried-one":     "unqueried",
		"abandoned-one":     "abandoned",
		"live-one":          "live",
		"pending-one":       "pending",
		"abandoned-queried": "abandoned",
	} {
		if outcomes[label] != want {
			t.Errorf("%s: outcome %q, want %q", label, outcomes[label], want)
		}
	}

	// The filter that earns its place: the rows nobody ever queried are the
	// list of customer CNAMEs to go and fix.
	rec = reader.do(http.MethodGet, path+"?unqueried=true", "")
	var unqueried []interactionView
	decode(t, rec, &unqueried)
	if len(unqueried) != 3 {
		t.Fatalf("unqueried=true returned %d rows, want the three with no queries: %+v", len(unqueried), unqueried)
	}
	for _, in := range unqueried {
		if in.QueryCount != 0 {
			t.Errorf("unqueried=true returned %s with %d queries", in.AliasLabel, in.QueryCount)
		}
	}
	if got := totalCount(t, rec); got != 3 {
		t.Errorf("%s = %d under the filter, want 3", totalCountHeader, got)
	}

	// The total stays the filtered count when a page is asked for.
	rec = reader.do(http.MethodGet, path+"?limit=2", "")
	decode(t, rec, &all)
	if len(all) != 2 || totalCount(t, rec) != 6 {
		t.Errorf("limit=2: %d rows and total %s, want 2 rows of 6", len(all), rec.Header().Get(totalCountHeader))
	}
	if rec := reader.do(http.MethodGet, path+"?limit=501", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("limit=501: %d, want 400", rec.Code)
	}

	// The free-text filter reaches the fields the screen shows.
	rec = reader.do(http.MethodGet, path+"?q=live-one.example.com", "")
	decode(t, rec, &all)
	if len(all) != 1 || all[0].AliasLabel != "live-one" {
		t.Fatalf("q over the domain names returned %+v, want the one row", all)
	}
}

// TestDomainCreateRefusesASuppliedAliasLabel is design A5: the label is
// system configuration, so a request that still carries the removed
// `aliasLabel` field is refused outright rather than having it silently
// dropped — a script that used to set it learns the first time it runs.
func TestDomainCreateRefusesASuppliedAliasLabel(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	lb := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	rec := cl.do(http.MethodPost, lb+"/domains", `{"name":"shop.example.com","aliasLabel":"shop-alias"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s, want 400 for an unknown aliasLabel field", rec.Code, rec.Body.String())
	}

	// And the service reports the label it chose itself.
	rec = cl.do(http.MethodPost, lb+"/domains", `{"name":"shop.example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s, want 201", rec.Code, rec.Body.String())
	}
	var dom domainCreated
	decode(t, rec, &dom)
	if dom.AliasLabel == "shop-alias" {
		t.Fatal("the refused label was used anyway")
	}
	if err := dnsname.ValidateLabel(dom.AliasLabel); err != nil {
		t.Errorf("assigned label %q is not a valid label: %v", dom.AliasLabel, err)
	}
}

// --- includeWildcard -------------------------------------------------------

// domainNamesOn lists what the load balancer holds, which is how "neither
// domain was created" is checked: a refusal that left a row behind is the
// failure this whole feature is designed around.
func domainNamesOn(t *testing.T, cl *client, lbPath string) []string {
	t.Helper()
	var listed []domainView
	decode(t, cl.do(http.MethodGet, lbPath+"/domains", ""), &listed)
	out := make([]string, 0, len(listed))
	for _, d := range listed {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

// TestDomainCreateIncludeWildcard covers the request field end to end: it
// creates the pair against one alias, it mirrors for a wildcard input, and its
// absence still creates exactly one domain (design B1, B2, B4).
func TestDomainCreateIncludeWildcard(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{"apex asks for its wildcard", `{"name":"shop.example.com","includeWildcard":true}`,
			[]string{"*.shop.example.com", "shop.example.com"}},
		{"wildcard asks for its apex", `{"name":"*.shop.example.com","includeWildcard":true}`,
			[]string{"*.shop.example.com", "shop.example.com"}},
		{"the field is absent", `{"name":"shop.example.com"}`,
			[]string{"shop.example.com"}},
		{"the field is false", `{"name":"shop.example.com","includeWildcard":false}`,
			[]string{"shop.example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newAdminEnv(t)
			cl := v.session("admin", groupAdmin)
			lbPath := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

			rec := cl.do(http.MethodPost, lbPath+"/domains", tc.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d %s, want 201", rec.Code, rec.Body.String())
			}
			var got domainCreated
			decode(t, rec, &got)

			if len(got.Domains) != len(tc.want) {
				t.Fatalf("response reports %d domains, want %d: %+v", len(got.Domains), len(tc.want), got.Domains)
			}
			// One alias label, shared: the pair costs no second thing for the
			// customer to publish.
			for _, d := range got.Domains {
				if d.AliasLabel != got.AliasLabel || d.AliasID != got.AliasID {
					t.Errorf("domain %q has alias %d/%q, want the shared %d/%q",
						d.Name, d.AliasID, d.AliasLabel, got.AliasID, got.AliasLabel)
				}
			}
			if got.Domains[0].Name != got.Name {
				t.Errorf("first reported domain is %q, want the submitted %q", got.Domains[0].Name, got.Name)
			}
			if listed := domainNamesOn(t, cl, lbPath); !reflect.DeepEqual(listed, tc.want) {
				t.Errorf("inventory holds %v, want %v", listed, tc.want)
			}
		})
	}
}

// TestDomainCreateIncludeWildcardRefusesTheWholeRequest is decision B3 through
// the API. `co.uk` is a valid plain name whose counterpart `*.co.uk` sits at a
// public suffix; the request is refused naming that counterpart, and the valid
// half is not created either.
func TestDomainCreateIncludeWildcardRefusesTheWholeRequest(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	lbPath := "/api/v1/loadbalancers/" + itoa(v.lb.ID)

	// The premise: without the flag, `co.uk` is accepted.
	rec := cl.do(http.MethodPost, lbPath+"/domains", `{"name":"co.uk","includeWildcard":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "*.co.uk") {
		t.Errorf("error %s does not name the refused counterpart *.co.uk", rec.Body.String())
	}
	if listed := domainNamesOn(t, cl, lbPath); len(listed) != 0 {
		t.Fatalf("inventory holds %v after the refusal, want nothing — not even the valid half", listed)
	}
}

// TestDomainPairIsAuditedTwice: two domains are created, so the trail records
// two objects, both from the transaction that wrote the rows (design B1).
func TestDomainPairIsAuditedTwice(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", groupAdmin)
	r := &auditRecorder{cl: cl}
	lbPath := "/api/v1/loadbalancers/" + itoa(v.lb.ID)
	pid := &v.lb.ProjectID

	r.step(t, http.MethodPost, lbPath+"/domains", `{"name":"shop.example.com","includeWildcard":true}`,
		http.StatusCreated,
		wantEntry{authz.ActionDomainCreate, "shop.example.com", pid},
		wantEntry{authz.ActionDomainCreate, "*.shop.example.com", pid},
		// One alias for the pair, so one alias entry.
		wantEntry{authz.ActionAliasCreate, anything, pid})

	// A refused pair writes nothing at all — the transaction rolled back.
	r.step(t, http.MethodPost, lbPath+"/domains", `{"name":"co.uk","includeWildcard":true}`,
		http.StatusBadRequest)
}
