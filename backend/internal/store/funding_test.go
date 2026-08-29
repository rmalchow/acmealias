package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// TestUsableIsTheWholeRule is design C3 and C6 as one table.
//
// It takes no database, deliberately: the point of Usable is that "does this
// account work" has exactly one answer everywhere it is asked, so the answer has
// to be a pure function of the row. A version that needed a connection to decide
// would be a version somebody could get a different answer from.
func TestUsableIsTheWholeRule(t *testing.T) {
	personal := func(credits int64, staff bool) *model.Account {
		return &model.Account{Kind: model.AccountKindPersonal, Credits: credits, StaffSuspended: staff}
	}
	team := func(credits int64, staff bool) *model.Account {
		return &model.Account{Kind: model.AccountKindTeam, Credits: credits, StaffSuspended: staff}
	}
	cases := []struct {
		name string
		acc  *model.Account
		want bool
	}{
		// The boundary is exactly zero, in both directions (task 4.6). Not a
		// threshold, not a grace period, not a low-water mark — one comparison.
		{"a team account at one credit works", team(1, false), true},
		{"a team account at zero does not", team(0, false), false},
		{"a team account well funded works", team(100, false), true},

		// The two suspensions are a conjunction and neither lifts the other
		// (C6). Paying is what the first row is: credit present, staff
		// suspension present, still refused.
		{"paying does not lift a staff suspension", team(100, true), false},
		{"an unfunded staff-suspended account is refused twice over", team(0, true), false},

		// A personal account is outside the machine entirely. It is free, it has
		// no balance, and it is always usable — being free is what it is.
		{"a personal account is always usable", personal(0, false), true},

		// Including when staff have flagged it, and this is design C6 taken
		// literally rather than an oversight: the formula is
		// `personal OR (credits > 0 AND NOT staff_suspended)`, and the first
		// clause short-circuits. See the note in Usable — stopping an abusive
		// personal account is a staff-plane question, and answering it here by
		// quietly widening the second clause would make this function disagree
		// with the design it is transcribed from.
		{"a personal account is not stopped by the funding predicate", personal(0, true), true},

		// Not a state anything produces; answered false rather than panicking,
		// because the failure mode of the alternative is a permitted request.
		{"a missing account is not usable", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The table is the MONETIZED deployment, which is where the
			// predicate has anything to decide. The unmonetized case is one
			// answer for every row and is asserted below rather than doubling
			// this table with a column whose value never varies.
			if got := Usable(tc.acc, true); got != tc.want {
				t.Fatalf("Usable = %v, want %v", got, tc.want)
			}
			reason := SuspensionReason(tc.acc, true)
			if tc.want && reason != "" {
				t.Fatalf("a usable account carries the reason %q", reason)
			}
			if !tc.want && reason == "" {
				t.Fatal("an unusable account carries no reason; C5's whole point is that a refusal names its remedy")
			}
		})
	}

	// The two suspensions must not be described in the same sentence: one is
	// fixed by paying and one is not, and a message that does not distinguish
	// them sends somebody to buy credit that will change nothing.
	if SuspensionReason(team(0, false), true) == SuspensionReason(team(100, true), true) {
		t.Fatal("running out of credit and being stopped by staff read as the same refusal")
	}

	// UNMONETIZED: a balance decides nothing, because nobody was asked for one.
	// A zero balance is not a statement about a customer who was never billed,
	// and refusing them would be refusing them for not paying a price that was
	// never quoted.
	for _, acc := range []*model.Account{team(0, false), team(0, true), team(100, false), personal(0, false)} {
		if !Usable(acc, false) {
			t.Errorf("Usable(%+v, monetized=false) = false; an unmonetized deployment charges nobody", acc)
		}
		if r := SuspensionReason(acc, false); r != "" {
			t.Errorf("an unmonetized account carries the refusal %q", r)
		}
	}

	// The one thing the switch must NOT do is resurrect an account a human
	// stopped for a reason that has nothing to do with money... except that it
	// does, and deliberately: see the note in Usable. This asserts the choice
	// rather than leaving it to be discovered.
	if !Usable(team(0, true), false) {
		t.Error("a staff suspension bites on an unmonetized deployment; Usable says it should not, and staff-plane is the change that revisits it")
	}

	// A missing account is still not usable, whatever the mode. The failure mode
	// of the alternative is a permitted request.
	if Usable(nil, false) {
		t.Error("a nil account is usable when unmonetized")
	}
}

