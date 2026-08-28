package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// lockingClause is SELECT ... FOR UPDATE, named so the call site reads as what
// it is doing rather than as a clause literal.
func lockingClause() clause.Locking { return clause.Locking{Strength: "UPDATE"} }

// Loading subjects: the rows behind an authorization decision.
//
// A subject is an anchor plus a set of grants, and this is where both come from.
// The evaluator in internal/authz does not know whether it is looking at a person
// or at an API key, which is the point — the two differ in how they authenticate,
// not in how they are authorized.

// MembershipsFor returns a user's memberships in one account, with their roles
// and grants preloaded.
//
// It is the producer of the Membership values that ScopeFor accepts, which is
// why it returns them rather than raw rows: a Scope must come from something the
// database said, never from something a request asked for.
func MembershipsFor(ctx context.Context, db *gorm.DB, accountID, userID model.ID) ([]Membership, error) {
	var rows []model.AccountMember
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Preload("Role").
			Where("account_id = ? AND user_id = ?", accountID, userID).
			Find(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: memberships for user %s in account %s: %w", userID, accountID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	m := Membership{accountID: accountID, userID: userID}
	for _, r := range rows {
		m.roleIDs = append(m.roleIDs, r.RoleID)
	}
	return []Membership{m}, nil
}

// AccountsForUser lists the accounts a user belongs to, for the account picker a
// session with no selection shows. Cross-account by construction: the question
// is which accounts this person can reach, which cannot be answered from inside
// any one of them.
func AccountsForUser(ctx context.Context, db *gorm.DB, userID model.ID) ([]model.Account, error) {
	var accs []model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Table("accounts").
			Joins("join account_members m on m.account_id = accounts.id").
			Where("m.user_id = ?", userID).
			Distinct().
			Order("accounts.slug").
			Find(&accs).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: accounts for user %s: %w", userID, err)
	}
	return accs, nil
}

// FindOrCreateUser resolves the identity provider's subject to a user row,
// creating one the first time that subject is seen.
//
// Unscoped, necessarily: a user exists before belonging to any account, and this
// runs at sign-in when no account is selected.
func FindOrCreateUser(ctx context.Context, db *gorm.DB, subject, displayName, email string) (*model.User, error) {
	var u model.User
	err := db.WithContext(ctx).Where("subject = ?", subject).First(&u).Error
	switch {
	case err == nil:
		return &u, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("store: find user %q: %w", subject, err)
	}
	u = model.User{Subject: subject, DisplayName: displayName}
	if email != "" {
		u.Email = &email
	}
	if err := db.WithContext(ctx).Create(&u).Error; err != nil {
		return nil, fmt.Errorf("store: create user %q: %w", subject, err)
	}
	return &u, nil
}

// RolesFor loads roles with their grants, within one account.
func RolesFor(ctx context.Context, db *gorm.DB, sc Scope, ids ...model.ID) ([]model.Role, error) {
	var rs []model.Role
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		q := sc.Where(tx).Preload("Grants").Order("name")
		if len(ids) > 0 {
			q = q.Where("id IN ?", ids)
		}
		return q.Find(&rs).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: roles: %w", err)
	}
	return rs, nil
}

// UnrestrictedRole returns the account's fixed role.
func UnrestrictedRole(ctx context.Context, db *gorm.DB, sc Scope) (*model.Role, error) {
	var r model.Role
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Where("unrestricted").First(&r).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: unrestricted role: %w", err)
	}
	return &r, nil
}

