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
func CreateDomain(ctx context.Context, db *gorm.DB, d *model.Domain) error {
	if err := db.WithContext(ctx).Create(d).Error; err != nil {
		return fmt.Errorf("store: create domain: %w", err)
	}
	return nil
}

// GetDomain loads a domain by id.
func GetDomain(ctx context.Context, db *gorm.DB, id int64) (*model.Domain, error) {
	var d model.Domain
	if err := db.WithContext(ctx).First(&d, id).Error; err != nil {
		return nil, fmt.Errorf("store: get domain %d: %w", id, err)
	}
	return &d, nil
}

// FindDomainByName looks up a domain by its exact normalized name.
func FindDomainByName(ctx context.Context, db *gorm.DB, name string) (*model.Domain, error) {
	var d model.Domain
	if err := db.WithContext(ctx).Where("name = ?", name).First(&d).Error; err != nil {
		return nil, fmt.Errorf("store: find domain %q: %w", name, err)
	}
	return &d, nil
}

// ListDomainsForLoadBalancer returns every domain on a load balancer, ordered by name.
func ListDomainsForLoadBalancer(ctx context.Context, db *gorm.DB, loadBalancerID int64) ([]model.Domain, error) {
	var ds []model.Domain
	err := db.WithContext(ctx).
		Where("load_balancer_id = ?", loadBalancerID).
		Order("name").
		Find(&ds).Error
	if err != nil {
		return nil, fmt.Errorf("store: list domains for load balancer %d: %w", loadBalancerID, err)
	}
	return ds, nil
}

// ListDomainsForAlias returns every domain sharing an alias — the set the
// caller must check before deciding whether deleting one leaves the alias
// orphaned (design D4).
func ListDomainsForAlias(ctx context.Context, db *gorm.DB, aliasID int64) ([]model.Domain, error) {
	var ds []model.Domain
	err := db.WithContext(ctx).
		Where("alias_id = ?", aliasID).
		Order("name").
		Find(&ds).Error
	if err != nil {
		return nil, fmt.Errorf("store: list domains for alias %d: %w", aliasID, err)
	}
	return ds, nil
}

// DeleteDomain deletes a domain by id. It does not touch the alias — the
// caller (inventory service) decides whether the alias is now orphaned and
// deletes it separately, since that also needs to cascade the DNS snapshot
// refresh (design D6).
func DeleteDomain(ctx context.Context, db *gorm.DB, id int64) error {
	res := db.WithContext(ctx).Delete(&model.Domain{}, id)
	if res.Error != nil {
		return fmt.Errorf("store: delete domain %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: delete domain %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}
