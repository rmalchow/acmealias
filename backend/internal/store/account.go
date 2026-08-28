package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// What an account IS, as opposed to what is inside it: whether it works, how it
// comes into existence, and how it is paid for (design C1, C3, C6, C8).
//
// Everything here reads and writes `accounts`, which is the one table a Scope
// cannot filter — it has an id where every other table has an account_id — so
// every query below is written inside CrossAccount, as every other read of that
// table in this package is.

// Usable answers "does this account work", and it is the ONLY place that
// question is answered (design C3, C6).
//
//	usable(account) = kind == personal
//	                  OR (credits > 0 AND NOT staff_suspended)
//
// Three properties are worth being explicit about, because each of them was a
// live alternative:
//
//   - It is one comparison, not a threshold, a grace period or a low-water mark.
//     `metering-and-credits` argued for an overdraft — lapsed but still serving —
//     and C4 rejects it: that is a third state, and the middle state of three is
//     the one nobody can explain to a customer who has not paid. Refusing
//     /present does not take a site down; it stops renewals, and the certificate
//     on the wire keeps working for weeks either way.
//   - A personal account is outside the machine entirely rather than being a
//     team account that is permanently topped up. It has no balance at all — the
//     database refuses a non-zero one — because being free is what it is.
//   - The two suspensions are a conjunction, written here once, so that adding a
//     third reason later is one clause rather than an audit of every call site
//     (C6).
//
// One consequence is worth naming rather than discovering: because the personal
// clause comes FIRST and short-circuits, a staff suspension does not stop a
// personal account. That is design C6 as written, and it is left as written. The
// alternative — hoisting staff_suspended above the kind test — would be a better
// rule and a worse situation: the design says the formula lives in one place and
// is transcribed, and a function that quietly disagrees with the decision it
// cites is how the decision stops being true. When `staff-plane` gives somebody
// the power to set that column it will also be the change that decides what it
// means for a free account, and this is the one line it has to edit.
//
// A nil account is not usable. That is not a real state — every caller has just
// read the row — and it answers false rather than panicking because the failure
// mode of the alternative is a permitted request.
func Usable(acc *model.Account) bool {
	if acc == nil {
		return false
	}
	if acc.Kind == model.AccountKindPersonal {
		return true
	}
	return acc.Credits > 0 && !acc.StaffSuspended
}

// SuspensionReason says WHY an account does not work, in a sentence a caller can
// act on. It is separate from Usable because the predicate must stay one
// comparison (C3) while the message must distinguish the two suspensions: one is
// fixed by paying and the other is not, and C5's whole point is that a refusal
// carries its remedy. It returns "" for a usable account.
func SuspensionReason(acc *model.Account) string {
	switch {
	case Usable(acc):
		return ""
	case acc == nil:
		return "the account could not be read"
	case acc.StaffSuspended:
		return "the account is suspended; adding credit will not lift it"
	default:
		return "the account has no credit remaining"
	}
}

// ErrPersonalAccountHasNoRoster is the refusal to answer for roles, members or
// invitations in a personal account (design C9).
//
// It is a refusal rather than an empty collection on purpose: an empty list
// reads as "there are none yet", which invites a client to offer an Add button
// for something that can never exist. "There cannot be any" is a different
// sentence with a different remedy, and only one of the two is true here.
//
// It is NOT a permission failure, and the distinction matters at the surface:
// somebody told they lack permission goes looking for whoever can grant it, and
// in an account of one there is nobody (C11).
var ErrPersonalAccountHasNoRoster = errors.New("store: a personal account has no roles or members; it is one person's own workspace")

// ErrAccountSuspended is the refusal to act in a team account that does not
// work. The message names which of the two suspensions it is — see
// SuspensionReason.
var ErrAccountSuspended = errors.New("store: the account is suspended")

// ErrAmountNotOffered is the refusal of a checkout amount that is not one of the
// offered ones (design C8).
var ErrAmountNotOffered = errors.New("store: that is not an amount this service offers")

// CheckoutAmounts is the fixed set of credit amounts, in EUR (design C8).
//
// A fixed set rather than a number the caller supplies, and this is the load
// bearing half of the decision: there is no payment processor in the way to
// notice that a body said 1000000 instead of 100, so the only thing standing
// between the balance and an arbitrary number is that no arbitrary number is
// accepted. When a processor does arrive it takes over the question of whether
// the money moved; it does not take over this one, because the offered amounts
// are a product decision either way.
var CheckoutAmounts = []int64{25, 50, 100}

// IsOfferedAmount reports whether an amount is one this service sells.
func IsOfferedAmount(amount int64) bool {
	for _, a := range CheckoutAmounts {
		if a == amount {
			return true
		}
	}
	return false
}

// PersonalAccountRole is the single role a personal account holds, and the name
// is deliberately not one of DefaultRoles' four: it is not part of a vocabulary
// anybody edits, and seeing it in a query should not read as "the team default
// set, minus three".
const PersonalAccountRole = "OWNER"

