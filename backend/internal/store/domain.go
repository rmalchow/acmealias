package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateDomain inserts a new domain, pointing at its (already resolved) alias.
// Find-or-create-the-alias and wildcard-conflict logic belongs to the
// inventory service (design D4), not here — this is the plain insert it
// calls once the alias id is known.
func CreateDomain(ctx context.Context, db *gorm.DB, sc Scope, d *model.Domain) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.Alias
		if err := sc.Where(tx).First(&parent, "id = ?", d.AliasID).Error; err != nil {
			return fmt.Errorf("parent alias %s: %w", d.AliasID, err)
		}
		d.AccountID = parent.AccountID
		d.ProjectID = parent.ProjectID
		if d.ID == model.NilID {
			d.ID = model.NewID()
		}
		if err := tx.Create(d).Error; err != nil {
			return fmt.Errorf("store: create domain: %w", err)
		}
		return nil
	})
}

// GetDomain loads a domain by id within the scope's account.
func GetDomain(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.Domain, error) {
	var d model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&d, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get domain %s: %w", id, err)
	}
	return &d, nil
}

// FindDomainByName looks up a domain by its exact normalized name, within the
// scope's account.
//
// A name is unique per account and not globally (design P8): two accounts may both
// hold example.com, and which of them can actually validate it is settled by DNS
// delegation rather than by this table. So this must be scoped — an unscoped
// lookup would find another tenant's row and report the name as taken.
func FindDomainByName(ctx context.Context, db *gorm.DB, sc Scope, name string) (*model.Domain, error) {
	var d model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Where("name = ?", name).First(&d).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: find domain %q: %w", name, err)
	}
	return &d, nil
}

// ListDomainsForLoadBalancer returns every domain on a load balancer, ordered by name.
func ListDomainsForLoadBalancer(ctx context.Context, db *gorm.DB, sc Scope, loadBalancerID model.ID) ([]model.Domain, error) {
	var ds []model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Where("load_balancer_id = ?", loadBalancerID).
			Order("name").
			Find(&ds).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list domains for load balancer %s: %w", loadBalancerID, err)
	}
	return ds, nil
}

// ListDomainsForAlias returns every domain sharing an alias — the set the
// caller must check before deciding whether deleting one leaves the alias
// orphaned (design D4).
func ListDomainsForAlias(ctx context.Context, db *gorm.DB, sc Scope, aliasID model.ID) ([]model.Domain, error) {
	var ds []model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Where("alias_id = ?", aliasID).
			Order("name").
			Find(&ds).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list domains for alias %s: %w", aliasID, err)
	}
	return ds, nil
}

// DeleteDomain deletes a domain by id. It does not touch the alias — the
// caller (inventory service) decides whether the alias is now orphaned and
// deletes it separately, since that also needs to cascade the DNS snapshot
// refresh (design D6).
func DeleteDomain(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx).Delete(&model.Domain{}, "id = ?", id)
		if res.Error != nil {
			return fmt.Errorf("store: delete domain %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: delete domain %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}
