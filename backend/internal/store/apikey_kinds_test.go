package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The lifetime rules the two kinds exist to make true (design K2, K6, task 1.7).
//
// These are database facts and are asserted as database facts, because that is
// where they are kept: a foreign key that cascades and a foreign key that
// restricts. An application-level check would be a rule the CLI, a migration or
// somebody's psql session could walk past — which is exactly what happened to
// the polymorphic grants, and is why nothing deletes one when its object goes.

// A project key dies with its project, which is the thing a grant could never
// do: api_key_grants.object_id names one of nine tables depending on the column
// beside it, so it declares no reference and nothing removes it.
func TestIntegrationDeletingAProjectDeletesItsKeys(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "doomed", Name: "Doomed"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	other := &model.Project{Slug: "survivor", Name: "Survivor"}
	if err := CreateProject(ctx, db, sc, other); err != nil {
		t.Fatalf("create project: %v", err)
	}

	doomed := &model.APIKey{
		Kind: model.APIKeyKindProject, ProjectID: &p.ID,
		Identifier: "k_doomed01", SecretHash: []byte("digest"), DisplayPrefix: "doomed",
	}
	if err := CreateAPIKey(ctx, db, sc, doomed, AllowAnyGrant); err != nil {
		t.Fatalf("create key: %v", err)
	}
	survivor := &model.APIKey{
		Kind: model.APIKeyKindProject, ProjectID: &other.ID,
		Identifier: "k_survive1", SecretHash: []byte("digest"), DisplayPrefix: "surviv",
	}
	if err := CreateAPIKey(ctx, db, sc, survivor, AllowAnyGrant); err != nil {
		t.Fatalf("create key: %v", err)
	}

	if err := DeleteProject(ctx, db, sc, p.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	// Gone from the lookup that authenticates requests, which is the property
	// that actually matters: a key that still authenticates is a live
	// credential whatever any listing says.
	if _, err := FindAPIKeyByIdentifier(ctx, db, doomed.Identifier); err == nil {
		t.Error("the deleted project's key still authenticates; it was supposed to go with the project")
	}
	if _, err := FindAPIKeyByIdentifier(ctx, db, survivor.Identifier); err != nil {
		t.Errorf("another project's key was taken too: %v", err)
	}
}

// A role a system key holds cannot be deleted from under it (design K6, task
// 1.3), matching account-invitations V6 — and the refusal names the keys,
// because "delete them or give them another role" is only actionable if you know
// which.
//
// The two alternatives both lose something silently: cascading strips an
// automation of its authority, and setting null leaves a credential whose
// authority is a mystery.
func TestIntegrationARoleHeldByASystemKeyCannotBeDeleted(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	role := &model.Role{Name: "ci", Description: "what the pipeline may do"}
	if err := CreateRole(ctx, db, sc, role, AllowAnyGrant); err != nil {
		t.Fatalf("create role: %v", err)
	}
	k := &model.APIKey{
		Kind: model.APIKeyKindSystem, RoleID: &role.ID,
		Identifier: "k_ci000001", SecretHash: []byte("digest"), DisplayPrefix: "ci0000",
	}
	if err := CreateAPIKey(ctx, db, sc, k, AllowAnyGrant); err != nil {
		t.Fatalf("create key: %v", err)
	}

	err := DeleteRole(ctx, db, sc, role.ID)
	if !errors.Is(err, ErrRoleHeldBySystemKey) {
		t.Fatalf("delete role: %v, want ErrRoleHeldBySystemKey", err)
	}
	if !strings.Contains(err.Error(), k.Identifier) {
		t.Errorf("the refusal does not name the key holding the role: %v", err)
	}

	// And REVOKING the key does not release the role, which is worth pinning
	// because it is the tempting shortcut. Revocation is not deletion: the row
	// survives so that the key still identifies past audit entries and challenge
	// attributions, and a role deleted out from under a surviving row would
	// leave those entries pointing at nothing. Deleting the key is what releases
	// the role.
	if err := RevokeAPIKey(ctx, db, sc, k.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	if err := DeleteRole(ctx, db, sc, role.ID); !errors.Is(err, ErrRoleHeldBySystemKey) {
		t.Error("a REVOKED key stopped defending its role; revocation is not deletion, and the row " +
			"still names the role in every audit entry it appears in")
	}
}

// A row cannot be both kinds or neither of them (task 1.4).
//
// The system branch deliberately permits a null role, and that is the one place
// this constraint is looser than the task's wording. Every key that existed
// before the two kinds is a system key holding its own grants, and requiring a
// role would mean inventing one for each of them at migration time — the
// conversion task 1.5 forbids. The null is a legacy shape, not a shape anybody
// can create: the API refuses a system key with no role.
func TestIntegrationAKeyIsExactlyOneKind(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "shapes", Name: "Shapes"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	role := &model.Role{Name: "ops"}
	if err := CreateRole(ctx, db, sc, role, AllowAnyGrant); err != nil {
		t.Fatalf("create role: %v", err)
	}

	cases := []struct {
		name string
		key  *model.APIKey
	}{
		{"a project key with no project", &model.APIKey{Kind: model.APIKeyKindProject}},
		{"a project key holding a role", &model.APIKey{
			Kind: model.APIKeyKindProject, ProjectID: &p.ID, RoleID: &role.ID,
		}},
		{"a system key bound to a project", &model.APIKey{
			Kind: model.APIKeyKindSystem, ProjectID: &p.ID,
		}},
		{"a kind that is neither", &model.APIKey{Kind: "admin", ProjectID: &p.ID}},
		{"the node limit without a project to compute it from", &model.APIKey{
			Kind: model.APIKeyKindSystem, RoleID: &role.ID, RestrictToProjectNodes: true,
		}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := *c.key
			k.Identifier = "k_shape" + string(rune('a'+i)) + "01"
			k.SecretHash = []byte("digest")
			k.DisplayPrefix = "shapes"
			if err := CreateAPIKey(ctx, db, sc, &k, AllowAnyGrant); err == nil {
				t.Fatal("the database accepted a key that is neither one kind nor the other")
			}
		})
	}
}

// The address union is read AT THIS MOMENT, from lb_nodes (design K3).
//
// This is the whole feature: there is nothing stored to go stale, so adding a
// node changes what a key permits with no write to the key at all. The test
// therefore asserts the three transitions rather than one answer.
func TestIntegrationNodeAddressesFollowTheProject(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	p := &model.Project{Slug: "fleet", Name: "Fleet"}
	if err := CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// A project with no nodes has an empty union, and that is NOT the same
	// emptiness as an empty CIDR list: there, no entries means the customer
	// never asked for a limit; here, the switch is the customer asking.
	addrs, err := NodeAddressesForProject(ctx, db, sc.AccountID(), p.ID)
	if err != nil {
		t.Fatalf("addresses: %v", err)
	}
	if len(addrs) != 0 {
		t.Fatalf("a project with no nodes offered %v", addrs)
	}

	v4, v6 := "203.0.113.7", "2001:db8::7"
	first := &model.LBNode{ProjectID: p.ID, Label: "a", IPv4: &v4, IPv6: &v6}
	if err := CreateLBNode(ctx, db, sc, first); err != nil {
		t.Fatalf("create node: %v", err)
	}
	addrs, err = NodeAddressesForProject(ctx, db, sc.AccountID(), p.ID)
	if err != nil {
		t.Fatalf("addresses: %v", err)
	}
	if len(addrs) != 2 {
		t.Fatalf("after adding a node the union is %v, want both families", addrs)
	}

	if err := DeleteLBNode(ctx, db, sc, first.ID); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	addrs, err = NodeAddressesForProject(ctx, db, sc.AccountID(), p.ID)
	if err != nil {
		t.Fatalf("addresses: %v", err)
	}
	if len(addrs) != 0 {
		t.Fatalf("after removing the node the union is %v, want nothing — a copied list would still hold it", addrs)
	}
}
