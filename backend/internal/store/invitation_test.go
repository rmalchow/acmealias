package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/invitation"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// noLimits turns the rate limits off for tests that are not about them. A zero
// value is "off" for every quota in this service, so this is the shape rather
// than a special case.
var noLimits = InvitationLimits{}

func issue(t *testing.T, db *gorm.DB, sc Scope, email string, now time.Time) (*model.Invitation, string) {
	t.Helper()
	owner := accountOwner(t, db, sc)
	inv, token, err := IssueInvitation(context.Background(), db, sc, email, owner, now)
	if err != nil {
		t.Fatalf("issue an invitation to %s: %v", email, err)
	}
	return inv, token
}

// TestIntegrationInvitationRoundTrip is the happy path, and the one property
// worth reading it for is what ACCEPTING produces: a member holding NO ROLE.
//
// An invitation offers membership and names no role (design V6), so the person
// is in the account and authorised for nothing in it until somebody assigns one.
func TestIntegrationInvitationRoundTrip(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()

	inv, token := issue(t, db, sc, "Alice@Corp.COM ", now)

	// Normalized on write, so the pending index and every later comparison see
	// one address rather than two spellings of one person.
	if inv.Email != "alice@corp.com" {
		t.Errorf("stored address %q, want it lower-cased and trimmed", inv.Email)
	}
	// The token is not recoverable from the row.
	if strings.Contains(string(inv.TokenHash), token) {
		t.Error("the stored hash contains the token")
	}

	got, acc, err := ResolveInvitation(ctx, db, token, now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != inv.ID || acc.ID != sc.AccountID() {
		t.Fatalf("resolved the wrong invitation or account")
	}

	alice, err := FindOrCreateUser(ctx, db, "alice-subject", "Alice", "alice@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := AcceptInvitation(ctx, db, acc, got, alice, now); err != nil {
		t.Fatalf("accept: %v", err)
	}

	ms, err := ListMembers(ctx, db, sc)
	if err != nil {
		t.Fatal(err)
	}
	var joined *model.AccountMember
	for i := range ms {
		if ms[i].UserID == alice.ID {
			joined = &ms[i]
		}
	}
	if joined == nil {
		t.Fatal("accepting produced no membership")
	}
	if joined.RoleID != nil {
		t.Errorf("the new member holds role %v; an invitation names no role, so they must hold none", *joined.RoleID)
	}

	// And the membership is real enough to select the account with, which is
	// what stops the roleless state being a dead end.
	memberships, err := MembershipsFor(ctx, db, sc.AccountID(), alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memberships) != 1 {
		t.Fatalf("MembershipsFor returned %d memberships, want 1 — a roleless member is still a member", len(memberships))
	}
	if n := len(memberships[0].RoleIDs()); n != 0 {
		t.Errorf("the roleless membership reports %d roles, want none", n)
	}
}

// TestIntegrationAcceptanceConsumesTheToken, and resolution does not.
//
// Resolving must be free, because links in mail are opened by things that are
// not people: security gateways follow every URL to scan it, clients generate
// previews. If arrival consumed the token, a meaningful share of invitations
// would be dead before their invitee ever saw them (design V4).
func TestIntegrationAcceptanceConsumesTheToken(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()

	_, token := issue(t, db, sc, "bob@corp.com", now)

	for i := 0; i < 3; i++ {
		if _, _, err := ResolveInvitation(ctx, db, token, now); err != nil {
			t.Fatalf("resolve %d: %v — resolving must not consume the token", i, err)
		}
	}

	inv, acc, err := ResolveInvitation(ctx, db, token, now)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := FindOrCreateUser(ctx, db, "bob-subject", "Bob", "bob@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := AcceptInvitation(ctx, db, acc, inv, bob, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveInvitation(ctx, db, token, now); !errors.Is(err, invitation.ErrInvalid) {
		t.Fatalf("resolve after acceptance = %v, want ErrInvalid", err)
	}
}

// TestIntegrationDeclineConsumesTheTokenToo.
//
// If a decline left the token live, "declined" would be a state a stray click
// could undo, and the person who declined would find themselves a member of an
// account they had refused (design V12).
func TestIntegrationDeclineConsumesTheToken(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()

	_, token := issue(t, db, sc, "carol@corp.com", now)
	inv, acc, err := ResolveInvitation(ctx, db, token, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeclineInvitation(ctx, db, acc, inv, now); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if _, _, err := ResolveInvitation(ctx, db, token, now); !errors.Is(err, invitation.ErrInvalid) {
		t.Fatalf("resolve after a decline = %v, want ErrInvalid", err)
	}

	// A decline is an answer, not a block. The account may ask again, and the
	// pending index has to let it.
	if _, _, err := IssueInvitation(ctx, db, sc, "carol@corp.com", accountOwner(t, db, sc), now); err != nil {
		t.Fatalf("re-inviting somebody who declined: %v — a decline is not a block", err)
	}
}

// TestIntegrationResendInvalidatesTheEarlierToken.
//
// Re-sending is what an administrator does when told "I never got it" — and the
// message that was never got is exactly the one that might have gone somewhere
// unintended, so it must not be left working (design V7).
func TestIntegrationResendInvalidatesTheEarlierToken(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()

	inv, first := issue(t, db, sc, "dave@corp.com", now)

	_, second, err := ResendInvitation(ctx, db, sc, inv.ID, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("re-send: %v", err)
	}
	if first == second {
		t.Fatal("re-sending returned the same token")
	}
	if _, _, err := ResolveInvitation(ctx, db, first, now.Add(time.Hour)); !errors.Is(err, invitation.ErrInvalid) {
		t.Fatalf("the earlier token still resolves (%v) — a re-send must invalidate it", err)
	}
	if _, _, err := ResolveInvitation(ctx, db, second, now.Add(time.Hour)); err != nil {
		t.Fatalf("the new token does not resolve: %v", err)
	}
}

// TestIntegrationInvitationStates: revoked, expired and already-answered
// invitations are all refused, and all refused THE SAME WAY.
//
// Same way, because a distinguishable response turns the endpoint into an oracle
// answering "was this token ever real" — the one question its unguessability is
// meant to make unanswerable.
func TestIntegrationInvitationStatesAreAllRefusedAlike(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()

	_, revokedToken := issue(t, db, sc, "erin@corp.com", now)
	revoked, _, err := ResolveInvitation(ctx, db, revokedToken, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RevokeInvitation(ctx, db, sc, revoked.ID, now); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, expiredToken := issue(t, db, sc, "frank@corp.com", now)

	cases := map[string]struct {
		token string
		at    time.Time
	}{
		"unknown": {"not-a-token-anybody-issued", now},
		"revoked": {revokedToken, now},
		// Expiry is evaluated AT RESOLUTION, not by a sweep, so this is dead
		// without any job having run (design V8).
		"expired": {expiredToken, now.Add(invitation.Lifetime + time.Minute)},
		"empty":   {"", now},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ResolveInvitation(ctx, db, tc.token, tc.at); !errors.Is(err, invitation.ErrInvalid) {
				t.Fatalf("resolve = %v, want ErrInvalid", err)
			}
		})
	}

	// And the fourteen days are really fourteen: alive on day thirteen.
	if _, _, err := ResolveInvitation(ctx, db, expiredToken, now.Add(13*24*time.Hour)); err != nil {
		t.Fatalf("an invitation is dead on day thirteen (%v); the lifetime is fourteen days precisely so "+
			"that it survives a holiday", err)
	}
}

// TestIntegrationOneOutstandingPerAddress, and an existing member is refused at
// issue rather than at acceptance.
func TestIntegrationInvitationIssueRefusals(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()
	owner := accountOwner(t, db, sc)

	issue(t, db, sc, "gina@corp.com", now)
	_, _, err := IssueInvitation(ctx, db, sc, "GINA@corp.com", owner, now)
	if !errors.Is(err, ErrInvitationOutstanding) {
		t.Fatalf("a second outstanding invitation to one address = %v, want ErrInvitationOutstanding "+
			"(and note the address differs only in case)", err)
	}

	// Somebody already in the account. Refused at ISSUE, because membership of
	// THIS account is already visible to the inviter through the roster, so
	// saying so discloses nothing — and deferring it would waste a message and
	// hand the recipient a dead end.
	hank, err := FindOrCreateUser(ctx, db, "hank-subject", "Hank", "hank@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddMemberWithoutRole(ctx, db, sc, hank.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := IssueInvitation(ctx, db, sc, "hank@corp.com", owner, now); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("inviting an existing member = %v, want ErrAlreadyMember — and note the existing "+
			"membership holds no role, which still counts as being in the account", err)
	}
}

