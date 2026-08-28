package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/webui"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// newServers builds the real three-listener set against a migrated database,
// with the console mounted. Everything asserted below is about which listener
// answers what, so the doubles from admin_test.go are enough for the session
// side.
func newServers(t *testing.T) (*Servers, *gorm.DB) {
	t.Helper()
	db := migratedDB(t)
	cfg := config.Config{
		Zone:               "acmeservice.example.",
		SessionIdleTimeout: time.Hour,
		SessionMaxLifetime: 8 * time.Hour,
	}
	m := obs.New()

	s, err := NewServers(t.Context(), ServerOptions{
		Challenge: ChallengeOptions{
			DB: db, Zone: cfg.Zone, View: zoneview.New(zoneview.FromDB(db)),
			Lookup: dbLookup(db), Lifetime: time.Hour, Metrics: m, Log: discard(),
		},
		Admin: AdminOptions{
			DB: db, Guard: &authz.Guard{Auth: &testAuth{db: db}, Log: discard()},
			Auth: testConsoleAuth{}, Config: cfg, Log: discard(),
		},
		Console:   webui.Handler(cfg.StaffHost),
		Liveness:  obs.Liveness(),
		Readiness: obs.Readiness(m, func() bool { return true }, func(context.Context) error { return nil }),
		Metrics:   m,
		Log:       discard(),
	})
	if err != nil {
		t.Fatalf("NewServers: %v", err)
	}
	return s, db
}

// signedIn inserts a session row and returns the header value that testAuth
// resolves it by, so a route table test can get past the session guard.
func signedIn(t *testing.T, db *gorm.DB) string {
	t.Helper()
	now := time.Now().UTC()
	s := &model.Session{
		ID: "server-test-" + hexish(), Subject: "tester", DisplayName: "Tester",
		Groups: model.StringArray{roleAdmin}, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateSession(t.Context(), db, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s.ID
}

func get(t *testing.T, e *echo.Echo, method, path string, session ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if len(session) == 1 {
		req.Header.Set("X-Test-Session", session[0])
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// Each path group carries its intended middleware (design P16, task 10.1).
//
// This replaces a test asserting that no listener mounted another's routes.
// That property came from network topology: three listeners, and a stolen API
// key reached no management route even if a routing rule in front of us was
// wrong. There is one listener now, and the guarantee is carried by the
// permission model instead — a key reaches only what its grants permit.
//
// So what is worth asserting changed shape. Not "these paths are absent here",
// but "these paths are guarded there": the management API needs a session, the
// challenge protocol needs a key, and the health endpoints need neither.
func TestEachPathGroupCarriesItsMiddleware(t *testing.T) {
	s, _ := newServers(t)

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		why        string
	}{
		{
			name:   "the management API refuses an unauthenticated caller",
			method: http.MethodGet, path: "/api/v1/me", wantStatus: http.StatusUnauthorized,
			why: "the session guard, not the absence of a route",
		},
		{
			name:   "the staff tree is present and empty",
			method: http.MethodGet, path: "/api/admin/anything", wantStatus: http.StatusNotFound,
			why: "a separate tree, reserved for staff-plane, answering so a caller learns the surface exists",
		},
		{
			name:   "the challenge protocol refuses an unauthenticated caller",
			method: http.MethodPost, path: "/present", wantStatus: http.StatusUnauthorized,
			why: "the API-key middleware",
		},
		{
			name:   "the namespaced challenge path is guarded identically",
			method: http.MethodPost, path: "/acme/httpreq/present", wantStatus: http.StatusUnauthorized,
			why: "the same handler and the same middleware, under the prefix design P16 gives it",
		},
		{
			name:   "the bare challenge path still serves",
			method: http.MethodPost, path: "/cleanup", wantStatus: http.StatusUnauthorized,
			why: "every existing deployment has the old endpoint configured; upgrading must not move it",
		},
		{
			name:   "health needs nothing",
			method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK,
			why: "a probe cannot hold a session",
		},
		{
			name:   "readiness needs nothing",
			method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK,
		},
		{
			name:   "metrics are served",
			method: http.MethodGet, path: "/metrics", wantStatus: http.StatusOK,
			why: "on the same listener as everything else now, so keeping it private is the proxy's job",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := get(t, s.Public, c.method, c.path)
			if rec.Code != c.wantStatus {
				t.Errorf("%s %s: status %d, want %d — %s", c.method, c.path, rec.Code, c.wantStatus, c.why)
			}
		})
	}
}

func TestConsoleFallback(t *testing.T) {
	s, db := newServers(t)
	session := signedIn(t, db)

	deep := get(t, s.Public, http.MethodGet, "/projects/7/keys")
	if deep.Code != http.StatusOK {
		t.Fatalf("deep link: status %d, want 200", deep.Code)
	}
	if ct := deep.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("deep link content type %q, want text/html", ct)
	}
	if cc := deep.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("entry document Cache-Control %q, want no-store", cc)
	}

	// Signed in, so what comes back is the router's answer for an unknown
	// path rather than the session guard's 401.
	api := get(t, s.Public, http.MethodGet, "/api/v1/no-such-endpoint", session)
	if api.Code != http.StatusNotFound {
		t.Errorf("unknown API path: status %d, want 404", api.Code)
	}
	if ct := api.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("unknown API path content type %q, want JSON — an HTML 404 here is the confusing bug report D9 warns about", ct)
	}

	// A missing file stays a missing file rather than arriving as HTML with
	// a 200, which would break the browser's script loading in silence.
	if rec := get(t, s.Public, http.MethodGet, "/nope-1234.js"); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset: status %d, want 404", rec.Code)
	}
}

