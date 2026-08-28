package authz

import (
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Both kinds of key become grants here and nowhere else (design K1, task 2.2).
//
// The property under test is not "a project key works". It is that a project key
// works THROUGH THE ORDINARY PREDICATE — the same Can() a person goes through —
// so that there is exactly one place where authority is decided. If either kind
// ever needed a check of its own, these tests would still pass and the design
// would still be broken, which is why each case below asserts the refusals as
// well as the permissions: a special case would show up as a refusal that did
// not happen.

func ids() (account, project, other model.ID) {
	return model.NewID(), model.NewID(), model.NewID()
}

// A project key reaches its project's challenges, and nothing else — including
// nothing else ON that project.
func TestProjectKeyReachesItsProjectAndNoOther(t *testing.T) {
	account, project, other := ids()
	k := &model.APIKey{
		AccountID: account, Kind: model.APIKeyKindProject, ProjectID: &project,
	}
	s := SubjectFromKey(k)

	mine := Object{Type: TypeAlias, ID: model.NewID(), AccountID: account, ProjectID: project}
	theirs := Object{Type: TypeAlias, ID: model.NewID(), AccountID: account, ProjectID: other}

	for _, action := range []string{ActionChallengePublish, ActionChallengeWithdraw} {
		if !Can(s, action, mine) {
			t.Errorf("a project key cannot %s on an alias of its own project", action)
		}
		if Can(s, action, theirs) {
			t.Errorf("a project key can %s on ANOTHER project's alias — and it was refused by nothing, "+
				"because there is nothing but the predicate to refuse it", action)
		}
	}

	// Its authority is challenge answering. Reading the inventory, writing it,
	// and touching keys are all somebody else's job: the point of the kind is
	// that the common case stops being assembled by hand, not that it becomes
	// wider than what was assembled.
	for _, action := range []string{string(ActionViewInventory), string(ActionInventoryWrite), string(ActionKeyWrite)} {
		if Can(s, action, mine) {
			t.Errorf("a project key can %s on its own project; its authority is that project's "+
				"challenge answering and nothing else", action)
		}
	}
}

// A system key is its role, exactly — including the unrestricted flag, which is
// a property of the role row rather than of a set of grants and therefore cannot
// be arrived at by accumulating grants.
func TestSystemKeyIsItsRole(t *testing.T) {
	account, project, _ := ids()
	role := model.Role{
		ID: model.NewID(), AccountID: account, Name: "publisher",
		Grants: []model.RoleGrant{{
			ID: model.NewID(), AccountID: account, ObjectType: string(TypeProject),
			ObjectID: &project, Action: ActionChallengePublish, Inheritable: true,
		}},
	}
	k := &model.APIKey{
		AccountID: account, Kind: model.APIKeyKindSystem, RoleID: &role.ID, Role: &role,
	}
	s := SubjectFromKey(k)

	alias := Object{Type: TypeAlias, ID: model.NewID(), AccountID: account, ProjectID: project}
	if !Can(s, ActionChallengePublish, alias) {
		t.Error("a system key does not hold what its role holds")
	}
	if Can(s, ActionChallengeWithdraw, alias) {
		t.Error("a system key holds an action its role does not; it is supposed to BE the role")
	}

	role.Unrestricted = true
	if !SubjectFromKey(k).Unrestricted {
		t.Error("a system key holding the unrestricted role is not unrestricted")
	}
}

// Every shape that cannot be resolved authorizes NOTHING rather than falling
// back to something (design K6's "a key whose role somehow vanished authorises
// nothing").
//
// The database forbids all of these, which is precisely why they are worth
// asserting: they are what a bug upstream looks like, and the only safe reading
// of an answer that should have been impossible is an empty one.
func TestAKeyThatCannotBeResolvedHoldsNothing(t *testing.T) {
	account, project, other := ids()
	roleID := model.NewID()

	cases := []struct {
		name string
		key  *model.APIKey
	}{
		{"a project key naming no project", &model.APIKey{
			AccountID: account, Kind: model.APIKeyKindProject,
		}},
		{"a system key whose role did not load", &model.APIKey{
			AccountID: account, Kind: model.APIKeyKindSystem, RoleID: &roleID,
		}},
		{"a system key holding a role of another account", &model.APIKey{
			AccountID: account, Kind: model.APIKeyKindSystem, RoleID: &roleID,
			Role: &model.Role{
				ID: roleID, AccountID: other, Unrestricted: true,
			},
		}},
		{"a system key whose loaded role is a different role", &model.APIKey{
			AccountID: account, Kind: model.APIKeyKindSystem, RoleID: &roleID,
			Role: &model.Role{ID: model.NewID(), AccountID: account, Unrestricted: true},
		}},
		{"no key at all", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := SubjectFromKey(c.key)
			if s.Unrestricted {
				t.Fatal("resolved to an UNRESTRICTED subject, which is the worst possible fallback")
			}
			if len(s.Grants) != 0 {
				t.Fatalf("resolved to %d grant(s), want none", len(s.Grants))
			}
			obj := Object{Type: TypeAlias, ID: model.NewID(), AccountID: account, ProjectID: project}
			if Can(s, ActionChallengePublish, obj) {
				t.Error("it can still publish")
			}
		})
	}
}

// A key from before there were two kinds keeps working, holding exactly the
// grants it holds. Their conversion into roles is a decision nobody asked for,
// so nothing here converts them.
func TestALegacyKeyStillHoldsItsOwnGrants(t *testing.T) {
	account, project, _ := ids()
	k := &model.APIKey{
		AccountID: account, Kind: model.APIKeyKindSystem,
		Grants: []model.APIKeyGrant{{
			ID: model.NewID(), AccountID: account, ObjectType: string(TypeProject),
			ObjectID: &project, Action: ActionChallengePublish, Inheritable: true,
		}},
	}
	alias := Object{Type: TypeAlias, ID: model.NewID(), AccountID: account, ProjectID: project}
	if !Can(SubjectFromKey(k), ActionChallengePublish, alias) {
		t.Error("a key that predates the two kinds stopped working")
	}
}
