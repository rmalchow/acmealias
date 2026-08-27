package authz

import (
	"context"
	"slices"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The permission matrix that used to live in permissions.go, kept here as data
// after design C10 deleted the code.
//
// This file used to assert constraint 1 — "the internal edition must not
// observably change" — by walking every seeded role against every Action and
// requiring the grant rows and the matrix to agree. The internal edition is gone
// and the constraint is DISCHARGED rather than violated: what it bought is
// exactly the grant set store.DefaultRoles now seeds into every new team
// account, and this is the reference that set is "the same" as.
//
// So the matrix is transcribed rather than imported, and that is the point. The
// question it answers is no longer "do two implementations agree" — there is one
// implementation — but "does a role called CONFIG_ADMIN still confer what a role
// called CONFIG_ADMIN conferred in 2024". Nobody can answer that from a table
// that moves when the code does.
//
// The two directions are different bugs. A role that gained an action is an
// escalation nobody asked for; a role that lost one is an operator who could do
// their job on Friday and cannot on Monday.
var theOldMatrix = map[Action][]string{
	ActionProjectAdmin:   {"ADMIN"},
	ActionInventoryWrite: {"ADMIN", "CONFIG_ADMIN"},
	ActionKeyWrite:       {"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"},
	ActionKeySecretView:  {"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"},
	ActionDNSPlan:        {"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"},
	ActionViewInventory:  {"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"},
}

// theOldSeededRoles is the order the matrix's columns were in.
var theOldSeededRoles = []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}

// TestIntegrationDefaultRolesAreTheOldMatrix is what constraint 1 bought,
// asserted rather than remembered.
//
// The roles come out of a freshly created account rather than out of the
// migration seed, because that is where they come from now (store.DefaultRoles,
// task 2.5): the same four names, the same authority, rows in one account
// instead of a global vocabulary.
func TestIntegrationDefaultRolesAreTheOldMatrix(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	// One object of each type, so the comparison is made against something
	// real. The old model took no object, so every object must give the same
	// answer — which is itself part of what is being asserted: seeding must not
	// have narrowed a role to some objects and not others.
	account := sc.AccountID()
	project, lb, node := model.NewID(), model.NewID(), model.NewID()
	objects := []Object{
		{Type: TypeProject, ID: project, AccountID: account, ProjectID: project},
		{Type: TypeLoadBalancer, ID: lb, AccountID: account, ProjectID: project, LoadBalancerID: lb},
		{Type: TypeNode, ID: node, AccountID: account, ProjectID: project, LoadBalancerID: lb},
		{Type: TypeAPIKey, ID: model.NewID(), AccountID: account, ProjectID: project, LoadBalancerID: lb},
		{Type: TypeDomain, ID: model.NewID(), AccountID: account, ProjectID: project, LoadBalancerID: lb},
		{Type: TypeAlias, ID: model.NewID(), AccountID: account, ProjectID: project, LoadBalancerID: lb},
	}

	for _, roleName := range theOldSeededRoles {
		t.Run(roleName, func(t *testing.T) {
			rows, err := store.RolesByName(ctx, db, sc, roleName)
			if err != nil {
				t.Fatalf("read role: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("found %d roles named %q, want exactly one — a new account is seeded with all four", len(rows), roleName)
			}
			subject := SubjectFromRoles(account, rows)

			for _, action := range AllActions {
				want := slices.Contains(theOldMatrix[action], roleName)
				for _, o := range objects {
					got := Can(subject, string(action), o)
					// Project administration was ADMIN-only and is now governed
					// by the unrestricted flag rather than by a grant (design
					// P14), which is how ADMIN still holds it and how nobody
					// else can be given it by editing a grant.
					if action == ActionProjectAdmin {
						got = subject.Unrestricted
					}
					if got == want {
						continue
					}
					verb := "gained"
					if want {
						verb = "lost"
					}
					t.Errorf("%s %s %q on a %s: the old matrix says %v, the seeded grants say %v",
						roleName, verb, action, o.Type, want, got)
				}
			}
		})
	}
}

// The boundary between CONFIG_ADMIN and KEY_ROTATION is the one line of the
// matrix that is easy to get wrong when it is retyped: they differ by exactly
// one action, and a seeding mistake that gave KEY_ROTATION inventory.write would
// be invisible in a test that only checked totals.
func TestIntegrationConfigAdminAndKeyRotationDifferByOneAction(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	subjects := map[string]Subject{}
	for _, name := range []string{"CONFIG_ADMIN", "KEY_ROTATION"} {
		rows, err := store.RolesByName(ctx, db, sc, name)
		if err != nil || len(rows) != 1 {
			t.Fatalf("read %s: %v (%d rows)", name, err, len(rows))
		}
		subjects[name] = SubjectFromRoles(sc.AccountID(), rows)
	}

	lb := model.NewID()
	o := Object{Type: TypeLoadBalancer, ID: lb, AccountID: sc.AccountID(), ProjectID: model.NewID(), LoadBalancerID: lb}
	if !Can(subjects["CONFIG_ADMIN"], string(ActionInventoryWrite), o) {
		t.Error("CONFIG_ADMIN cannot write inventory")
	}
	if Can(subjects["KEY_ROTATION"], string(ActionInventoryWrite), o) {
		t.Error("KEY_ROTATION may write inventory; it never could")
	}
	for _, a := range []Action{ActionKeyWrite, ActionKeySecretView, ActionDNSPlan, ActionViewInventory} {
		if !Can(subjects["KEY_ROTATION"], string(a), o) {
			t.Errorf("KEY_ROTATION lost %q", a)
		}
	}
}

// A role that may mint an API key must hold what the key is minted with, or the
// subset rule (P10) lets it create only useless ones. That omission shipped
// once — migration 000012 is the repair — so it is asserted about the successor
// seed rather than trusted to the transcription.
func TestIntegrationKeyMintingRolesHoldWhatAKeyIsMintedWith(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	lb := model.NewID()
	o := Object{Type: TypeLoadBalancer, ID: lb, AccountID: sc.AccountID(), ProjectID: model.NewID(), LoadBalancerID: lb}
	for _, name := range []string{"CONFIG_ADMIN", "KEY_ROTATION"} {
		rows, err := store.RolesByName(ctx, db, sc, name)
		if err != nil || len(rows) != 1 {
			t.Fatalf("read %s: %v (%d rows)", name, err, len(rows))
		}
		subject := SubjectFromRoles(sc.AccountID(), rows)
		for _, a := range []string{"challenge.publish", "challenge.withdraw"} {
			if !Can(subject, a, o) {
				t.Errorf("%s cannot %s, so every key it mints is useless", name, a)
			}
		}
	}
}

// The seeded roles must also confer nothing in another account, which the
// matrix could not express at all — it had no notion of one.
func TestIntegrationDefaultRolesConferNothingElsewhere(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	rows, err := store.RolesByName(ctx, db, sc, "ADMIN")
	if err != nil || len(rows) != 1 {
		t.Fatalf("read ADMIN: %v (%d rows)", err, len(rows))
	}
	subject := SubjectFromRoles(sc.AccountID(), rows)

	elsewhere := Object{Type: TypeLoadBalancer, ID: model.NewID(), AccountID: model.NewID()}
	for _, action := range AllActions {
		if Can(subject, string(action), elsewhere) {
			t.Fatalf("ADMIN permitted %q on an object of another account", action)
		}
	}
}