// hashedRef finds a content-hashed asset by reading the entry document, so
// this works against whatever `ng build` last produced without naming a hash
// that changes on every build.
var hashedRef = regexp.MustCompile(`(?:src|href)="([^"]*-[A-Za-z0-9_]{8,}\.(?:js|css))"`)

func TestHashedAssetIsCacheable(t *testing.T) {
	s, _ := newServers(t)

	index := get(t, s.Public, http.MethodGet, "/")
	body, _ := io.ReadAll(index.Body)
	match := hashedRef.FindSubmatch(body)
	if match == nil {
		t.Skip("skipping: the embedded console has no content-hashed asset — run `ng build admin` first")
	}

	rec := get(t, s.Public, http.MethodGet, "/"+strings.TrimPrefix(string(match[1]), "/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset %s: status %d, want 200", match[1], rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset %s Cache-Control %q, want a long-lived immutable directive", match[1], cc)
	}
}

// The documentation page's mark comes out of this binary too (design W2, task
// 6.2).
//
// openapi_test.go proves the page names no other host. It cannot prove the last
// step, because two of the page's references — the mark and the favicon — are
// the CONSOLE's copies, served by internal/webui rather than by the docs route.
// That is deliberate: one mark, one file, and a documentation page whose logo
// could drift from the console's would be worse than one that shares it. The
// cost is that only a server with the console mounted can show the sharing
// actually works, which is here.
func TestSwaggerPageBrandAssetsAreServedByThisService(t *testing.T) {
	s, _ := newServers(t)

	page := get(t, s.Public, http.MethodGet, "/api/swagger")
	if page.Code != http.StatusOK {
		t.Fatalf("GET /api/swagger: status %d, want 200", page.Code)
	}
	body := page.Body.String()

	for _, ref := range []string{"/brand/logo.svg", "/brand/favicon.svg"} {
		if !strings.Contains(body, ref) {
			continue
		}
		rec := get(t, s.Public, http.MethodGet, ref)
		if rec.Code == http.StatusNotFound {
			t.Skipf("skipping: the embedded console has no %s — run `ng build console` first", ref)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("the documentation page asks for %s and this service answers %d", ref, rec.Code)
		}
	}
}

// The reader is not a way past anything (design W5, task 3.4).
//
// Requests fired from the page go to the same /api/v1 routes, through the same
// middleware, as any other client's — there is no proxy and no second entrance.
// What that means in practice is that the page's own first request, the one that
// works out which account is selected, is refused without a session exactly like
// every other read. If that ever answers 200 to nobody, the docs route has grown
// an authority it must not have.
func TestSwaggerPageIsNoWayPastTheSessionGuard(t *testing.T) {
	s, _ := newServers(t)

	if rec := get(t, s.Public, http.MethodGet, "/api/v1/accounts"); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/accounts unauthenticated: status %d, want 401", rec.Code)
	}
	// And the docs tree serves files, not operations: a path that climbs out of
	// it is a 404 rather than a route somewhere else.
	if rec := get(t, s.Public, http.MethodGet, "/api/swagger/../v1/accounts"); rec.Code == http.StatusOK {
		t.Error("a traversal out of the documentation asset tree answered 200")
	}
}
