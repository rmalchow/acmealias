package oidcauth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

const testDatabaseURLEnv = "TEST_DATABASE_URL"

// testDB skips unless TEST_DATABASE_URL names a live PostgreSQL. Sessions are
// keyed by a random 256-bit id, so these tests do not need a schema of their
// own to stay clear of each other.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(testDatabaseURLEnv)
	if dsn == "" {
		t.Skipf("skipping: %s is not set — this test needs a live PostgreSQL, e.g. "+
			`%s="postgres://user:pass@localhost:5432/acmeservice_test?sslmode=disable"`,
			testDatabaseURLEnv, testDatabaseURLEnv)
	}
	if err := store.MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := store.Open(store.Options{DatabaseURL: dsn, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// requestWith builds a request carrying the cookies a response set.
func requestWith(rec *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}

// TestIntegrationSignInSessionLifecycle walks the whole thing: a valid
// callback establishes a session, the cookie it sets authenticates the next
// request, and revoking it — by signing out, or by an administrator calling
// store.RevokeSession — unauthenticates the one after that.
func TestIntegrationSignInSessionLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	idp := newStubIDP(t)
	s := idp.service()
	s.db = db

	flowCookie, fs, _ := startFlow(t, s)
	idp.token = idp.sign(t, map[string]any{
		"nonce":  fs.Nonce,
		"sub":    "f:1234:alice",
		"name":   "Alice Example",
		"groups": []string{"/acme/admins"},
	})

	// Replay the flow cookie into the callback.
	rec := httptest.NewRecorder()
	if err := s.Callback(rec, callbackRequest(flowCookie, fs.State)); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback = %d %q, want 302 to /", rec.Code, rec.Header().Get("Location"))
	}

	var sessCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName && c.Value != "" {
			sessCookie = c
		}
	}
	if sessCookie == nil {
		t.Fatal("callback set no session cookie")
	}
	assertCookieAttrs(t, sessCookie)

	sess, err := s.Authenticate(ctx, requestWith(rec))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if sess.Subject != "f:1234:alice" || sess.DisplayName != "Alice Example" {
		t.Errorf("session = %+v, want alice", sess)
	}
	if len(sess.Groups) != 1 || sess.Groups[0] != "admins" {
		t.Errorf("groups = %v, want [admins]", sess.Groups)
	}

	// An administrator revoking the session takes effect on the next request.
	if err := store.RevokeSession(ctx, db, sess.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.Authenticate(ctx, requestWith(rec)); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("after revocation err = %v, want ErrSessionInvalid", err)
	}

	// An unknown cookie value is invalid; no cookie at all is simply not signed in.
	bogus := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	bogus.AddCookie(newCookie(SessionCookieName, randomToken(), time.Hour))
	if _, err := s.Authenticate(ctx, bogus); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("unknown session err = %v, want ErrSessionInvalid", err)
	}
	if _, err := s.Authenticate(ctx, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no cookie err = %v, want ErrNoSession", err)
	}
}

// TestIntegrationLogoutEndsTheSession checks sign-out from the other end: the
// cookie is cleared and the session no longer authenticates.
func TestIntegrationLogoutEndsTheSession(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	idp := newStubIDP(t)
	s := idp.service()
	s.db = db

	sess, err := s.createSession(ctx, &Identity{Subject: "bob", DisplayName: "Bob", Groups: []string{"readers"}})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.AddCookie(newCookie(SessionCookieName, sess.ID, time.Hour))
	rec := httptest.NewRecorder()
	if err := s.Logout(ctx, rec, r); err != nil {
		t.Fatalf("logout: %v", err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName && c.MaxAge >= 0 {
			t.Errorf("logout left the session cookie live: %+v", c)
		}
	}

	again := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	again.AddCookie(newCookie(SessionCookieName, sess.ID, time.Hour))
	if _, err := s.Authenticate(ctx, again); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("after logout err = %v, want ErrSessionInvalid", err)
	}
}

// TestIntegrationIdleAndAbsoluteExpiry drives both timeouts through the real
// store rather than the pure check, so a session whose timestamps live in the
// database is rejected the same way.
func TestIntegrationIdleAndAbsoluteExpiry(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	idp := newStubIDP(t)
	s := idp.service()
	s.db = db
	s.cfg.SessionIdleTimeout = time.Hour
	s.cfg.SessionMaxLifetime = 8 * time.Hour

	req := func(id string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		r.AddCookie(newCookie(SessionCookieName, id, time.Hour))
		return r
	}

	idleOut, err := s.createSession(ctx, &Identity{Subject: "idle"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stale := time.Now().UTC().Add(-2 * time.Hour)
	if err := db.Exec("update sessions set last_seen_at = ? where id = ?", stale, idleOut.ID).Error; err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := s.Authenticate(ctx, req(idleOut.ID)); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("idle session err = %v, want ErrSessionInvalid", err)
	}

	oldOut, err := s.createSession(ctx, &Identity{Subject: "old"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	past := time.Now().UTC().Add(-time.Second)
	if err := db.Exec("update sessions set expires_at = ? where id = ?", past, oldOut.ID).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if _, err := s.Authenticate(ctx, req(oldOut.ID)); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expired session err = %v, want ErrSessionInvalid", err)
	}
}
