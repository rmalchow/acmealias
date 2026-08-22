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
func CreateAPIKey(ctx context.Context, db *gorm.DB, k *model.APIKey) error {
	if err := db.WithContext(ctx).Create(k).Error; err != nil {
		return fmt.Errorf("store: create api key: %w", err)
	}
	return nil
}

// ListAPIKeysForLoadBalancer returns every key on a load balancer, with their CIDR
// networks preloaded, ordered by creation time.
func ListAPIKeysForLoadBalancer(ctx context.Context, db *gorm.DB, loadBalancerID int64) ([]model.APIKey, error) {
	var ks []model.APIKey
	err := db.WithContext(ctx).
		Preload("Networks").
		Where("load_balancer_id = ?", loadBalancerID).
		Order("created_at").
		Find(&ks).Error
	if err != nil {
		return nil, fmt.Errorf("store: list api keys for load balancer %d: %w", loadBalancerID, err)
	}
	return ks, nil
}

// FindAPIKeyByIdentifier looks up a key by its public identifier (e.g.
// "k_7f3a91c4"), with its CIDR networks preloaded, for request
// authentication.
func FindAPIKeyByIdentifier(ctx context.Context, db *gorm.DB, identifier string) (*model.APIKey, error) {
	var k model.APIKey
	err := db.WithContext(ctx).
		Preload("Networks").
		Where("identifier = ?", identifier).
		First(&k).Error
	if err != nil {
		return nil, fmt.Errorf("store: find api key %q: %w", identifier, err)
	}
	return &k, nil
}

// RevokeAPIKey marks a key disabled as of now. It does not delete the row,
// so the key still identifies past audit entries and challenge attributions.
func RevokeAPIKey(ctx context.Context, db *gorm.DB, id int64) error {
	res := db.WithContext(ctx).
		Model(&model.APIKey{}).
		Where("id = ? AND disabled_at IS NULL", id).
		Update("disabled_at", time.Now().UTC())
	if res.Error != nil {
		return fmt.Errorf("store: revoke api key %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: revoke api key %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

// TouchLastUsed records that a key was just used to authenticate a request.
// Best-effort bookkeeping: a missed update loses only a display timestamp,
// never correctness, so the caller need not treat an error here as fatal.
func TouchLastUsed(ctx context.Context, db *gorm.DB, id int64) error {
	err := db.WithContext(ctx).
		Model(&model.APIKey{}).
		Where("id = ?", id).
		Update("last_used_at", time.Now().UTC()).Error
	if err != nil {
		return fmt.Errorf("store: touch last used for api key %d: %w", id, err)
	}
	return nil
}