// EnsurePersonalAccount returns the user's personal account, materializing it on
// the first call (design C1, task 3.1).
//
// There is no route that creates one and no form asking for a name: a user has a
// personal account because they are a user, so this runs at sign-in and nowhere
// else. Calling it again is a read.
//
// IDEMPOTENCE IS NOT THE READ — IT IS THE INDEX. Two concurrent first sign-ins
// for the same person both find no row and both insert one; the check below
// loses that race by construction. What actually enforces "at most one" is
// the schema's `unique (owner_user_id) where kind = 'personal'`, so the
// insert's unique violation is caught and turned back into a second read rather
// than reported (C2). The happy path is merely the common case.
//
// # Why the account has a role and a membership when C9 says it has neither
//
// A personal account holds one unrestricted role and one membership, for its
// owner. That looks like a contradiction of C9 and is not: C9 is about the
// roster as a THING THE TENANT HAS — screens, invitations, an Add button — and
// those are refused at the API surface (ErrPersonalAccountHasNoRoster). The rows
// underneath are how the owner has any authority at all, because every
// authorization path in the service reads authority out of account_members.
//
// The alternative was to special-case the owner in authz.SubjectForUser: no
// rows, and "owner of a personal account" is unrestricted by definition. It was
// rejected because the special case does not stay in one place — AccountsForUser
// joins account_members to build the picker, IsMember gates selection, and
// ListMembers, the audit actor and the grant subset rule all read the same rows.
// One kind of account whose authority came from somewhere else would be a second
// authorization model running beside the real one, in the accounts nobody tests.
func EnsurePersonalAccount(ctx context.Context, db *gorm.DB, user *model.User) (*model.Account, error) {
	if user == nil || user.ID == model.NilID {
		return nil, errors.New("store: a personal account needs the user it belongs to")
	}
	find := func(tx *gorm.DB) (*model.Account, error) {
		var acc model.Account
		err := tx.First(&acc, "owner_user_id = ? AND kind = ?", user.ID, model.AccountKindPersonal).Error
		if err != nil {
			return nil, err
		}
		return &acc, nil
	}

	var out *model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		existing, err := find(tx)
		switch {
		case err == nil:
			out = existing
			return nil
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("look for an existing personal account: %w", err)
		}
		acc, err := createPersonalAccount(tx, user)
		if err == nil {
			out = acc
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
		// The other sign-in won. Its transaction has committed by the time ours
		// sees the violation, so the row is there to be read — and reading it is
		// the whole answer, because both callers wanted the same account rather
		// than one each.
		return fmt.Errorf("%w", errRetryPersonal)
	})
	if errors.Is(err, errRetryPersonal) {
		err = CrossAccount(ctx, db, func(tx *gorm.DB) error {
			acc, err := find(tx)
			if err != nil {
				return fmt.Errorf("re-read after a concurrent materialization: %w", err)
			}
			out = acc
			return nil
		})
	}
	if err != nil {
		return nil, fmt.Errorf("store: personal account for user %s: %w", user.ID, err)
	}
	return out, nil
}

// errRetryPersonal escapes the first transaction so the re-read happens in a new
// one. It cannot happen inside the failed transaction: the unique violation has
// already aborted it, and every statement after that is refused by PostgreSQL
// until it rolls back.
var errRetryPersonal = errors.New("store: personal account was materialized concurrently")

// createPersonalAccount writes the row, its single role and its single
// membership. It runs inside the caller's cross-account transaction.
func createPersonalAccount(tx *gorm.DB, user *model.User) (*model.Account, error) {
	acc := model.Account{
		ID:          model.NewID(),
		Kind:        model.AccountKindPersonal,
		OwnerUserID: user.ID,
		// Credits stays zero and the database refuses anything else: a personal
		// account has no balance rather than a balance that happens to be empty
		// (C3).
		Name: personalAccountName(user),
	}
	// A personal account is NOT slug-addressable (C1) — there is nothing to
	// disambiguate, since a user has exactly one — but `accounts.slug` is still
	// `not null unique`, so a value has to exist. It is derived from the
	// identifier rather than from the person: a slug built from a display name
	// or an email would put the user's name into a URL-shaped, guessable string
	// that the CLI's --account happily accepts, and would collide the first time
	// two people share a name.
	//
	// Making the column nullable was the alternative. It was rejected as a
	// bigger change than the problem: `slug` is unique-not-null in the schema,
	// three lookups treat it as the human handle for an account, and
	// relaxing it would make "an account has a slug" something every one of them
	// has to re-check.
	acc.Slug = "personal-" + strings.ReplaceAll(acc.ID.String(), "-", "")
	if err := tx.Create(&acc).Error; err != nil {
		return nil, fmt.Errorf("create personal account: %w", err)
	}
	role := model.Role{
		AccountID:    acc.ID,
		Name:         PersonalAccountRole,
		Description:  "The owner of this personal account",
		Unrestricted: true,
	}
	if err := tx.Create(&role).Error; err != nil {
		return nil, fmt.Errorf("create the owner role: %w", err)
	}
	member := model.AccountMember{AccountID: acc.ID, UserID: user.ID, RoleID: &role.ID}
	if err := tx.Create(&member).Error; err != nil {
		return nil, fmt.Errorf("create the owner membership: %w", err)
	}
	return &acc, nil
}