// TestIntegrationInvitationsForAddressSpanAccounts is the second way in.
//
// A flow that depends on a link depends on mail delivery being perfect, and it
// is not: the message lands in spam, a gateway rewrites the URL, somebody reads
// it on a phone and signs in on a laptop. So being signed in as the invited
// address is enough — which grants nothing the token grants, since both require
// authenticating as that address (design V11).
func TestIntegrationInvitationsForAddressSpanAccounts(t *testing.T) {
	db, sc := isolatedDB(t)
	other := seedAccount(t, db, "other-account")
	ctx := context.Background()
	now := time.Now()

	issue(t, db, sc, "ivy@corp.com", now)
	issue(t, db, other, "ivy@corp.com", now)
	issue(t, db, sc, "jack@corp.com", now)

	invs, accs, err := InvitationsForAddress(ctx, db, "IVY@corp.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 2 {
		t.Fatalf("found %d invitations for ivy, want 2 across two accounts", len(invs))
	}
	if len(accs) != 2 {
		t.Fatalf("found %d accounts, want 2", len(accs))
	}
	for _, i := range invs {
		if i.Email != "ivy@corp.com" {
			t.Errorf("returned an invitation for %q", i.Email)
		}
	}

	// Somebody else's invitation is not presented.
	invs, _, err = InvitationsForAddress(ctx, db, "nobody@corp.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 0 {
		t.Fatalf("found %d invitations for an address with none", len(invs))
	}
}

