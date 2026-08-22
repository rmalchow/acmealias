package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateLoadBalancer inserts a new load balancer.
func CreateLoadBalancer(ctx context.Context, db *gorm.DB, p *model.LoadBalancer) error {
	if err := db.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("store: create load balancer: %w", err)
	}
	return nil
}

// GetLoadBalancer loads a load balancer by id, with its nodes preloaded.
func GetLoadBalancer(ctx context.Context, db *gorm.DB, id int64) (*model.LoadBalancer, error) {
	var p model.LoadBalancer
	if err := db.WithContext(ctx).Preload("Nodes").First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("store: get load balancer %d: %w", id, err)
	}
	return &p, nil
}

// ListLoadBalancersForProject returns a project's load balancers with their nodes,
// ordered by slug.
func ListLoadBalancersForProject(ctx context.Context, db *gorm.DB, projectID int64) ([]model.LoadBalancer, error) {
	var ps []model.LoadBalancer
	err := db.WithContext(ctx).
		Preload("Nodes").
		// Domains too: the project screen shows a domain COUNT per load balancer, and without this it
		// showed 0 for every load balancer that had any.
		Preload("Domains").
		Where("project_id = ?", projectID).
		Order("slug").
		Find(&ps).Error
	if err != nil {
		return nil, fmt.Errorf("store: list load balancers for project %d: %w", projectID, err)
	}
	return ps, nil
}

// DeleteLoadBalancer deletes a load balancer by id. Domains and aliases reference the
// lb with ON DELETE RESTRICT, so this fails when either still exists.
func DeleteLoadBalancer(ctx context.Context, db *gorm.DB, id int64) error {
	res := db.WithContext(ctx).Delete(&model.LoadBalancer{}, id)
	if res.Error != nil {
		return fmt.Errorf("store: delete load balancer %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: delete load balancer %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

// CreateLBNode inserts a new node under a load balancer.
func CreateLBNode(ctx context.Context, db *gorm.DB, n *model.LBNode) error {
	if err := db.WithContext(ctx).Create(n).Error; err != nil {
		return fmt.Errorf("store: create lb node: %w", err)
	}
	return nil
}

// UpdateLBNode saves changes to an existing node (e.g. new addresses).
func UpdateLBNode(ctx context.Context, db *gorm.DB, n *model.LBNode) error {
	if err := db.WithContext(ctx).Save(n).Error; err != nil {
		return fmt.Errorf("store: update lb node %d: %w", n.ID, err)
	}
	return nil
}

// DeleteLBNode deletes a node by id. Nodes cascade with their load balancer, but can
// also be removed individually (e.g. shrinking a three-node cluster).
func DeleteLBNode(ctx context.Context, db *gorm.DB, id int64) error {
	res := db.WithContext(ctx).Delete(&model.LBNode{}, id)
	if res.Error != nil {
		return fmt.Errorf("store: delete lb node %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: delete lb node %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}