// personalAccountName derives the account's name from the user (task 3.1a).
//
// It is not part of the tenant's vocabulary and is not renameable: there is
// nothing to disambiguate. The console labels the entry by KIND rather than by
// this string — the picker shows "Personal" — so this is what shows up in an
// audit trail and a support conversation, where the person's own name is what
// makes the row legible.
func personalAccountName(user *model.User) string {
	if n := strings.TrimSpace(user.DisplayName); n != "" {
		return n
	}
	return user.Subject
}

// isUniqueViolation reports whether err is PostgreSQL's 23505.
//
// Matched on the message rather than on a driver error type, because this
// package deliberately does not import the pgx error package — the store speaks
// gorm, and one string match is a smaller dependency than a driver type that
// then has to be true of every future driver. The cost is that a message change
// makes the concurrent materialization report an error instead of retrying,
// which is loud rather than silent, and TestIntegrationPersonalAccountRace is
// what would notice.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value violates unique constraint")
}

// AccountOwnedBy reads the account one user owns, by identifier, and answers
// gorm.ErrRecordNotFound for everything else — an account that does not exist,
// and an account that exists and belongs to somebody else.
//
// The uniform answer is the whole function. Checkout is the one route the
// tenancy invariant's usual machinery cannot serve: it is deliberately outside
// the account-scoped group, because the account it exists to repair is exactly
// the one a scope would refuse (design C4, C5), so its account comes from a path
// parameter and there is no selection to check it against. Reading the account
// first and comparing the owner afterwards made the route an existence oracle —
// 403 for an account that exists, 404 for one that does not — so anybody with a
// session could confirm an account identifier by guessing it.
//
// Asking the question as ONE query ("the account with this id that this person
// owns") is what removes the difference, and it also removes the construction
// the invariant forbids by name: the Scope handed back is built from the row the
// database returned, having established the caller's right to it, rather than
// from the identifier that was asked about.
func AccountOwnedBy(ctx context.Context, db *gorm.DB, accountID, ownerUserID model.ID) (Scope, *model.Account, error) {
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.First(&acc, "id = ? AND owner_user_id = ?", accountID, ownerUserID).Error
	})
	if err != nil {
		return Scope{}, nil, fmt.Errorf("store: account %s owned by %s: %w", accountID, ownerUserID, err)
	}
	return Scope{accountID: acc.ID}, &acc, nil
}

// Credit adds to a team account's balance (design C8, task 5.2).
//
// It writes the same row and the same balance change a real payment will, which
// is the point of building checkout before there is a processor: a fake that
// wrote different state would prove nothing about the path that replaces it.
// What is missing is the processor, not the mechanism.
//
// The row is locked for the length of the update so that two concurrent
// checkouts add up rather than one overwriting the other's read. The balance is
// incremented in SQL rather than read-modify-written in Go for the same reason,
// and the lock is belt-and-braces the day somebody adds a read to this function.
//
// It takes the account id rather than a Scope because it is called from checkout,
// which is reachable in an account that a Scope-taking route would refuse — the
// owner of a suspended account has to be able to pay for it (C4).
func Credit(ctx context.Context, db *gorm.DB, accountID model.ID, amount int64) (*model.Account, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrAmountNotOffered, amount)
	}
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Clauses(lockingClause()).First(&acc, "id = ?", accountID).Error; err != nil {
			return fmt.Errorf("read account: %w", err)
		}
		if acc.Kind != model.AccountKindTeam {
			// The database refuses this too (accounts_personal_has_no_balance),
			// and it is refused here as well so the caller gets a sentence
			// rather than a constraint name. A personal account is free; there
			// is nothing to buy (C3).
			return fmt.Errorf("%w: a personal account has no balance", ErrAmountNotOffered)
		}
		if err := tx.Model(&acc).UpdateColumn("credits", gorm.Expr("credits + ?", amount)).Error; err != nil {
			return fmt.Errorf("add credit: %w", err)
		}
		// Re-read rather than adding in Go: the row we return is what the
		// database holds, so a caller rendering the new balance is rendering the
		// balance and not our arithmetic.
		return tx.First(&acc, "id = ?", accountID).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: credit account %s: %w", accountID, err)
	}
	return &acc, nil
}

// CountProjects counts the projects in the scope's account, for the quota
// (design C13, task 3.5).
//
// Scoped, and it must be: an unscoped count of a policied table returns zero
// with no error under row-level security, and a quota that reads zero is a quota
// that never fires.
func CountProjects(ctx context.Context, db *gorm.DB, sc Scope) (int64, error) {
	var n int64
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx.Model(&model.Project{})).Count(&n).Error
	})
	if err != nil {
		return 0, fmt.Errorf("store: count projects: %w", err)
	}
	return n, nil
}