// CountUnrestrictedMembers counts the members of the account's fixed role,
// optionally locking the rows so a caller can decide whether a removal would
// leave it empty without another transaction observing the same remaining
// member.
//
// The lock is the whole point: two concurrent removals must not each see two
// members and both proceed. FOR UPDATE serializes them, so the second sees the
// count the first left behind.
// It runs inside Scope.Run, which reuses an already-open transaction rather than
// nesting. That matters more than it looks: called with a bare handle and no
// scope, the row-level security policy filters every row out and the count comes
// back as zero — a caller checking "is anyone left" would then conclude nobody
// is. Wrapping it here means the question cannot be asked in a way that answers
// wrongly.
func CountUnrestrictedMembers(ctx context.Context, db *gorm.DB, sc Scope, lock bool) (int64, error) {
	var n int64
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		q := tx.
			Table("account_members m").
			Joins("join roles r on r.id = m.role_id").
			Where("m.account_id = ? AND r.unrestricted", sc.AccountID())
		if lock {
			// A plain COUNT cannot be locked, so the rows are selected and
			// counted in Go — the set is a handful of members, and correctness
			// here is worth more than the round trip.
			var ids []model.ID
			if err := q.Clauses(lockingClause()).Pluck("m.id", &ids).Error; err != nil {
				return err
			}
			n = int64(len(ids))
			return nil
		}
		return q.Count(&n).Error
	})
	if err != nil {
		return 0, fmt.Errorf("store: count unrestricted members: %w", err)
	}
	return n, nil
}

// GetAccount reads the account a scope confines to.
func GetAccount(ctx context.Context, db *gorm.DB, sc Scope) (*model.Account, error) {
	var a model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.First(&a, "id = ?", sc.AccountID()).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get account %s: %w", sc.AccountID(), err)
	}
	return &a, nil
}

// CreateAccountWithOwner creates an account and makes one user its first
// unrestricted member, indivisibly (task 7.4a).
//
// The two halves cannot be separated. An account whose creation succeeded and
// whose membership did not is an account nobody can administer — and no route
// could repair it, because every repairing route is one that needs a member.
//
// Cross-account by construction: it runs before the account it is creating
// exists, so there is no scope to run it in.
//
// credits is the account's opening balance, and it is a PARAMETER rather than a
// zero somebody tops up afterwards because of design C8: a team account is
// created by funding it, in one transaction, so that no team account exists that
// has never been funded. An account created with zero is suspended from its first
// instant, which is a state with no purpose — its owner would land on the pay
// screen of an account they just made. Callers that genuinely want that (a
// fixture, a test of the boundary) have to say so, which is the point of making
// them pass the number.
//
// It takes the *gorm.DB the caller holds, so a checkout can pass its transaction
// and get the account, the roles, the membership, the credit and the audit entry
// in one commit.
//
// The positive-credits check is HERE and not in the database, and the asymmetry
// with accounts_personal_has_no_balance is not an oversight. A team account at
// zero is not an illegal row — it is the SUSPENDED state the whole of C3 and C4
// is about, and every account reaches it eventually — so `check (kind =
// 'personal' or credits > 0)` would forbid the state the design requires. The
// rule is about creation only, and creation is a function rather than a
// constraint, so this is the lowest floor there is. Both callers already refuse
// it (POST /accounts against CheckoutAmounts, `adminctl account create` against
// --credits); this is what a third one — a repair script, a seed, the staff
// plane — walks into instead of quietly producing an account that is broken from
// its first instant.
func CreateAccountWithOwner(ctx context.Context, db *gorm.DB, slug, name string, userID model.ID, credits int64) (*model.Account, error) {
	if credits <= 0 {
		return nil, fmt.Errorf("%w: a team account is created by funding it, and %d leaves it suspended "+
			"from its first instant (design C8)", ErrAmountNotOffered, credits)
	}
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		// A team account, always: a personal account is materialized rather than
		// requested (design C1), so nothing that goes through a slug and a name
		// a caller chose can be one. The owner is the same user the first
		// membership names — the column records what was already true.
		acc = model.Account{
			Slug: slug, Name: name,
			Kind: model.AccountKindTeam, OwnerUserID: userID, Credits: credits,
		}
		if err := tx.Create(&acc).Error; err != nil {
			return fmt.Errorf("create account: %w", err)
		}
		// The default role set (design C10, task 2.5). It used to be seeded by
		// a migration across every account that existed at migration time,
		// which is what made the four names a global vocabulary; it is written
		// per account here instead, which is what makes them this account's own.
		//
		// Personal accounts never reach this line, and that is the point: they
		// have nobody to give a role to (design C9).
		var unrestricted model.ID
		for _, r := range DefaultRoles() {
			r.AccountID = acc.ID
			for i := range r.Grants {
				r.Grants[i].AccountID = acc.ID
			}
			if err := tx.Create(&r).Error; err != nil {
				return fmt.Errorf("create role %q: %w", r.Name, err)
			}
			if r.Unrestricted {
				unrestricted = r.ID
			}
		}
		if unrestricted == model.NilID {
			// Unreachable while DefaultRoles carries ADMIN, and checked because
			// the alternative is an account with a membership pointing at the
			// nil role — which the foreign key would refuse with a message
			// nobody could act on.
			return fmt.Errorf("the default role set has no unrestricted role")
		}
		member := model.AccountMember{AccountID: acc.ID, UserID: userID, RoleID: unrestricted}
		if err := tx.Create(&member).Error; err != nil {
			return fmt.Errorf("create first membership: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: create account %q: %w", slug, err)
	}
	return &acc, nil
}

