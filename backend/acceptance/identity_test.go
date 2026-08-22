//go:build acceptance

package acceptance

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
)

type meView struct {
	Subject     string   `json:"subject"`
	DisplayName string   `json:"displayName"`
	Groups      []string `json:"groups"`
	Roles       []string `json:"roles"`
}

// TestSignInCarriesTheSession is the regression pin for signin.go's cookie jar
// (task 1.3). If the jar ever goes back to net/http/cookiejar, the session
// cookie is held but never sent — because it is marked Secure and this origin
// is plain http — and /api/v1/me answers 401 with no cookie on the request.
// So both halves are asserted: the call succeeded, and it carried the cookie.
func TestSignInCarriesTheSession(t *testing.T) {
	s := mustSignIn(t, adminUser, devPassword)

	if !s.jar.has("acme-console.localhost", oidcauth.SessionCookieName) {
		t.Fatalf("no %s cookie held for the console origin", oidcauth.SessionCookieName)
	}
	var me meView
	if err := s.getJSON("/api/v1/me", &me); err != nil {
		t.Fatalf("GET /api/v1/me after signing in: %v", err)
	}
	// The realm gives admin-user a display name of its own ("Admin Dev"), so
	// the thing that identifies WHICH user signed in is the group the token
	// carries, not the label.
	if me.Subject == "" || me.DisplayName == "" || !slices.Contains(me.Groups, "acme-admin") {
		t.Fatalf("GET /api/v1/me: got subject %q, display name %q, groups %v — "+
			"expected the session of %s", me.Subject, me.DisplayName, me.Groups, adminUser)
	}
}

// Task 2.7: the four seeded users' groups and roles through the real Keycloak,
// and the enforcement those roles are for.
//
// The realm's group mapper is configured with full.path=false, so the claim
// carries "acme-admin" rather than "/acme-admin" — which is what
// ACME_ADMIN_GROUP and friends in development/.env are set to.
func TestSeededUsersResolveToTheirRoles(t *testing.T) {
	for _, tc := range []struct{ user, group, role string }{
		{adminUser, "acme-admin", "ADMIN"},
		{configAdminUser, "acme-config-admin", "CONFIG_ADMIN"},
		{keyRotationUser, "acme-key-rotation", "KEY_ROTATION"},
		{readOnlyUser, "acme-read-only", "READ_ONLY"},
	} {
		t.Run(tc.user, func(t *testing.T) {
			s := mustSignIn(t, tc.user, devPassword)
			var me meView
			if err := s.getJSON("/api/v1/me", &me); err != nil {
				t.Fatalf("GET /api/v1/me: %v", err)
			}
			if !slices.Contains(me.Groups, tc.group) {
				t.Errorf("groups %v do not include %q", me.Groups, tc.group)
			}
			if !slices.Contains(me.Roles, tc.role) {
				t.Errorf("roles %v do not include %q, which %q is configured to grant",
					me.Roles, tc.role, tc.group)
			}
		})
	}
}

// TestRoleEnforcementThroughTheManagementAPI pins the boundary the role table
// exists for.
//
// The key-rotation pair is the load-bearing half: read-only refused and
// config-admin allowed would BOTH still pass under a naive "any signed-in user
// may write" implementation with a read-only special case. Only a user who is
// refused one write and allowed a different one proves the permissions are per
// action rather than per "may write at all".
func TestRoleEnforcementThroughTheManagementAPI(t *testing.T) {
	admin := mustSignIn(t, adminUser, devPassword)
	_, lb := demoProd(t, admin)
	domains := fmt.Sprintf("/api/v1/loadbalancers/%d/domains", lb)

	// A name of its own, in a zone the plan comparison in plan_test.go does not
	// look at, and removed again either way.
	newDomain := func(name string) map[string]string { return map[string]string{"name": name} }

	readOnly := mustSignIn(t, readOnlyUser, devPassword)
	if status, body, err := readOnly.do(http.MethodPost, domains, newDomain("acceptance-ro.example.net")); err != nil {
		t.Fatalf("as %s: %v", readOnlyUser, err)
	} else if status != http.StatusForbidden {
		t.Errorf("%s creating a domain: got %d, want 403: %s", readOnlyUser, status, body)
	}

	configAdmin := mustSignIn(t, configAdminUser, devPassword)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := configAdmin.json(http.MethodPost, domains,
		newDomain("acceptance-ca.example.net"), http.StatusCreated, &created); err != nil {
		t.Errorf("%s creating a domain: %v", configAdminUser, err)
	} else {
		t.Cleanup(func() {
			_, _, _ = admin.do(http.MethodDelete, fmt.Sprintf("/api/v1/domains/%d", created.ID), nil)
		})
	}

	rotation := mustSignIn(t, keyRotationUser, devPassword)
	if status, body, err := rotation.do(http.MethodPost, domains, newDomain("acceptance-kr.example.net")); err != nil {
		t.Fatalf("as %s: %v", keyRotationUser, err)
	} else if status != http.StatusForbidden {
		t.Errorf("%s creating a domain: got %d, want 403: %s", keyRotationUser, status, body)
	}
	var key keyCreated
	if err := rotation.json(http.MethodPost, fmt.Sprintf("/api/v1/loadbalancers/%d/keys", lb),
		map[string]any{"note": "acceptance: key-rotation may mint"}, http.StatusCreated, &key); err != nil {
		t.Errorf("%s creating an API key: %v", keyRotationUser, err)
	} else {
		t.Cleanup(func() {
			_, _, _ = admin.do(http.MethodDelete, fmt.Sprintf("/api/v1/keys/%d", key.Key.ID), nil)
		})
	}
}
