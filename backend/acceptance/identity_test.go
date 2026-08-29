//go:build acceptance

package acceptance

import (
	"net/http"
	"slices"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
)

type meView struct {
	SelectedAccountID *string  `json:"selectedAccountId"`
	Subject           string   `json:"subject"`
	DisplayName       string   `json:"displayName"`
	Groups            []string `json:"groups"`
	Roles             []string `json:"roles"`
	Permissions       []string `json:"permissions"`
	// Monetized is the deployment's answer to "does a balance decide anything".
	// The funding assertions read it rather than assuming, so this suite says
	// something true against a deployment configured either way instead of
	// failing on the one it was not written for.
	Monetized bool `json:"monetized"`
}

func (s *session) me(t *testing.T) meView {
	t.Helper()
	var me meView
	if err := s.getJSON("/api/v1/me", &me); err != nil {
		t.Fatalf("GET /api/v1/me as %s: %v", s.user, err)
	}
	return me
}

// TestSignInCarriesTheSession is the regression pin for signin.go's cookie jar
// (task 1.3). If the jar ever goes back to net/http/cookiejar, the session
// cookie is held but never sent — because it is marked Secure and this origin
// is plain http — and /api/v1/me answers 401 with no cookie on the request.
// So both halves are asserted: the call succeeded, and it carried the cookie.
func TestSignInCarriesTheSession(t *testing.T) {
	s := mustSignIn(t, adminUser, devPassword)

	if !s.jar.has("console.localhost", oidcauth.SessionCookieName) {
		t.Fatalf("no %s cookie held for the console origin", oidcauth.SessionCookieName)
	}
	me := s.me(t)
	// The group claim still arrives and is still reported, and it still says
	// WHICH of the four indistinguishable-by-display-name users signed in. What
	// it no longer does is grant anything (design C10): the blueprint keeps the
	// groups because `acme-admin` gates the staff application, and because this
	// suite wants four people it can tell apart.
	if me.Subject == "" || me.DisplayName == "" || !slices.Contains(me.Groups, "acme-admin") {
		t.Fatalf("GET /api/v1/me: got subject %q, display name %q, groups %v — "+
			"expected the session of %s", me.Subject, me.DisplayName, me.Groups, adminUser)
	}
}

// TestANewSessionHasChosenNothingAndIsOfferedSomewhere is P19's front door
// (tasks 7.5, 7.6).
//
// It used to be unreachable. Sign-in selected the deployment's one account, so
// "no account selected" was a state the console rendered for and nobody ever
// saw; design C10 removed the selection, and this is now the first thing every
// user in every deployment meets. Two halves, and they say different things:
//
//   - NOTHING is selected, and no role or permission is held. Authority is a
//     property of an account, so outside one there is none — where before a
//     signed-in user carried four global roles wherever they went.
//   - the picker is NOT EMPTY. A user always has their personal account, which
//     nobody creates and nobody can lose (design C1). That is what makes "no
//     account selected" mean "you have not said which" rather than "you have
//     nothing" — the distinction P19 refused to blur, now load-bearing.
func TestANewSessionHasChosenNothingAndIsOfferedSomewhere(t *testing.T) {
	for user := range seededRoles {
		t.Run(user, func(t *testing.T) {
			s := mustSignIn(t, user, devPassword)
			me := s.me(t)
			if me.SelectedAccountID != nil {
				t.Errorf("a fresh session is acting in account %s; sign-in must select nothing",
					*me.SelectedAccountID)
			}
			if len(me.Roles) != 0 || len(me.Permissions) != 0 {
				t.Errorf("with no account selected %s holds roles %v and permissions %v; "+
					"both are per-account and must be empty here", user, me.Roles, me.Permissions)
			}

			accounts := s.accounts(t)
			if len(accounts) == 0 {
				t.Fatalf("the picker offered %s nothing at all — a user always has their "+
					"personal account, so an empty picker is a materialization that did not "+
					"happen (design C1, task 3.1), not a user with no accounts", user)
			}
			personal := s.personalAccount(t)
			if !personal.Owner {
				t.Errorf("%s does not own their own personal account", user)
			}
			if personal.Credits != 0 || personal.Suspended {
				t.Errorf("personal account %s: credits %d, suspended %v — a personal account "+
					"is free and always usable, and is outside the funded/suspended machine entirely (design C3)",
					personal.Slug, personal.Credits, personal.Suspended)
			}
		})
	}
}

