package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateAPIKey inserts a new key, its digest already computed by the caller
// (design D3 — the plaintext secret is never persisted). Any CIDR networks
// on k.Networks are inserted along with it.
func CreateAPIKey(ctx context.Context, db *gorm.DB, sc Scope, k *model.APIKey) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.LoadBalancer
		if err := sc.Where(tx).First(&parent, "id = ?", k.LoadBalancerID).Error; err != nil {
			return fmt.Errorf("parent load balancer %s: %w", k.LoadBalancerID, err)
		}
		k.AccountID = parent.AccountID
		k.ProjectID = parent.ProjectID
		if k.ID == model.NilID {
			k.ID = model.NewID()
		}
		for i := range k.Networks {
			if k.Networks[i].ID == model.NilID {
				k.Networks[i].ID = model.NewID()
			}
		}
		if err := tx.Create(k).Error; err != nil {
			return fmt.Errorf("store: create api key: %w", err)
		}
		return nil
	})
}

// ListAPIKeysForLoadBalancer returns every key on a load balancer, with their CIDR
// networks preloaded, ordered by creation time.
func ListAPIKeysForLoadBalancer(ctx context.Context, db *gorm.DB, sc Scope, loadBalancerID model.ID) ([]model.APIKey, error) {
	var ks []model.APIKey
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Preload("Networks").
			Where("load_balancer_id = ?", loadBalancerID).
			Order("created_at").
			Find(&ks).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list api keys for load balancer %s: %w", loadBalancerID, err)
	}
	return ks, nil
}

// FindAPIKeyByIdentifier looks up a key by its public identifier (e.g.
// "k_7f3a91c4"), with its CIDR networks preloaded, for request
// authentication.
//
// Unscoped, necessarily: this is what *establishes* which account a request
// belongs to, so it runs before any scope exists. The key's own AccountID is the
// anchor every later query in that request is confined to, and the caller builds
// the scope from it rather than from anything the request said.
func FindAPIKeyByIdentifier(ctx context.Context, db *gorm.DB, identifier string) (*model.APIKey, error) {
	var k model.APIKey
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Preload("Networks").
			Where("identifier = ?", identifier).
			First(&k).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: find api key %q: %w", identifier, err)
	}
	return &k, nil
}

// RevokeAPIKey marks a key disabled as of now. It does not delete the row,
// so the key still identifies past audit entries and challenge attributions.
func RevokeAPIKey(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx.Model(&model.APIKey{})).
			Where("id = ? AND disabled_at IS NULL", id).
			Update("disabled_at", time.Now().UTC())
		if res.Error != nil {
			return fmt.Errorf("store: revoke api key %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: revoke api key %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// TouchLastUsed records that a key was just used to authenticate a request.
// Best-effort bookkeeping: a missed update loses only a display timestamp,
// never correctness, so the caller need not treat an error here as fatal.
//
// Unscoped for the same reason as FindAPIKeyByIdentifier: it runs on the
// authentication path, against the row that is itself establishing the account.
func TouchLastUsed(ctx context.Context, db *gorm.DB, id model.ID) error {
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Model(&model.APIKey{}).
			Where("id = ?", id).
			Update("last_used_at", time.Now().UTC()).Error
	})
	if err != nil {
		return fmt.Errorf("store: touch last used for api key %s: %w", id, err)
	}
	return nil
}
