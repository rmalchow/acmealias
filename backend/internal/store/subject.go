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
