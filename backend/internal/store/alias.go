package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateAlias inserts a new alias, keyed by challenge name (design D4).
func CreateAlias(ctx context.Context, db *gorm.DB, a *model.Alias) error {
	if err := db.WithContext(ctx).Create(a).Error; err != nil {
		return fmt.Errorf("store: create alias: %w", err)
	}
	return nil
}

// FindAliasByChallengeName looks up the alias owning a challenge FQDN
// (e.g. "_acme-challenge.example.com"). Returns gorm.ErrRecordNotFound when
// no load balancer has claimed that name yet.
func FindAliasByChallengeName(ctx context.Context, db *gorm.DB, challengeFQDN string) (*model.Alias, error) {
	var a model.Alias
	err := db.WithContext(ctx).Where("challenge_fqdn = ?", challengeFQDN).First(&a).Error
	if err != nil {
		return nil, fmt.Errorf("store: find alias by challenge name %q: %w", challengeFQDN, err)
	}
	return &a, nil
}

// ListAliasesForLoadBalancer returns every alias owned by a load balancer, with their
// domains preloaded, ordered by label.
func ListAliasesForLoadBalancer(ctx context.Context, db *gorm.DB, loadBalancerID int64) ([]model.Alias, error) {
	var as []model.Alias
	err := db.WithContext(ctx).
		Preload("Domains").
		Where("load_balancer_id = ?", loadBalancerID).
		Order("label").
		Find(&as).Error
	if err != nil {
		return nil, fmt.Errorf("store: list aliases for load balancer %d: %w", loadBalancerID, err)
	}
	return as, nil
}

// ListAliases returns every alias, for the DNS snapshot refresh and the
// zone view (design D6): every label, whether or not it currently has
// domains or challenge values.
func ListAliases(ctx context.Context, db *gorm.DB) ([]model.Alias, error) {
	var as []model.Alias
	if err := db.WithContext(ctx).Order("label").Find(&as).Error; err != nil {
		return nil, fmt.Errorf("store: list aliases: %w", err)
	}
	return as, nil
}

// DeleteAlias deletes an alias by id. Domains reference it with
// ON DELETE RESTRICT, so this fails while any domain still points at it;
// challenges cascade.
func DeleteAlias(ctx context.Context, db *gorm.DB, id int64) error {
	res := db.WithContext(ctx).Delete(&model.Alias{}, id)
	if res.Error != nil {
		return fmt.Errorf("store: delete alias %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: delete alias %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

// DomainNamesForAlias lists the domain names an alias currently serves. Used to
// snapshot them into a challenge-interaction row at publication time.
func DomainNamesForAlias(ctx context.Context, db *gorm.DB, aliasID int64) ([]string, error) {
	var names []string
	err := db.WithContext(ctx).
		Model(&model.Domain{}).
		Where("alias_id = ?", aliasID).
		Order("name").
		Pluck("name", &names).Error
	if err != nil {
		return nil, fmt.Errorf("store: domain names for alias %d: %w", aliasID, err)
	}
	return names, nil
}

// ProjectIDForLoadBalancer returns the project a load balancer belongs to. Used to
// stamp a challenge-interaction row with its project at publication time: the
// row keeps no foreign keys, so without this copy a load balancer's deletion would leave
// its history attributable to nothing at all.
func ProjectIDForLoadBalancer(ctx context.Context, db *gorm.DB, loadBalancerID int64) (int64, error) {
	var projectID int64
	err := db.WithContext(ctx).
		Model(&model.LoadBalancer{}).
		Where("id = ?", loadBalancerID).
		Pluck("project_id", &projectID).Error
	if err != nil {
		return 0, fmt.Errorf("store: project of load balancer %d: %w", loadBalancerID, err)
	}
	return projectID, nil
}

// GetAlias reads one alias by id.
func GetAlias(ctx context.Context, db *gorm.DB, id int64) (*model.Alias, error) {
	var a model.Alias
	if err := db.WithContext(ctx).First(&a, id).Error; err != nil {
		return nil, fmt.Errorf("store: get alias %d: %w", id, err)
	}
	return &a, nil
}
