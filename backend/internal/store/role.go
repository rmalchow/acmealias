package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Roles as rows.
//
// The invariants here are the ones that keep an account reachable. They live in
// the store rather than in a service layer because both the API and the CLI
// write roles, and a rule enforced in one of them is a rule the other can walk
// past.

// ErrUnrestrictedRole is the refusal to edit or delete the account's fixed role.
var ErrUnrestrictedRole = errors.New("store: the unrestricted role cannot be changed")

// ErrLastUnrestrictedMember is the refusal to remove the last member of the
// unrestricted role.
var ErrLastUnrestrictedMember = errors.New("store: an account must keep at least one unrestricted member")

// RolesByName loads roles by name within the scope's account, with their grants.
func RolesByName(ctx context.Context, db *gorm.DB, sc Scope, names ...string) ([]model.Role, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var rs []model.Role
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Preload("Grants").Where("name IN ?", names).Order("name").Find(&rs).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: roles by name: %w", err)
	}
	return rs, nil
}

// GrantCheck decides whether one grant may be written. It is a parameter rather
// than a rule this package knows, because the rule lives in the permission model
// and this package must not import it — but it is a REQUIRED parameter, so a new
// write path cannot forget the check by omission (task 4.3).
//
// AllowAnyGrant is the honest way to say "no check here", and every use of it
// has to explain itself.
type GrantCheck func(g Grant) error

// Grant is the shape a grant takes as it crosses this boundary, so the check can
// be written against something without importing the model of either side.
type Grant struct {
	ObjectType  string
	ObjectID    *model.ID
	Action      string
	Inheritable bool
}

// AllowAnyGrant permits everything. It exists for `internal/adminctl`, whose
// authority is the database connection string: a check it could bypass by
// writing the row directly would be theatre, and the audit trail would claim a
// constraint that did not exist.
func AllowAnyGrant(Grant) error { return nil }

// CreateRole inserts a role and its grants in one transaction.
//
// The unrestricted flag cannot be set through this path: the account's fixed
// role is created by the migration, and a second one would make "the role that
// can always get you back in" ambiguous. The database also refuses it, with a
// partial unique index — this is the error message.
func CreateRole(ctx context.Context, db *gorm.DB, sc Scope, r *model.Role, check GrantCheck) error {
	if r.Unrestricted {
		return fmt.Errorf("%w: an account has exactly one, created with it", ErrUnrestrictedRole)
	}
	if err := checkGrants(check, r.Grants); err != nil {
		return err
	}
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		r.AccountID = sc.AccountID()
		for i := range r.Grants {
			r.Grants[i].AccountID = sc.AccountID()
		}
		if err := tx.Create(r).Error; err != nil {
			return fmt.Errorf("store: create role: %w", err)
		}
		return nil
	})
}

// UpdateRole replaces a role's name, description and grants.
//
// Grants are replaced wholesale rather than merged. A merge needs a stable
// identity per grant and an intent for rows the caller did not mention, and
// neither is obvious enough to guess: "these are the grants" is a sentence an
// operator can verify against the screen in front of them.
func UpdateRole(ctx context.Context, db *gorm.DB, sc Scope, r *model.Role, check GrantCheck) error {
	if err := checkGrants(check, r.Grants); err != nil {
		return err
	}
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var existing model.Role
		if err := sc.Where(tx).First(&existing, "id = ?", r.ID).Error; err != nil {
			return fmt.Errorf("store: update role %s: %w", r.ID, err)
		}
		if existing.Unrestricted {
			return fmt.Errorf("%w: %q confers everything in this account by definition",
				ErrUnrestrictedRole, existing.Name)
		}
		if err := sc.Where(tx.Model(&model.Role{})).Where("id = ?", r.ID).
			Updates(map[string]any{"name": r.Name, "description": r.Description}).Error; err != nil {
			return fmt.Errorf("store: update role %s: %w", r.ID, err)
		}
		if err := sc.Where(tx).Where("role_id = ?", r.ID).Delete(&model.RoleGrant{}).Error; err != nil {
			return fmt.Errorf("store: replace grants of role %s: %w", r.ID, err)
		}
		for i := range r.Grants {
			r.Grants[i].ID = model.NilID
			r.Grants[i].RoleID = r.ID
			r.Grants[i].AccountID = sc.AccountID()
		}
		if len(r.Grants) > 0 {
			if err := tx.Create(&r.Grants).Error; err != nil {
				return fmt.Errorf("store: write grants of role %s: %w", r.ID, err)
			}
		}
		return nil
	})
}

// DeleteRole removes a role. The unrestricted one is refused, and so is one that
// still has members — a membership pointing at nothing is not a state worth
// having, and cascading it would silently strip people of access.
func DeleteRole(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var r model.Role
		if err := sc.Where(tx).First(&r, "id = ?", id).Error; err != nil {
			return fmt.Errorf("store: delete role %s: %w", id, err)
		}
		if r.Unrestricted {
			return fmt.Errorf("%w: %q is how an account is administered", ErrUnrestrictedRole, r.Name)
		}
		var members int64
		if err := sc.Where(tx.Model(&model.AccountMember{})).Where("role_id = ?", id).Count(&members).Error; err != nil {
			return fmt.Errorf("store: delete role %s: %w", id, err)
		}
		if members > 0 {
			return fmt.Errorf("store: delete role %s: %d member(s) still hold it", id, members)
		}
		if err := sc.Where(tx).Delete(&model.Role{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("store: delete role %s: %w", id, err)
		}
		return nil
	})
}

