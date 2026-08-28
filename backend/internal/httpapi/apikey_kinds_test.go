package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The two kinds, through the API that mints them.

// impliedGrants and authz.SubjectFromKey compute the same authority.
//
// They have to, and nothing but this test makes them: one decides what the
// SUBSET RULE checks at creation, the other decides what the key can actually
// do, and the two live in different packages because store must not import the
// evaluator and the evaluator must not import the handler. If they ever
// disagreed, the disagreement would be silent in the direction that matters —
// a key conferring more than its creator was checked for.
func TestImpliedGrantsMatchWhatTheKeyResolvesTo(t *testing.T) {
	projectID := model.NewID()
	accountID := model.NewID()
	k := &model.APIKey{
		AccountID: accountID, Kind: model.APIKeyKindProject, ProjectID: &projectID,
	}

	checked := impliedGrants(k)
	held := authz.SubjectFromKey(k).Grants
	if len(checked) != len(held) {
		t.Fatalf("the subset rule checks %d grant(s) and the key resolves to %d", len(checked), len(held))
	}
	for i, c := range checked {
		h := held[i]
		if c.ObjectType != string(h.Type) || c.Action != h.Action || c.Inheritable != h.Inheritable {
			t.Errorf("grant %d: checked %+v, resolves to %+v", i, c, h)
		}
		if c.ObjectID == nil || h.ObjectID == nil || *c.ObjectID != *h.ObjectID {
			t.Errorf("grant %d names different objects: %v and %v", i, c.ObjectID, h.ObjectID)
		}
	}

	// A system key confers nothing that is not already a role's, and its role
	// was subset-checked when the role was written. So there is nothing implied
	// to check, and returning something here would double-check the wrong thing.
	roleID := model.NewID()
	system := &model.APIKey{AccountID: accountID, Kind: model.APIKeyKindSystem, RoleID: &roleID}
	if g := impliedGrants(system); len(g) != 0 {
		t.Errorf("a system key implies %d grant(s); its authority is its role's", len(g))
	}
}