// TestRolesComeFromMembershipRatherThanFromAGroup replaces
// TestSeededUsersResolveToTheirRoles (task 7.2).
//
// WHAT THE OLD TEST ASSERTED, AND WHY IT COULD NOT BE REPAIRED. It walked a
// table of {user, group, role} and checked that /me reported the role the group
// was CONFIGURED to grant — ACME_ADMIN_GROUP and its three siblings, read at
// startup into authz.Mapping. Design C10 deleted the mapping, the settings and
// the global roles the mapping produced; there is nothing left for that
// assertion to be about.
//
// What survives is the four names. They are the default role set every new team
// account is seeded with (task 2.5), so the same four words mean the same four
// things — but as rows in one account, held because of a membership rather than
// because of a claim. This walks the same table and asserts the new mechanism:
// the group is still on the token and grants nothing; the role appears only in
// the account the membership is in, and only once that account is selected.
func TestRolesComeFromMembershipRatherThanFromAGroup(t *testing.T) {
	for _, tc := range []struct{ user, group, role string }{
		{adminUser, "acme-admin", "ADMIN"},
		{configAdminUser, "acme-config-admin", "CONFIG_ADMIN"},
		{keyRotationUser, "acme-key-rotation", "KEY_ROTATION"},
		{readOnlyUser, "acme-read-only", "READ_ONLY"},
	} {
		t.Run(tc.user, func(t *testing.T) {
			s := mustSignIn(t, tc.user, devPassword)
			if before := s.me(t); slices.Contains(before.Roles, tc.role) {
				t.Fatalf("%s already holds %q with no account selected; a role is held "+
					"IN an account and nowhere else (design C10)", tc.user, tc.role)
			}

			// Their own personal account grants them everything in it and none
			// of the four names: a personal account holds one unrestricted role
			// and there is nobody to give a second one to (design C9).
			s.selectInto(t, s.personalAccount(t))
			if own := s.me(t); slices.Contains(own.Roles, tc.role) {
				t.Errorf("%s holds %q in their PERSONAL account (roles %v); the four default "+
					"roles are seeded into team accounts, not into personal ones",
					tc.user, tc.role, own.Roles)
			}

			joinAccount(t, seededAccountSlug, s.subject, tc.role)
			s.selectInto(t, s.accountBySlug(t, seededAccountSlug))
			me := s.me(t)
			if !slices.Contains(me.Groups, tc.group) {
				t.Errorf("groups %v do not include %q — the blueprint still puts this user "+
					"in it, even though it now grants nothing", me.Groups, tc.group)
			}
			if !slices.Contains(me.Roles, tc.role) {
				t.Errorf("roles %v in %s do not include %q, which the membership row confers",
					me.Roles, seededAccountSlug, tc.role)
			}
			if len(me.Permissions) == 0 {
				t.Errorf("%s holds role %q in %s and no permissions at all; the role's grants "+
					"are what /me answers with now (task 2.5)", tc.user, tc.role, seededAccountSlug)
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
//
// The four roles are the same four and the boundary between them is the same
// boundary — what changed is where the authority comes from. Every session below
// holds its role because of an account_members row in the seeded account (task
// 7.1), so this now also proves the seeded default roles carry the grants the
// old configured matrix did.
func TestRoleEnforcementThroughTheManagementAPI(t *testing.T) {
	admin := mustSignInSeeded(t, adminUser)
	project := demoProject(t, admin)
	// Every session below is a member of the SAME account and has selected it,
	// so one address serves all four. That is now a property of the fixture
	// rather than of the edition: a session acting in another account would be
	// refused this path outright (design P19).
	domains := admin.acct("/projects/" + project + "/domains")

	// A name of its own, in a zone the plan comparison in plan_test.go does not
	// look at, and removed again either way.
	newDomain := func(name string) map[string]string { return map[string]string{"name": name} }

	readOnly := mustSignInSeeded(t, readOnlyUser)
	if status, body, err := readOnly.do(http.MethodPost, domains, newDomain("acceptance-ro.example.net")); err != nil {
		t.Fatalf("as %s: %v", readOnlyUser, err)
	} else if status != http.StatusForbidden {
		t.Errorf("%s creating a domain: got %d, want 403: %s", readOnlyUser, status, body)
	}

	configAdmin := mustSignInSeeded(t, configAdminUser)
	var created struct {
		ID string `json:"id"`
	}
	if err := configAdmin.json(http.MethodPost, domains,
		newDomain("acceptance-ca.example.net"), http.StatusCreated, &created); err != nil {
		t.Errorf("%s creating a domain: %v", configAdminUser, err)
	} else {
		t.Cleanup(func() {
			_, _, _ = admin.do(http.MethodDelete, admin.acct("/domains/"+created.ID), nil)
		})
	}

	rotation := mustSignInSeeded(t, keyRotationUser)
	if status, body, err := rotation.do(http.MethodPost, domains, newDomain("acceptance-kr.example.net")); err != nil {
		t.Fatalf("as %s: %v", keyRotationUser, err)
	} else if status != http.StatusForbidden {
		t.Errorf("%s creating a domain: got %d, want 403: %s", keyRotationUser, status, body)
	}
	var key keyCreated
	if err := rotation.json(http.MethodPost, rotation.acct("/projects/"+project+"/keys"),
		map[string]any{"note": "acceptance: key-rotation may mint"}, http.StatusCreated, &key); err != nil {
		t.Errorf("%s creating an API key: %v", keyRotationUser, err)
	} else {
		t.Cleanup(func() {
			_, _, _ = admin.do(http.MethodDelete, admin.acct("/keys/"+key.Key.ID), nil)
		})
	}
}
