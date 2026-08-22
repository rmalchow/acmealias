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

// RoleNames lists the names of every role in the account, for reporting a
// mapping that names one nobody created.
func RoleNames(ctx context.Context, db *gorm.DB, sc Scope) ([]string, error) {
	var names []string
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx.Model(&model.Role{})).Order("name").Pluck("name", &names).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: role names: %w", err)
	}
	return names, nil
}

// CreateRole inserts a role and its grants in one transaction.
//
// The unrestricted flag cannot be set through this path: the account's fixed
// role is created by the migration, and a second one would make "the role that
// can always get you back in" ambiguous. The database also refuses it, with a
// partial unique index — this is the error message.
func CreateRole(ctx context.Context, db *gorm.DB, sc Scope, r *model.Role) error {
	if r.Unrestricted {
		return fmt.Errorf("%w: an account has exactly one, created with it", ErrUnrestrictedRole)
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
func UpdateRole(ctx context.Context, db *gorm.DB, sc Scope, r *model.Role) error {
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
		m := &model.AccountMember{AccountID: sc.AccountID(), UserID: userID, RoleID: roleID}
		if err := tx.Create(m).Error; err != nil {
			return fmt.Errorf("store: add member: %w", err)
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
func RemoveMember(ctx context.Context, db *gorm.DB, sc Scope, userID, roleID model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var role model.Role
		if err := sc.Where(tx).First(&role, "id = ?", roleID).Error; err != nil {
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
		res := sc.Where(tx).Where("user_id = ? AND role_id = ?", userID, roleID).Delete(&model.AccountMember{})
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