// A system key is made in the account's settings, holds a role, and keeps that
// role through a rotation (design K6, K8, tasks 5.1 and 5.2).
func TestIntegrationSystemKeyHoldsARole(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	rec := cl.do(http.MethodPost, acct+"/keys", `{"note":"ci","roleId":"`+v.roleID.String()+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create system key: %d %s", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if created.Key.Kind != model.APIKeyKindSystem {
		t.Fatalf("kind %q, want %q", created.Key.Kind, model.APIKeyKindSystem)
	}
	if created.Key.RoleID == nil || *created.Key.RoleID != v.roleID {
		t.Fatalf("the key names role %v, want %v", created.Key.RoleID, v.roleID)
	}
	if created.Key.RoleName == "" {
		t.Error("the key's role has no name in the view; a console would render a blank where the " +
			"interesting word belongs")
	}
	if len(created.Key.Grants) != 0 {
		t.Errorf("a system key holding a role wrote %d grant row(s); it IS the role, and a copy of "+
			"its grants would stop following it the first time somebody edits the role", len(created.Key.Grants))
	}

	// A role is required. A key that authenticates and holds nothing is a
	// coherent row and a useless credential, and it is not what anybody meant.
	if rec := cl.do(http.MethodPost, acct+"/keys", `{"note":"nothing"}`); rec.Code == http.StatusCreated {
		t.Error("a system key was created with no role")
	}
	// And it must be a role of THIS account. Naming another account's role is a
	// not-found rather than a key holding somebody else's authority.
	if rec := cl.do(http.MethodPost, acct+"/keys",
		`{"note":"theirs","roleId":"`+model.NewID().String()+`"}`); rec.Code == http.StatusCreated {
		t.Error("a system key was created holding a role that is not this account's")
	}

	// It is listed in the account's settings and NOT on a project's page — the
	// two kinds do not share a screen, because their lifetimes differ and one
	// screen makes that invisible (design K8, task 5.4).
	rec = cl.do(http.MethodGet, acct+"/keys", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list system keys: %d %s", rec.Code, rec.Body.String())
	}
	var listed []keyView
	decode(t, rec, &listed)
	if len(listed) != 1 || listed[0].ID != created.Key.ID {
		t.Fatalf("the account's system keys are %+v, want the one just made", listed)
	}
	rec = cl.do(http.MethodGet, acct+"/projects/"+itoa(v.projID)+"/keys", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list project keys: %d %s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &listed)
	for _, k := range listed {
		if k.ID == created.Key.ID {
			t.Fatal("a system key appears on a project's page; deleting that project would then look " +
				"like it should revoke it, and it does not")
		}
	}

	// Rotation replaces the secret and nothing else. A rotation that quietly
	// changed a key's authority would be the worst kind of change, because
	// nobody would look.
	rec = cl.do(http.MethodPost, acct+"/keys/"+itoa(created.Key.ID)+"/rotate", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rotated keyCreated
	decode(t, rec, &rotated)
	if rotated.Key.Kind != model.APIKeyKindSystem || rotated.Key.RoleID == nil || *rotated.Key.RoleID != v.roleID {
		t.Fatalf("rotation changed the key's authority: %+v", rotated.Key)
	}
}

// A project key made through the API is bound to its project and carries the
// switch it was asked for (design K1, K3, tasks 3.1 and 4.1).
func TestIntegrationProjectKeyCarriesTheSwitchAndNotACopy(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()
	proj := acct + "/projects/" + itoa(v.projID)

	rec := cl.do(http.MethodPost, proj+"/keys", `{"note":"traefik","restrictToNodes":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project key: %d %s", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if created.Key.Kind != model.APIKeyKindProject {
		t.Fatalf("kind %q, want %q", created.Key.Kind, model.APIKeyKindProject)
	}
	if created.Key.ProjectID == nil || *created.Key.ProjectID != v.projID {
		t.Fatalf("the key names project %v, want %v", created.Key.ProjectID, v.projID)
	}
	if !created.Key.RestrictToNodes {
		t.Fatal("the address switch was asked for and is off")
	}
	// The switch is not a list. Nothing was copied into the CIDR restrictions,
	// which is the entire difference between this and what the console used to
	// do — that copy was correct until somebody added a node.
	if len(created.Key.Networks) != 0 {
		t.Fatalf("the switch wrote %v into the address restrictions; a copied list is what this "+
			"replaces, and it goes stale the moment a node is added", created.Key.Networks)
	}

	// Rotation keeps the switch, like every other part of a key's authority.
	rec = cl.do(http.MethodPost, acct+"/keys/"+itoa(created.Key.ID)+"/rotate", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rotated keyCreated
	decode(t, rec, &rotated)
	if !rotated.Key.RestrictToNodes || rotated.Key.ProjectID == nil || *rotated.Key.ProjectID != v.projID {
		t.Fatalf("rotation changed what the key is bound to or limited by: %+v", rotated.Key)
	}
}

// Deleting a role a system key holds is refused THROUGH THE API, with a message
// that names the key (design K6, task 1.7).
//
// The store test pins the rule; this pins that it reaches a caller as an
// explanation rather than as a foreign-key violation with a 500.
func TestIntegrationDeletingARoleHeldByASystemKeyIsRefused(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	rec := cl.do(http.MethodPost, acct+"/roles", `{"name":"pipeline","grants":[]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create role: %d %s", rec.Code, rec.Body.String())
	}
	var role roleView
	decode(t, rec, &role)

	rec = cl.do(http.MethodPost, acct+"/keys", `{"note":"ci","roleId":"`+role.ID.String()+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create system key: %d %s", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)

	rec = cl.do(http.MethodDelete, acct+"/roles/"+itoa(role.ID), "")
	if rec.Code == http.StatusNoContent || rec.Code == http.StatusOK {
		t.Fatal("the role was deleted out from under a key holding it")
	}
	if rec.Code >= 500 {
		t.Fatalf("the refusal arrived as a %d; a constraint violation is not an explanation", rec.Code)
	}
	var body ErrorBody
	decode(t, rec, &body)
	if !strings.Contains(body.Message, created.Key.Identifier) {
		t.Errorf("the refusal %q does not name the key holding the role, so there is nothing to act on",
			body.Message)
	}
}