// TestIntegrationPersonalAccountIsMaterializedOnce is task 3.1: a user has a
// personal account because they are a user, and calling again is a read.
func TestIntegrationPersonalAccountIsMaterializedOnce(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()
	user, err := FindOrCreateUser(ctx, db, "u-materialize", "Ada Lovelace", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := EnsurePersonalAccount(ctx, db, user)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	second, err := EnsurePersonalAccount(ctx, db, user)
	if err != nil {
		t.Fatalf("materialize again: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("two calls produced two accounts, %s and %s", first.ID, second.ID)
	}
	if first.Kind != model.AccountKindPersonal {
		t.Fatalf("kind %q, want personal", first.Kind)
	}
	if first.Credits != 0 {
		t.Fatalf("a personal account holds %d credits; it has no balance at all", first.Credits)
	}
	// The name is derived from the person (task 3.1a). Nobody chose it, and no
	// route can change it.
	if first.Name != "Ada Lovelace" {
		t.Fatalf("name %q, want the user's own", first.Name)
	}
	// It is not slug-addressable: the slug exists because the column is NOT NULL
	// UNIQUE, and it must not be the person's name in a URL-shaped string.
	if first.Slug == "" || first.Slug == user.DisplayName || first.Slug == user.Subject {
		t.Fatalf("slug %q is guessable from the person; a personal account is not slug-addressable", first.Slug)
	}

	// The owner is a MEMBER, and unrestricted. Without that the account exists
	// and its owner has no authority in it — every authorization path in the
	// service reads authority out of account_members.
	sc := Scope{accountID: first.ID}
	ms, err := MembershipsFor(ctx, db, first.ID, user.ID)
	if err != nil {
		t.Fatalf("memberships: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("%d memberships, want exactly one — the owner and nobody else", len(ms))
	}
	role, err := UnrestrictedRole(ctx, db, sc)
	if err != nil {
		t.Fatalf("unrestricted role: %v", err)
	}
	if role.Name != PersonalAccountRole {
		t.Fatalf("role %q, want %q — a personal account is not seeded with the team default set", role.Name, PersonalAccountRole)
	}
	roles, err := RolesFor(ctx, db, sc)
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("%d roles in a personal account, want one; it has nobody to give a role to (design C9)", len(roles))
	}
}

// TestIntegrationConcurrentMaterializationProducesOneAccount is C2's race
// through the production path rather than through raw inserts.
//
// Two first sign-ins for the same person at the same instant both find no row
// and both try to insert one. The check in Go loses that race by construction;
// the partial unique index is what enforces the limit, and this asserts that
// EnsurePersonalAccount turns the loser's violation back into the winner's
// account rather than into an error the sign-in would fail on.
func TestIntegrationConcurrentMaterializationProducesOneAccount(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()
	user, err := FindOrCreateUser(ctx, db, "u-materialize-race", "Racer", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	const n = 4
	var wg sync.WaitGroup
	got := make([]*model.Account, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i], errs[i] = EnsurePersonalAccount(ctx, db, user)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("materialization %d failed: %v — a losing race must read the winner's account, not fail the sign-in", i, err)
		}
		if got[i].ID != got[0].ID {
			t.Fatalf("materialization %d produced account %s, %d produced %s", i, got[i].ID, 0, got[0].ID)
		}
	}

	var n64 int64
	if err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).
			Where("owner_user_id = ? AND kind = ?", user.ID, model.AccountKindPersonal).
			Count(&n64).Error
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n64 != 1 {
		t.Fatalf("%d personal accounts after %d concurrent sign-ins, want 1", n64, n)
	}
}

// TestIntegrationCreditAddsUp is design C8's "same row, same balance change".
func TestIntegrationCreditAddsUp(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()
	owner := seedUser(t, db, "u-credit")
	acc, err := CreateAccountWithOwner(ctx, db, "credited", "Credited", owner, 25, true)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if acc.Credits != 25 {
		t.Fatalf("opening balance %d, want 25 — an account is created by funding it", acc.Credits)
	}

	after, err := Credit(ctx, db, acc.ID, 100)
	if err != nil {
		t.Fatalf("credit: %v", err)
	}
	if after.Credits != 125 {
		t.Fatalf("balance %d after crediting 100 onto 25, want 125", after.Credits)
	}

	// Concurrent checkouts add up rather than one overwriting the other's read:
	// the increment happens in SQL under a row lock, not as a read-modify-write
	// in Go.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Credit(ctx, db, acc.ID, 25); err != nil {
				t.Errorf("concurrent credit: %v", err)
			}
		}()
	}
	wg.Wait()
	final, err := GetAccount(ctx, db, Scope{accountID: acc.ID})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if final.Credits != 225 {
		t.Fatalf("balance %d after four concurrent 25s onto 125, want 225 — a credit was lost", final.Credits)
	}
}

// TestIntegrationAPersonalAccountCannotBeCredited is C3 in the one place that
// would erode it: a crediting path that forgot to ask which kind of account it
// was crediting.
func TestIntegrationAPersonalAccountCannotBeCredited(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()
	user, err := FindOrCreateUser(ctx, db, "u-free", "Free", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc, err := EnsurePersonalAccount(ctx, db, user)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := Credit(ctx, db, acc.ID, 25); !errors.Is(err, ErrAmountNotOffered) {
		t.Fatalf("crediting a personal account: %v, want a refusal — being free is what it is", err)
	}
	back, err := GetAccount(ctx, db, Scope{accountID: acc.ID})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if back.Credits != 0 {
		t.Fatalf("a personal account holds %d credits after a refused checkout", back.Credits)
	}
}

// TestIsOfferedAmountRefusesEverythingElse is C8's fixed set. There is no
// processor in the way to notice a body saying a million, so this is the whole
// of what stands between the balance and an arbitrary number.
func TestIsOfferedAmountRefusesEverythingElse(t *testing.T) {
	for _, a := range CheckoutAmounts {
		if !IsOfferedAmount(a) {
			t.Fatalf("%d is offered and refused", a)
		}
	}
	for _, a := range []int64{0, -25, 1, 24, 26, 75, 101, 1000000} {
		if IsOfferedAmount(a) {
			t.Fatalf("%d is not an amount this service offers and was accepted", a)
		}
	}
}
