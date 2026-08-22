package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func seedUser(t *testing.T, db *gorm.DB, subject string) model.ID {
	t.Helper()
	u, err := FindOrCreateUser(context.Background(), db, subject, subject, "")
	if err != nil {
		t.Fatalf("create user %s: %v", subject, err)
	}
	return u.ID
}

// The unrestricted role is the way back into an account, so the two things that
// could take it away are refused: editing it, and deleting it.
func TestIntegrationUnrestrictedRoleIsFixed(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	admin, err := UnrestrictedRole(ctx, db, sc)
	if err != nil {
		t.Fatalf("read unrestricted role: %v", err)
	}

	admin.Description = "something else"
	if err := UpdateRole(ctx, db, sc, admin); !errors.Is(err, ErrUnrestrictedRole) {
		t.Fatalf("update = %v, want ErrUnrestrictedRole", err)
	}
	if err := DeleteRole(ctx, db, sc, admin.ID); !errors.Is(err, ErrUnrestrictedRole) {
		t.Fatalf("delete = %v, want ErrUnrestrictedRole", err)
	}
	if err := CreateRole(ctx, db, sc, &model.Role{Name: "second-admin", Unrestricted: true}); !errors.Is(err, ErrUnrestrictedRole) {
		t.Fatalf("create a second unrestricted role = %v, want ErrUnrestrictedRole", err)
	}
}

// TestIntegrationLastUnrestrictedMemberIsKept covers the guard in the ordinary
// case: one member left, removal refused.
func TestIntegrationLastUnrestrictedMemberIsKept(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	admin, err := UnrestrictedRole(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	alone := seedUser(t, db, "u-alone")
	if err := AddMember(ctx, db, sc, alone, admin.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := RemoveMember(ctx, db, sc, alone, admin.ID); !errors.Is(err, ErrLastUnrestrictedMember) {
		t.Fatalf("remove = %v, want ErrLastUnrestrictedMember", err)
	}
}

// TestIntegrationConcurrentRemovalOfTheLastTwoMembers is the race the row lock
// exists for. Two removals run at once against an account with exactly two
// unrestricted members. Without the lock each reads two, each concludes one will
// remain, and the account ends with nobody who can administer it — with neither
// request having done anything wrong.
//
// Exactly one must succeed.
func TestIntegrationConcurrentRemovalOfTheLastTwoMembers(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	admin, err := UnrestrictedRole(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	first, second := seedUser(t, db, "u-first"), seedUser(t, db, "u-second")
	for _, u := range []model.ID{first, second} {
		if err := AddMember(ctx, db, sc, u, admin.ID); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, u := range []model.ID{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = RemoveMember(ctx, db, sc, u, admin.ID)
		}()
	}
	close(start)
	wg.Wait()

	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrLastUnrestrictedMember):
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("%d succeeded and %d were refused, want exactly one of each", ok, refused)
	}

	remaining, err := CountUnrestrictedMembers(ctx, db, sc, false)
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("%d unrestricted members remain, want 1 — the account must never be left unadministrable", remaining)
	}
}

// A role that still has members is not deleted: a membership pointing at nothing
// is not a state worth having, and cascading would silently strip access.
func TestIntegrationRoleWithMembersIsNotDeleted(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	r := &model.Role{Name: "operators", Description: "test"}
	if err := CreateRole(ctx, db, sc, r); err != nil {
		t.Fatalf("create role: %v", err)
	}
	u := seedUser(t, db, "u-op")
	if err := AddMember(ctx, db, sc, u, r.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := DeleteRole(ctx, db, sc, r.ID); err == nil {
		t.Fatal("a role with members was deleted")
	}
	if err := RemoveMember(ctx, db, sc, u, r.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if err := DeleteRole(ctx, db, sc, r.ID); err != nil {
		t.Fatalf("delete role after its last member left: %v", err)
	}
}

// Updating a role replaces its grants wholesale, which is the contract the
// screen shows: "these are the grants", not "these are some more grants".
func TestIntegrationUpdateRoleReplacesGrants(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	r := &model.Role{Name: "operators", Grants: []model.RoleGrant{
		{ObjectType: "*", Action: "inventory.view", Inheritable: true},
		{ObjectType: "*", Action: "dns.plan", Inheritable: true},
	}}
	if err := CreateRole(ctx, db, sc, r); err != nil {
		t.Fatalf("create role: %v", err)
	}

	r.Grants = []model.RoleGrant{{ObjectType: "*", Action: "inventory.view", Inheritable: true}}
	if err := UpdateRole(ctx, db, sc, r); err != nil {
		t.Fatalf("update role: %v", err)
	}

	got, err := RolesByName(ctx, db, sc, "operators")
	if err != nil || len(got) != 1 {
		t.Fatalf("read back: %v (%d roles)", err, len(got))
	}
	if len(got[0].Grants) != 1 || got[0].Grants[0].Action != "inventory.view" {
		t.Fatalf("grants = %+v, want exactly the one that was written", got[0].Grants)
	}
}
