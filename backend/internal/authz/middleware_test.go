package authz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

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

// serve runs one request through Authenticated + Require(action) and reports
// the status and body. It also records what the handler saw, so the tests can
// check that the session and roles reach the handler.
func serve(t *testing.T, g *Guard, action Action) (int, string, RoleSet) {
	t.Helper()
	e := echo.New()
	var seen RoleSet
	h := func(c echo.Context) error {
		seen = RolesFromContext(c)
		if SessionFromContext(c) == nil {
			t.Error("handler ran without a session in the context")
		}
		return c.String(http.StatusOK, "ok")
	}
	e.GET("/x", h, g.Authenticated(), Require(action))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	return rec.Code, rec.Body.String(), seen
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func guard(auth Authenticator) *Guard {
	return &Guard{Auth: auth, Mapping: maps(testMapping), Log: discardLogger()}
}

// maps copies the shared testMapping so a test that mutates it cannot leak
// into another.
func maps(m Mapping) Mapping {
	out := Mapping{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestMiddlewareUnauthenticatedIs401(t *testing.T) {
	g := guard(&fakeAuth{err: errors.New("no session")})
	code, body, _ := serve(t, g, ActionViewInventory)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", code, body)
	}
}

func TestMiddlewareRevokedSessionEndsAccessAtTheNextRequest(t *testing.T) {
	auth := &fakeAuth{sess: sessionIn("acme-admins")}
	g := guard(auth)

	if code, body, _ := serve(t, g, ActionProjectAdmin); code != http.StatusOK {
		t.Fatalf("before revocation: status = %d, want 200 (body %s)", code, body)
	}

	// An administrator revokes it: Authenticate stops resolving it.
	auth.err = errors.New("oidcauth: session invalid: revoked")

	if code, _, _ := serve(t, g, ActionProjectAdmin); code != http.StatusUnauthorized {
		t.Errorf("after revocation: status = %d, want 401", code)
	}
}

func TestMiddlewareNoMappedGroupReportsTheGroupsObserved(t *testing.T) {
	// The "Full group path" mistake: the claim carries paths, the mapping has
	// leaf names, nothing matches.
	observed := []string{"/acme/acme-admins", "/everyone"}
	g := guard(&fakeAuth{sess: sessionIn(observed...)})

	code, body, _ := serve(t, g, ActionViewInventory)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", code, body)
	}

	var got NoRoleBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if !slices.Equal(got.ObservedGroups, observed) {
		t.Errorf("observed_groups = %v, want %v — without them a mapping typo cannot be diagnosed",
			got.ObservedGroups, observed)
	}
	if got.Message == "" {
		t.Error("the refusal carries no explanation")
	}
}

func TestMiddlewareNoRoleBodyDoesNotLeakTheConfiguredGroups(t *testing.T) {
	// Diagnosis needs what the user presented, not what the server expects:
	// the configured group names go to the log, where the operator already is,
	// not to a caller who has just been refused.
	g := guard(&fakeAuth{sess: sessionIn("everyone")})

	_, body, _ := serve(t, g, ActionViewInventory)
	for _, group := range testMapping {
		if strings.Contains(body, group) {
			t.Errorf("the 403 body leaks the configured group %q: %s", group, body)
		}
	}
}

func TestMiddlewareEnforcesTheMatrixPerRoute(t *testing.T) {
	tests := []struct {
		group  string
		action Action
		want   int
	}{
		{"acme-admins", ActionProjectAdmin, http.StatusOK},
		{"acme-config", ActionProjectAdmin, http.StatusForbidden},
		{"acme-keys", ActionProjectAdmin, http.StatusForbidden},
		{"acme-viewers", ActionProjectAdmin, http.StatusForbidden},

		{"acme-config", ActionInventoryWrite, http.StatusOK},
		{"acme-keys", ActionInventoryWrite, http.StatusForbidden},
		{"acme-viewers", ActionInventoryWrite, http.StatusForbidden},

		{"acme-keys", ActionKeyWrite, http.StatusOK},
		{"acme-viewers", ActionKeyWrite, http.StatusForbidden},

		{"acme-viewers", ActionDNSPlan, http.StatusOK},
		{"acme-viewers", ActionViewInventory, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.group+"/"+string(tc.action), func(t *testing.T) {
			g := guard(&fakeAuth{sess: sessionIn(tc.group)})
			code, body, _ := serve(t, g, tc.action)
			if code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", code, tc.want, body)
			}
		})
	}
}

func TestMiddlewareRolesComeFromTheCurrentConfiguration(t *testing.T) {
	// Same session throughout: its groups claim is fixed at sign-in. What
	// changes is the mapping, which is re-read on every request rather than
	// cached on the session.
	auth := &fakeAuth{sess: sessionIn("acme-admins")}
	g := guard(auth)

	if code, _, roles := serve(t, g, ActionProjectAdmin); code != http.StatusOK || !roles.Has(RoleAdmin) {
		t.Fatalf("status = %d, roles = %v", code, roles.List())
	}

	// The operator points ACME_ADMIN_GROUP at a different group and restarts.
	g.Mapping[RoleAdmin] = "acme-admins-v2"
	g.Mapping[RoleReadOnly] = "acme-admins"

	code, _, roles := serve(t, g, ActionProjectAdmin)
	if code != http.StatusForbidden {
		t.Errorf("status = %d, want 403: the old role was cached", code)
	}
	if roles != nil {
		t.Errorf("the handler ran with roles %v", roles.List())
	}

	// The same session is still a reader under the new mapping.
	if code, _, roles := serve(t, g, ActionViewInventory); code != http.StatusOK ||
		!slices.Equal(roles.List(), []string{"READ_ONLY"}) {
		t.Errorf("status = %d, roles = %v, want 200 [READ_ONLY]", code, roles.List())
	}
}

func TestRequireWithoutAuthenticatedRefuses(t *testing.T) {
	// A route wired up with Require but not Authenticated must fail closed.
	e := echo.New()
	e.GET("/x", func(c echo.Context) error { return c.String(http.StatusOK, "ok") },
		Require(ActionViewInventory))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
