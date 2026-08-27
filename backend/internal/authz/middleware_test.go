package authz

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// What is left of this file after design C10 is worth stating, because most of
// it was deleted rather than rewritten.
//
// The Guard used to do two jobs: establish who is asking, and decide what they
// may do — deriving four global roles from the session's group claim through
// four environment variables, refusing an authenticated user in none of those
// groups, and enforcing a route's required permission. Everything in the second
// job is gone: there are no global roles, no mapping, and no route middleware.
// Authority is now a membership row in one account, resolved once the request's
// account is known, and tested against real rows in equivalence_test.go and in
// internal/httpapi.
//
// So the tests here are about authentication and nothing else, which is all the
// Guard still claims to do.

// fakeAuth stands in for *oidcauth.Service. Setting err is how a test says
// "this session has been revoked, expired, or was never valid".
type fakeAuth struct {
	sess *model.Session
	err  error
}

func (f *fakeAuth) Authenticate(context.Context, *http.Request) (*model.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sess, nil
}

func sessionIn(groups ...string) *model.Session {
	return &model.Session{ID: "sid", Subject: "u-1", DisplayName: "Ada", Groups: groups}
}

// serve runs one request through Authenticated and reports the status, so a
// test can check that the session reaches the handler.
func serve(t *testing.T, g *Guard) (int, string) {
	t.Helper()
	e := echo.New()
	h := func(c echo.Context) error {
		if SessionFromContext(c) == nil {
			t.Error("handler ran without a session in the context")
		}
		return c.String(http.StatusOK, "ok")
	}
	e.GET("/x", h, g.Authenticated())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	return rec.Code, rec.Body.String()
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func guard(auth Authenticator) *Guard {
	return &Guard{Auth: auth, Log: discardLogger()}
}

func TestMiddlewareUnauthenticatedIs401(t *testing.T) {
	g := guard(&fakeAuth{err: errors.New("no session")})
	if code, _ := serve(t, g); code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", code)
	}
}

func TestMiddlewareRevokedSessionEndsAccessAtTheNextRequest(t *testing.T) {
	auth := &fakeAuth{sess: sessionIn("acme-admins")}
	g := guard(auth)

	if code, body := serve(t, g); code != http.StatusOK {
		t.Fatalf("before revocation: status = %d, want 200 (body %s)", code, body)
	}

	// An administrator revokes it: Authenticate stops resolving it.
	auth.err = errors.New("oidcauth: session invalid: revoked")

	if code, _ := serve(t, g); code != http.StatusUnauthorized {
		t.Errorf("after revocation: status = %d, want 401", code)
	}
}

// A person in no group at all is now admitted, and that is the change rather
// than an oversight (task 2.3).
//
// The Guard used to refuse them with a 403 naming the groups it had observed,
// because holding no mapped group meant holding no authority anywhere. It no
// longer means that: every authenticated person has their own personal account
// (design C1), so being signed in and permitted nothing is not a state the
// service can be configured into. What they may do in any particular account is
// decided per account, and being refused there says which account.
func TestMiddlewareAdmitsAnAuthenticatedUserInNoGroup(t *testing.T) {
	g := guard(&fakeAuth{sess: sessionIn()})
	if code, body := serve(t, g); code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a user in no group has their own account (body %s)", code, body)
	}
}

// The groups claim is still carried on the session even though nothing grants
// on it: it is what the identity provider said about this person, and the audit
// trail records it beside the roles they held.
func TestMiddlewareCarriesTheGroupsClaimToTheHandler(t *testing.T) {
	e := echo.New()
	var seen []string
	e.GET("/x", func(c echo.Context) error {
		seen = SessionFromContext(c).Groups
		return c.String(http.StatusOK, "ok")
	}, guard(&fakeAuth{sess: sessionIn("acme-admins", "everyone")}).Authenticated())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if len(seen) != 2 || seen[0] != "acme-admins" {
		t.Errorf("groups = %v, want the claim as seen at sign-in", seen)
	}
}
