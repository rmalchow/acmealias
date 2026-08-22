package authz

import (
	"slices"
	"testing"
)

// matrix is the table from the identity-and-roles spec, transcribed
// independently of permissions.go: this is the normative statement, and the
// implementation is checked against it rather than against itself.
var matrix = map[Action]map[Role]bool{
	//                        ADMIN  CONFIG_ADMIN  KEY_ROTATION  READ_ONLY
	ActionProjectAdmin:   {RoleAdmin: true, RoleConfigAdmin: false, RoleKeyRotation: false, RoleReadOnly: false},
	ActionSessionAdmin:   {RoleAdmin: true, RoleConfigAdmin: false, RoleKeyRotation: false, RoleReadOnly: false},
	ActionInventoryWrite: {RoleAdmin: true, RoleConfigAdmin: true, RoleKeyRotation: false, RoleReadOnly: false},
	ActionKeyWrite:       {RoleAdmin: true, RoleConfigAdmin: true, RoleKeyRotation: true, RoleReadOnly: false},
	ActionKeySecretView:  {RoleAdmin: true, RoleConfigAdmin: true, RoleKeyRotation: true, RoleReadOnly: false},
	ActionDNSPlan:        {RoleAdmin: true, RoleConfigAdmin: true, RoleKeyRotation: true, RoleReadOnly: true},
	ActionViewInventory:  {RoleAdmin: true, RoleConfigAdmin: true, RoleKeyRotation: true, RoleReadOnly: true},
}

func TestCanEveryRoleAgainstEveryAction(t *testing.T) {
	if len(matrix) != len(AllActions) {
		t.Fatalf("matrix covers %d actions, AllActions has %d — a new action needs a row here",
			len(matrix), len(AllActions))
	}

	for _, action := range AllActions {
		row, ok := matrix[action]
		if !ok {
			t.Fatalf("action %q has no row in the matrix", action)
		}
		for _, role := range AllRoles {
			want := row[role]
			if got := CanRole(NewRoleSet(role), action); got != want {
				t.Errorf("CanRole(%s, %s) = %v, want %v", role, action, got, want)
			}
		}
	}
}

func TestCanNoRolesPermitsNothing(t *testing.T) {
	for _, action := range AllActions {
		if CanRole(RoleSet{}, action) {
			t.Errorf("an actor with no roles was permitted %s", action)
		}
	}
}

func TestCanUnknownActionFailsClosed(t *testing.T) {
	// A typo in a route's required permission must refuse everyone, including
	// an administrator.
	if CanRole(NewRoleSet(RoleAdmin), Action("inventory.wriet")) {
		t.Error("an unknown action was permitted")
	}
}

func TestCanRolesAreAdditive(t *testing.T) {
	// KEY_ROTATION + CONFIG_ADMIN is the interesting union: it gains the
	// inventory writes that KEY_ROTATION alone does not have, and still does
	// not gain project administration.
	union := NewRoleSet(RoleKeyRotation, RoleConfigAdmin)
	if !CanRole(union, ActionInventoryWrite) {
		t.Error("KEY_ROTATION+CONFIG_ADMIN may not write inventory")
	}
	if !CanRole(union, ActionKeyWrite) {
		t.Error("KEY_ROTATION+CONFIG_ADMIN may not write keys")
	}
	if CanRole(union, ActionProjectAdmin) {
		t.Error("KEY_ROTATION+CONFIG_ADMIN reached an ADMIN-only action")
	}

	// The union is exactly the union of what each half permits.
	want := map[Action]bool{}
	for _, a := range Allowed(NewRoleSet(RoleKeyRotation)) {
		want[a] = true
	}
	for _, a := range Allowed(NewRoleSet(RoleConfigAdmin)) {
		want[a] = true
	}
	for _, a := range AllActions {
		if got := CanRole(union, a); got != want[a] {
			t.Errorf("CanRole(union, %s) = %v, want %v", a, got, want[a])
		}
	}
}

func TestCanBoundaryBetweenConfigAdminAndKeyRotation(t *testing.T) {
	// The one boundary that is easy to get wrong: a key yes, a domain no.
	keyRotation := NewRoleSet(RoleKeyRotation)
	if !CanRole(keyRotation, ActionKeyWrite) || !CanRole(keyRotation, ActionKeySecretView) {
		t.Error("KEY_ROTATION cannot create a key or see its secret")
	}
	if CanRole(keyRotation, ActionInventoryWrite) {
		t.Error("KEY_ROTATION can change the inventory")
	}
}

func TestCanOnlyAdminReachesAdminOnlyActions(t *testing.T) {
	for _, role := range []Role{RoleConfigAdmin, RoleKeyRotation, RoleReadOnly} {
		for _, action := range []Action{ActionProjectAdmin, ActionSessionAdmin} {
			if CanRole(NewRoleSet(role), action) {
				t.Errorf("%s reached ADMIN-only action %s", role, action)
			}
		}
	}
	// And an ADMIN reaches everything there is.
	if got := len(Allowed(NewRoleSet(RoleAdmin))); got != len(AllActions) {
		t.Errorf("ADMIN is permitted %d of %d actions", got, len(AllActions))
	}
}

func TestAllowedIsOrderedAndReadOnlyIsReadOnly(t *testing.T) {
	want := []Action{ActionDNSPlan, ActionViewInventory}
	if got := Allowed(NewRoleSet(RoleReadOnly)); !slices.Equal(got, want) {
		t.Errorf("Allowed(READ_ONLY) = %v, want %v", got, want)
	}
}