// AddMember binds a user to a role in the scope's account.
func AddMember(ctx context.Context, db *gorm.DB, sc Scope, userID, roleID model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		m := &model.AccountMember{AccountID: sc.AccountID(), UserID: userID, RoleID: &roleID}
		if err := tx.Create(m).Error; err != nil {
			return fmt.Errorf("store: add member: %w", err)
		}
		return nil
	})
}

// AddMemberWithoutRole is what accepting an invitation writes: the person is in
// the account and holds nothing until somebody gives them a role
// (account-invitations V6).
//
// It is a separate function rather than AddMember with a nil argument because
// the two are different intentions and only one of them is ever what a caller
// meant. A nil slipping into AddMember from a decoded request body would produce
// this silently; a caller has to ask for it by name.
func AddMemberWithoutRole(ctx context.Context, db *gorm.DB, sc Scope, userID model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		m := &model.AccountMember{AccountID: sc.AccountID(), UserID: userID}
		if err := tx.Create(m).Error; err != nil {
			return fmt.Errorf("store: add member without a role: %w", err)
		}
		return nil
	})
}

// AssignRole gives a role to a membership that holds none, IN PLACE.
//
// In place, and not by inserting a second row, because memberships are per-role:
// the unique key is (account_id, user_id, role_id), so a person may legitimately
// hold several. Inserting would therefore leave the new member listed twice —
// once with the role they were just given, and once as somebody still waiting
// for one — and the roster would be reporting a state that no longer exists.
//
// The WHERE clause requires role_id to still be null, so two administrators
// assigning different roles at once produce one winner and one ErrRecordNotFound
// rather than a lost update. If an account genuinely wants somebody in two
// roles, that is AddMember, afterwards.
func AssignRole(ctx context.Context, db *gorm.DB, sc Scope, memberID, roleID model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		// Resolved under the scope first, so a role identifier from another
		// account cannot be written onto this account's membership. The
		// composite foreign key would refuse it anyway; this is what turns that
		// refusal into a legible error instead of a constraint violation.
		var role model.Role
		if err := sc.Where(tx).First(&role, "id = ?", roleID).Error; err != nil {
			return fmt.Errorf("store: assign role %s: %w", roleID, err)
		}
		res := sc.Where(tx).
			Model(&model.AccountMember{}).
			Where("id = ? AND role_id IS NULL", memberID).
			Update("role_id", roleID)
		if res.Error != nil {
			return fmt.Errorf("store: assign role: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: assign role: %w", gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// RemoveMember unbinds a user from a role, refusing the removal that would leave
// the account's unrestricted role empty.
//
// The guard reads the remaining members with SELECT ... FOR UPDATE inside the
// same transaction as the delete. That is the whole mechanism: without the lock,
// two concurrent removals each see two members, each concludes one will remain,
// and together they remove both — leaving an account nobody can administer and
// no single request that did anything wrong.
// A nil roleID removes a ROLELESS membership. It needs no last-member guard:
// a membership holding no role cannot be the last member of the unrestricted
// one, and `role_id = NULL` matches nothing in SQL — so a caller passing the
// null through as a value would delete zero rows, report not-found, and leave
// somebody in an account nobody can remove them from.
func RemoveMember(ctx context.Context, db *gorm.DB, sc Scope, userID model.ID, roleID *model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		if roleID == nil {
			res := sc.Where(tx).Where("user_id = ? AND role_id IS NULL", userID).Delete(&model.AccountMember{})
			if res.Error != nil {
				return fmt.Errorf("store: remove roleless member: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("store: remove roleless member: %w", gorm.ErrRecordNotFound)
			}
			return nil
		}
		var role model.Role
		if err := sc.Where(tx).First(&role, "id = ?", *roleID).Error; err != nil {
			return fmt.Errorf("store: remove member: %w", err)
		}
		if role.Unrestricted {
			remaining, err := CountUnrestrictedMembers(ctx, tx, sc, true)
			if err != nil {
				return err
			}
			if remaining <= 1 {
				return fmt.Errorf("%w: %q would be left with none", ErrLastUnrestrictedMember, role.Name)
			}
		}
		res := sc.Where(tx).Where("user_id = ? AND role_id = ?", userID, *roleID).Delete(&model.AccountMember{})
		if res.Error != nil {
			return fmt.Errorf("store: remove member: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: remove member: %w", gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// ListMembers returns the account's roster, with each member's user and role.
func ListMembers(ctx context.Context, db *gorm.DB, sc Scope) ([]model.AccountMember, error) {
	var ms []model.AccountMember
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Preload("User").Preload("Role").Find(&ms).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list members: %w", err)
	}
	return ms, nil
}

// checkGrants runs the write-time rule over a role's grants.
func checkGrants(check GrantCheck, grants []model.RoleGrant) error {
	if check == nil {
		return fmt.Errorf("store: a grant write was attempted with no check; " +
			"pass AllowAnyGrant if that is genuinely intended and say why")
	}
	for _, g := range grants {
		if err := check(Grant{
			ObjectType: g.ObjectType, ObjectID: g.ObjectID,
			Action: g.Action, Inheritable: g.Inheritable,
		}); err != nil {
			return err
		}
	}
	return nil
}
