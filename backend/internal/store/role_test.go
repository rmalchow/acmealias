package store

import (
	"context"
	"errors"
	"strings"
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
	if err := UpdateRole(ctx, db, sc, admin, AllowAnyGrant); !errors.Is(err, ErrUnrestrictedRole) {
		t.Fatalf("update = %v, want ErrUnrestrictedRole", err)
	}
	if err := DeleteRole(ctx, db, sc, admin.ID); !errors.Is(err, ErrUnrestrictedRole) {
		t.Fatalf("delete = %v, want ErrUnrestrictedRole", err)
	}
	if err := CreateRole(ctx, db, sc, &model.Role{Name: "second-admin", Unrestricted: true}, AllowAnyGrant); !errors.Is(err, ErrUnrestrictedRole) {
		t.Fatalf("create a second unrestricted role = %v, want ErrUnrestrictedRole", err)
	}
}

// TestIntegrationOwnerCannotBeDemoted is task 0.3: the person the account is
// billed to stays an administrator of it.
//
// The refusal must be its OWN error and not the last-member one (task 0.4). Here
// the owner is also the only unrestricted member, so both constraints are hit at
// once — and the message has to name the one whose remedy works. "Appoint a
// second administrator" is advice that leads nowhere: doing it does not let the
// removal through.
func TestIntegrationOwnerCannotBeDemoted(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	admin, err := UnrestrictedRole(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	owner := accountOwner(t, db, sc)

	if err := RemoveMember(ctx, db, sc, owner, &admin.ID); !errors.Is(err, ErrOwnerMustBeUnrestricted) {
		t.Fatalf("remove the sole owner-administrator = %v, want ErrOwnerMustBeUnrestricted", err)
	}

	// And still refused once the account has other administrators, which is the
	// case the last-member guard would have allowed: the role keeps a member,
	// but not the one who pays for it.
	second := seedUser(t, db, "u-second-admin")
	if err := AddMember(ctx, db, sc, second, admin.ID); err != nil {
		t.Fatalf("add a second administrator: %v", err)
	}
	if err := RemoveMember(ctx, db, sc, owner, &admin.ID); !errors.Is(err, ErrOwnerMustBeUnrestricted) {
		t.Fatalf("remove the owner beside another administrator = %v, want ErrOwnerMustBeUnrestricted", err)
	}
	// The other one goes, though. The rule is about the owner, not about
	// freezing the roster.
	if err := RemoveMember(ctx, db, sc, second, &admin.ID); err != nil {
		t.Fatalf("remove a non-owner administrator: %v", err)
	}
}

// ownerOutsideTheRole hands the account to a user who is not a member of it,
// which is the only state in which the LAST-MEMBER guard is still reachable:
// while the owner is an unrestricted member — which task 0.3 now enforces on
// every removal — the count can never fall to zero, because the removal that
// would take it there is refused earlier and for a different reason.
//
// It is a real state and not a contrivance. store.TransferOwnership moves the
// column and says in its own doc comment that establishing the recipient's
// membership is the caller's job; a caller that does not do it produces exactly
// this. The last-member guard is what stands between that account and nobody
// being able to administer it at all.
func ownerOutsideTheRole(t *testing.T, db *gorm.DB, sc Scope, subject string) {
	t.Helper()
	stranger := seedUser(t, db, subject)
	if err := TransferOwnership(context.Background(), db, sc, stranger); err != nil {
		t.Fatalf("transfer ownership to %s: %v", subject, err)
	}
}

// TestIntegrationEnsureUnrestrictedMember is the transfer case that the old
// IsMember check got wrong: the recipient was already in the account, holding a
// role that administers nothing. "Is a member" answered yes, the membership
// write was skipped, and the new owner could not administer what they now owned
// — a state nothing could repair, since promoting them is itself an unrestricted
// action.
func TestIntegrationEnsureUnrestrictedMember(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	admin, err := UnrestrictedRole(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	readers := &model.Role{Name: "readers", Description: "test"}
	if err := CreateRole(ctx, db, sc, readers, AllowAnyGrant); err != nil {
		t.Fatalf("create role: %v", err)
	}
	user := seedUser(t, db, "u-reader")
	if err := AddMember(ctx, db, sc, user, readers.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if err := EnsureUnrestrictedMember(ctx, db, sc, user); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// Twice, because a transfer to somebody who is already an administrator
	// must not abort the transaction on a duplicate key.
	if err := EnsureUnrestrictedMember(ctx, db, sc, user); err != nil {
		t.Fatalf("ensure, again: %v", err)
	}

	ms, err := ListMembers(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	var held int
	for _, m := range ms {
		if m.UserID == user && m.RoleID != nil && *m.RoleID == admin.ID {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("%d unrestricted memberships for the recipient, want exactly 1", held)
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
	// The account's first unrestricted member is its owner, so ownership is
	// moved away first — otherwise the removal is refused by the owner rule
	// above and this test would be asserting the wrong guard.
	alone := accountOwner(t, db, sc)
	ownerOutsideTheRole(t, db, sc, "u-absent-owner")
	if err := RemoveMember(ctx, db, sc, alone, &admin.ID); !errors.Is(err, ErrLastUnrestrictedMember) {
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
	// The owner is already the first member, so one more makes exactly two —
	// and ownership then moves away, because otherwise one of the two removals
	// is refused by the owner rule (task 0.3) rather than by the lock, and the
	// test would pass while proving nothing about the race.
	first, second := accountOwner(t, db, sc), seedUser(t, db, "u-second")
	if err := AddMember(ctx, db, sc, second, admin.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ownerOutsideTheRole(t, db, sc, "u-absent-owner")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, u := range []model.ID{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = RemoveMember(ctx, db, sc, u, &admin.ID)
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
	if err := CreateRole(ctx, db, sc, r, AllowAnyGrant); err != nil {
		t.Fatalf("create role: %v", err)
	}
	u := seedUser(t, db, "u-op")
	if err := AddMember(ctx, db, sc, u, r.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := DeleteRole(ctx, db, sc, r.ID); err == nil {
		t.Fatal("a role with members was deleted")
	}
	if err := RemoveMember(ctx, db, sc, u, &r.ID); err != nil {
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
	if err := CreateRole(ctx, db, sc, r, AllowAnyGrant); err != nil {
		t.Fatalf("create role: %v", err)
	}

	r.Grants = []model.RoleGrant{{ObjectType: "*", Action: "inventory.view", Inheritable: true}}
	if err := UpdateRole(ctx, db, sc, r, AllowAnyGrant); err != nil {
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

// The check constraint 000004 added, asserted at the database rather than only
// at authz.Grant.Validate.
//
// Two layers for one rule, deliberately. Validate is what produces a sentence a
// caller can act on; the constraint is what makes the rule true of rows that
// never went through Go — a psql session, a future import, a code path somebody
// adds without knowing the vocabulary moved. The Go check being removed by
// accident would leave this test as the one that notices.
func TestIntegrationDatabaseRefusesTheRemovedObjectType(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	role := &model.Role{Name: "writer"}
	if err := CreateRole(ctx, db, sc, role, AllowAnyGrant); err != nil {
		t.Fatalf("create role: %v", err)
	}

	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return tx.Create(&model.RoleGrant{
			RoleID: role.ID, AccountID: sc.AccountID(),
			ObjectType: "load_balancer", Action: "inventory.write",
		}).Error
	})
	if err == nil {
		t.Fatal("a grant naming load_balancer was stored; the object type is gone and the " +
			"constraint that refuses it is what keeps the grammar from having two words for one thing")
	}
	if !strings.Contains(err.Error(), "object_type_not_load_balancer") {
		t.Errorf("refused with %v, want the check constraint rather than something incidental", err)
	}
}