// TestIntegrationCrossAccountInvitationRateLimit is the limit that cannot be
// omitted.
//
// Any authenticated person may create accounts freely, so a per-account limit is
// defeated by creating accounts. Only a limit keyed on the VICTIM'S address
// survives that, and this test is what says so (design V9).
func TestIntegrationCrossAccountInvitationRateLimit(t *testing.T) {
	db, first := isolatedDB(t)
	second := seedAccount(t, db, "limit-second")
	third := seedAccount(t, db, "limit-third")
	ctx := context.Background()
	now := time.Now()

	lim := InvitationLimits{PerAccountPerHour: 100, PerAddressPerDay: 2, ResendInterval: time.Hour}
	const target = "victim@corp.com"

	for _, sc := range []Scope{first, second} {
		if err := CheckInvitationRate(ctx, db, sc, target, lim, now); err != nil {
			t.Fatalf("within the allowance: %v", err)
		}
		issue(t, db, sc, target, now)
	}

	// A THIRD account, which under a per-account limit alone would have a fresh
	// allowance. It must not.
	var limited *ErrRateLimited
	if err := CheckInvitationRate(ctx, db, third, target, lim, now); !errors.As(err, &limited) {
		t.Fatalf("a third account could still invite the same person (%v). Per-account limits are "+
			"defeated by creating accounts, which anybody may do freely — the per-address limit is "+
			"the only one that survives it", err)
	}
	if limited.RetryAfter.IsZero() {
		t.Error("the refusal says nothing about when to retry, which is an instruction to retry in a loop")
	}

	// A day later the allowance is back.
	if err := CheckInvitationRate(ctx, db, third, target, lim, now.Add(25*time.Hour)); err != nil {
		t.Fatalf("still limited a day later: %v", err)
	}
}

func TestIntegrationPerAccountInvitationRateLimit(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()
	now := time.Now()

	lim := InvitationLimits{PerAccountPerHour: 2, PerAddressPerDay: 100}
	issue(t, db, sc, "one@corp.com", now)
	issue(t, db, sc, "two@corp.com", now)

	var limited *ErrRateLimited
	if err := CheckInvitationRate(ctx, db, sc, "three@corp.com", lim, now); !errors.As(err, &limited) {
		t.Fatalf("a third invitation in the hour = %v, want a rate refusal", err)
	}
	if err := CheckInvitationRate(ctx, db, sc, "three@corp.com", lim, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("still limited two hours later: %v", err)
	}
}

