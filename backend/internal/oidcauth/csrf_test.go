package oidcauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// csrfServer returns an Echo instance with the middleware mounted and one
// handler that answers 200 to anything, plus the session cookie a signed-in
// browser would be carrying.
func csrfServer(t *testing.T) (*echo.Echo, *http.Cookie) {
	t.Helper()
	idp := newStubIDP(t)
	e := echo.New()
	e.Use(idp.service().CSRF())
	e.Any("/api/v1/projects", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	return e, newCookie(SessionCookieName, randomToken(), time.Hour)
}

// issueToken performs the safe request that hands the browser its CSRF
// cookie, and returns that cookie.
func issueToken(t *testing.T, e *echo.Echo, session *http.Cookie) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	r.AddCookie(session)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie was issued", CSRFCookieName)
	return nil
}

// TestCSRFRejectsStateChangingRequestWithoutTheHeader is the load balancer the spec
// asks for: the same request, with and without the header.
func TestCSRFRejectsStateChangingRequestWithoutTheHeader(t *testing.T) {
	e, session := csrfServer(t)
	token := issueToken(t, e, session)

	post := func(header string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
		r.AddCookie(session)
		r.AddCookie(token)
		if header != "" {
			r.Header.Set(CSRFHeaderName, header)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, r)
		return rec.Code
	}

	if code := post(""); code < 400 {
		t.Errorf("POST without %s = %d, want a client error", CSRFHeaderName, code)
	}
	if code := post("a-token-the-attacker-guessed"); code < 400 {
		t.Errorf("POST with a wrong %s = %d, want a client error", CSRFHeaderName, code)
	}
	if code := post(token.Value); code != http.StatusOK {
		t.Errorf("POST with the matching %s = %d, want 200", CSRFHeaderName, code)
	}
}

// A browser that sends Fetch Metadata must not get a way past the token:
// Echo's Sec-Fetch-Site fast path is deliberately disabled (see csrf.go).
func TestCSRFIgnoresSecFetchSite(t *testing.T) {
	e, session := csrfServer(t)
	token := issueToken(t, e, session)

	for _, site := range []string{"same-origin", "none", "same-site", "cross-site"} {
		t.Run(site, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
			r.AddCookie(session)
			r.AddCookie(token)
			r.Header.Set(echo.HeaderSecFetchSite, site)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, r)
			if rec.Code < 400 {
				t.Errorf("POST with Sec-Fetch-Site: %s and no token = %d, want a client error", site, rec.Code)
			}
		})
	}
}

// The CSRF cookie is the one cookie that must be script-readable; every other
// attribute still matches the session cookie.
func TestCSRFCookieIsReadableByTheBrowser(t *testing.T) {
	e, session := csrfServer(t)
	token := issueToken(t, e, session)

	if token.HttpOnly {
		t.Error("XSRF-TOKEN is HttpOnly; the browser could not read it back to send the header")
	}
	if !token.Secure {
		t.Error("XSRF-TOKEN: Secure is not set")
	}
	if token.SameSite != http.SameSiteLaxMode {
		t.Errorf("XSRF-TOKEN: SameSite = %v, want Lax", token.SameSite)
	}
	if token.Domain != "" {
		t.Errorf("XSRF-TOKEN: Domain = %q, want host-only", token.Domain)
	}
	if token.Value == "" {
		t.Error("XSRF-TOKEN is empty")
	}
}

// Safe methods carry no token and must still work, or the console cannot read
// anything.
func TestCSRFAllowsSafeMethods(t *testing.T) {
	e, session := csrfServer(t)
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		r := httptest.NewRequest(m, "/api/v1/projects", nil)
		r.AddCookie(session)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", m, rec.Code)
		}
	}
}
