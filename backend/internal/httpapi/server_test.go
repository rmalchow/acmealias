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
		AdminGroup:         groupAdmin,
	}
	m := obs.New()

	s, err := NewServers(t.Context(), ServerOptions{
		Challenge: ChallengeOptions{
			DB: db, Zone: cfg.Zone, View: zoneview.New(zoneview.FromDB(db)),
			Lookup: dbLookup(db), Lifetime: time.Hour, Metrics: m, Log: discard(),
		},
		Admin: AdminOptions{
			DB: db, Guard: &authz.Guard{Auth: &testAuth{db: db}, Mapping: authz.MappingFromConfig(cfg), Log: discard()},
			Auth: testConsoleAuth{}, Config: cfg, Log: discard(),
		},
		Console:   webui.Handler(),
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
		Groups: model.StringArray{groupAdmin}, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
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

// No listener serves another's routes — the separation is the security
// property, not a tidiness one (design D9).
func TestListenersServeOnlyTheirOwnRoutes(t *testing.T) {
	s, _ := newServers(t)

	cases := []struct {
		listener   string
		e          *echo.Echo
		method     string
		path       string
		wantStatus int
	}{
		{"challenge", s.Challenge, http.MethodGet, "/api/v1/me", http.StatusNotFound},
		{"challenge", s.Challenge, http.MethodGet, "/api/v1/projects", http.StatusNotFound},
		{"challenge", s.Challenge, http.MethodGet, "/metrics", http.StatusNotFound},
		{"challenge", s.Challenge, http.MethodGet, "/healthz", http.StatusNotFound},
		{"challenge", s.Challenge, http.MethodGet, "/readyz", http.StatusNotFound},
		{"challenge", s.Challenge, http.MethodGet, "/", http.StatusNotFound},
		{"admin", s.Admin, http.MethodGet, "/metrics", http.StatusOK}, // console fallback, NOT metrics
		// 405, not 404: the console fallback claims /* for GET and HEAD, so
		// Echo answers "method not allowed" for a POST. Either way the
		// challenge handler is not reachable here, which is the property.
		{"admin", s.Admin, http.MethodPost, "/present", http.StatusMethodNotAllowed},
		{"admin", s.Admin, http.MethodPost, "/cleanup", http.StatusMethodNotAllowed},
		{"internal", s.Internal, http.MethodGet, "/metrics", http.StatusOK},
		{"internal", s.Internal, http.MethodGet, "/healthz", http.StatusOK},
		{"internal", s.Internal, http.MethodGet, "/readyz", http.StatusOK},
		{"internal", s.Internal, http.MethodPost, "/present", http.StatusNotFound},
		{"internal", s.Internal, http.MethodGet, "/api/v1/me", http.StatusNotFound},
	}
	for _, c := range cases {
		rec := get(t, c.e, c.method, c.path)
		if rec.Code != c.wantStatus {
			t.Errorf("%s listener %s %s: status %d, want %d", c.listener, c.method, c.path, rec.Code, c.wantStatus)
		}
	}

	// The admin listener answering /metrics at all is only acceptable because
	// what it returns is the console, not the metrics exposition.
	if body := get(t, s.Admin, http.MethodGet, "/metrics").Body.String(); strings.Contains(body, "acmeservice_http_requests_total") {
		t.Error("admin listener served the metrics exposition")
	}
	if body := get(t, s.Internal, http.MethodGet, "/metrics").Body.String(); !strings.Contains(body, "acmeservice_") {
		t.Errorf("internal /metrics did not expose acmeservice metrics: %q", body)
	}
}

// A deep link loads the application; a typo'd API path stays JSON.
func TestConsoleFallback(t *testing.T) {
	s, db := newServers(t)
	session := signedIn(t, db)

	deep := get(t, s.Admin, http.MethodGet, "/projects/7/keys")
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
	api := get(t, s.Admin, http.MethodGet, "/api/v1/no-such-endpoint", session)
	if api.Code != http.StatusNotFound {
		t.Errorf("unknown API path: status %d, want 404", api.Code)
	}
	if ct := api.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("unknown API path content type %q, want JSON — an HTML 404 here is the confusing bug report D9 warns about", ct)
	}

	// A missing file stays a missing file rather than arriving as HTML with
	// a 200, which would break the browser's script loading in silence.
	if rec := get(t, s.Admin, http.MethodGet, "/nope-1234.js"); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset: status %d, want 404", rec.Code)
	}
}

// hashedRef finds a content-hashed asset by reading the entry document, so
// this works against whatever `ng build` last produced without naming a hash
// that changes on every build.
var hashedRef = regexp.MustCompile(`(?:src|href)="([^"]*-[A-Za-z0-9_]{8,}\.(?:js|css))"`)

func TestHashedAssetIsCacheable(t *testing.T) {
	s, _ := newServers(t)

	index := get(t, s.Admin, http.MethodGet, "/")
	body, _ := io.ReadAll(index.Body)
	match := hashedRef.FindSubmatch(body)
	if match == nil {
		t.Skip("skipping: the embedded console has no content-hashed asset — run `ng build admin` first")
	}

	rec := get(t, s.Admin, http.MethodGet, "/"+strings.TrimPrefix(string(match[1]), "/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset %s: status %d, want 200", match[1], rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset %s Cache-Control %q, want a long-lived immutable directive", match[1], cc)
	}
}