func TestResendRateLimit(t *testing.T) {
	now := time.Now()
	inv := &model.Invitation{LastSentAt: now}
	lim := InvitationLimits{ResendInterval: time.Hour}

	var limited *ErrRateLimited
	if err := CheckResendRate(inv, lim, now.Add(time.Minute)); !errors.As(err, &limited) {
		t.Fatalf("re-sending a minute later = %v, want a rate refusal", err)
	}
	if err := CheckResendRate(inv, lim, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("re-sending two hours later: %v", err)
	}
	// Off means off, like every other quota here.
	if err := CheckResendRate(inv, InvitationLimits{}, now); err != nil {
		t.Fatalf("with no interval configured: %v", err)
	}
}

// TestIntegrationAssignRoleConsumesTheRolelessRow.
//
// In place rather than beside, because memberships are per-role: inserting would
// leave the new member listed twice, once with the role they were just given and
// once as somebody still waiting for one (design V6).
func TestIntegrationAssignRoleConsumesTheRolelessRow(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	kim, err := FindOrCreateUser(ctx, db, "kim-subject", "Kim", "kim@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddMemberWithoutRole(ctx, db, sc, kim.ID); err != nil {
		t.Fatal(err)
	}
	role := &model.Role{Name: "operator", Description: "for the test"}
	if err := CreateRole(ctx, db, sc, role, AllowAnyGrant); err != nil {
		t.Fatal(err)
	}

	ms, _ := ListMembers(ctx, db, sc)
	var memberID model.ID
	for _, m := range ms {
		if m.UserID == kim.ID {
			memberID = m.ID
		}
	}
	if err := AssignRole(ctx, db, sc, memberID, role.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	ms, _ = ListMembers(ctx, db, sc)
	n := 0
	for _, m := range ms {
		if m.UserID == kim.ID {
			n++
			if m.RoleID == nil || *m.RoleID != role.ID {
				t.Errorf("membership %s holds %v, want the assigned role", m.ID, m.RoleID)
			}
		}
	}
	if n != 1 {
		t.Fatalf("kim now holds %d memberships, want 1 — assigning a role must consume the roleless "+
			"row rather than insert beside it", n)
	}

	// A second assignment to the same row loses, which is what stops two
	// administrators picking different roles at once from producing a lost
	// update.
	if err := AssignRole(ctx, db, sc, memberID, role.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("assigning to a membership that already holds a role = %v, want not-found", err)
	}
}

// TestIntegrationRolelessMemberIsRemovable. Without the nil branch in
// RemoveMember, `role_id = NULL` matches nothing in SQL and somebody waiting for
// a role could never be taken out of the account.
func TestIntegrationRolelessMemberIsRemovable(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	lee, err := FindOrCreateUser(ctx, db, "lee-subject", "Lee", "lee@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddMemberWithoutRole(ctx, db, sc, lee.ID); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMember(ctx, db, sc, lee.ID, nil); err != nil {
		t.Fatalf("removing a roleless member: %v", err)
	}
	if err := RemoveMember(ctx, db, sc, lee.ID, nil); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("removing them twice = %v, want not-found", err)
	}
}

// TestIntegrationOneRolelessMembershipPerPerson. The three-column unique key
// stops constraining anything once role_id may be null — nulls are distinct —
// so a partial index does that job, and this is what notices if it is dropped.
func TestIntegrationOneRolelessMembershipPerPerson(t *testing.T) {
	db, sc := isolatedDB(t)
	ctx := context.Background()

	mo, err := FindOrCreateUser(ctx, db, "mo-subject", "Mo", "mo@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddMemberWithoutRole(ctx, db, sc, mo.ID); err != nil {
		t.Fatal(err)
	}
	if err := AddMemberWithoutRole(ctx, db, sc, mo.ID); err == nil {
		t.Fatal("a second roleless membership was accepted; the same person would appear twice in " +
			"the roster, each row a separate placeholder for one role assignment")
	}
}