// ErrPersonalAccountFixed is the refusal to hand a personal account to somebody
// else. It is a property of what the account is, not of who is asking: a
// personal account handed over is a team account with the team parts missing
// (design C1).
var ErrPersonalAccountFixed = errors.New("store: a personal account belongs to its user and cannot be transferred")

// ErrSecondPersonalAccount is the refusal to leave one user owning two personal
// accounts (design C2).
var ErrSecondPersonalAccount = errors.New("store: a user may own only one personal account")

// TransferOwnership moves a team account to another user.
//
// It takes a Scope because the account being transferred is the one the caller
// is already acting in, and CrossAccount because `accounts` is the one table a
// Scope cannot filter — it has an id where every other table has an account_id,
// which is why every read of it in this package is written this way.
//
// The row is locked for the length of the check, so two concurrent transfers
// serialize rather than both reading the same owner and both writing over it.
// The database refuses both of the refusals below on its own (the
// accounts_kind_is_immutable trigger and the accounts_one_personal_per_owner
// index); this is where they become a sentence the caller can read instead of a
// constraint name.
//
// Establishing the recipient's membership is the CALLER's job, in this same
// transaction — pass the transaction handle. An owner who is not a member is an
// account whose owner cannot open it.
func TransferOwnership(ctx context.Context, db *gorm.DB, sc Scope, newOwnerUserID model.ID) error {
	if !sc.Valid() {
		return ErrNoScope
	}
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		var acc model.Account
		if err := tx.Clauses(lockingClause()).First(&acc, "id = ?", sc.AccountID()).Error; err != nil {
			return fmt.Errorf("read account: %w", err)
		}
		if acc.Kind == model.AccountKindPersonal {
			return ErrPersonalAccountFixed
		}
		if acc.OwnerUserID == newOwnerUserID {
			return nil
		}
		// C2's other door. Unreachable while the refusal above holds, because a
		// personal account never reaches this line — and written anyway,
		// because the day somebody adds "transfer a personal account, for
		// support reasons" the limit would otherwise be gone without anybody
		// removing it.
		if acc.Kind == model.AccountKindPersonal {
			var n int64
			if err := tx.Model(&model.Account{}).
				Where("owner_user_id = ? AND kind = ?", newOwnerUserID, model.AccountKindPersonal).
				Count(&n).Error; err != nil {
				return fmt.Errorf("count personal accounts of %s: %w", newOwnerUserID, err)
			}
			if n > 0 {
				return fmt.Errorf("%w: %s already owns one", ErrSecondPersonalAccount, newOwnerUserID)
			}
		}
		if err := tx.Model(&acc).Update("owner_user_id", newOwnerUserID).Error; err != nil {
			return fmt.Errorf("write owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: transfer account %s to %s: %w", sc.AccountID(), newOwnerUserID, err)
	}
	return nil
}

// IsMember reports whether a user holds any membership in an account.
//
// Cross-account: it answers "may this person act here", which is asked before
// any scope exists and is exactly the question a scope would beg.
func IsMember(ctx context.Context, db *gorm.DB, accountID, userID model.ID) (bool, error) {
	var n int64
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Model(&model.AccountMember{}).
			Where("account_id = ? AND user_id = ?", accountID, userID).
			Count(&n).Error
	})
	if err != nil {
		return false, fmt.Errorf("store: membership of %s in %s: %w", userID, accountID, err)
	}
	return n > 0, nil
}
