package authz

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// TestIntegrationSeededRolesPermitExactlyWhatTheyDidBefore is constraint 1 —
// "the internal edition must not observably change" — asserted rather than
// hoped for.
//
// The old model is the matrix in permissions.go: a RoleSet and an Action, no
// object. The new one is a set of grant rows evaluated against an object. This
// walks every seeded role against every Action and requires the two to agree,
// in both directions.
//
// Disagreement in either direction is a bug, and they are different bugs. A role
// that gained an action is an escalation nobody asked for; a role that lost one
// is an operator who could do their job on Friday and cannot on Monday.
func TestIntegrationSeededRolesPermitExactlyWhatTheyDidBefore(t *testing.T) {
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

	for _, roleName := range AllRoles {
		t.Run(string(roleName), func(t *testing.T) {
			rows, err := store.RolesByName(ctx, db, sc, string(roleName))
			if err != nil {
				t.Fatalf("read role: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("found %d roles named %q, want exactly one — the migration seeds all four", len(rows), roleName)
			}
			subject := SubjectFromRoles(account, rows)
			old := NewRoleSet(roleName)

			for _, action := range AllActions {
				want := CanRole(old, action)
				for _, o := range objects {
					got := Can(subject, string(action), o)
					if got == want {
						continue
					}
					verb := "gained"
					if want {
						verb = "lost"
					}
					t.Errorf("%s %s %q on a %s: matrix says %v, grants say %v",
						roleName, verb, action, o.Type, want, got)
				}
			}
		})
	}
}

// The seeded roles must also confer nothing in another account, which the
// matrix could not express at all — it had no notion of one.
func TestIntegrationSeededRolesConferNothingElsewhere(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	rows, err := store.RolesByName(ctx, db, sc, string(RoleAdmin))
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
