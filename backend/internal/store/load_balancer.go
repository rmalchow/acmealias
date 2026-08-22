package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateLoadBalancer inserts a new load balancer under a project of the scope's
// account.
//
// The account and project are stamped onto the row from the parent rather than
// taken from the caller. The composite foreign key would refuse a row whose
// account disagreed with its project's, so this is belt and braces — but it is the
// belt that produces a comprehensible error instead of a constraint violation.
func CreateLoadBalancer(ctx context.Context, db *gorm.DB, sc Scope, lb *model.LoadBalancer) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.Project
		if err := sc.Where(tx).First(&parent, "id = ?", lb.ProjectID).Error; err != nil {
			return fmt.Errorf("parent project %s: %w", lb.ProjectID, err)
		}
		lb.AccountID = parent.AccountID
		if lb.ID == model.NilID {
			lb.ID = model.NewID()
		}
		if err := tx.Create(lb).Error; err != nil {
			return fmt.Errorf("store: create load balancer: %w", err)
		}
		return nil
	})
}

// GetLoadBalancer loads a load balancer by id within the scope's account, with its
// nodes preloaded.
func GetLoadBalancer(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.LoadBalancer, error) {
	var lb model.LoadBalancer
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Preload("Nodes").First(&lb, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get load balancer %s: %w", id, err)
	}
	return &lb, nil
}

// ListLoadBalancersForProject returns a project's load balancers with their nodes,
// ordered by slug.
func ListLoadBalancersForProject(ctx context.Context, db *gorm.DB, sc Scope, projectID model.ID) ([]model.LoadBalancer, error) {
	var lbs []model.LoadBalancer
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Preload("Nodes").
			// Domains too: the project screen shows a domain COUNT per load balancer, and without this it
			// showed 0 for every load balancer that had any.
			Preload("Domains").
			Where("project_id = ?", projectID).
			Order("slug").
			Find(&lbs).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list load balancers for project %s: %w", projectID, err)
	}
	return lbs, nil
}

// DeleteLoadBalancer deletes a load balancer by id. Domains and aliases reference the
// lb with ON DELETE RESTRICT, so this fails when either still exists.
func DeleteLoadBalancer(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx).Delete(&model.LoadBalancer{}, "id = ?", id)
		if res.Error != nil {
			return fmt.Errorf("store: delete load balancer %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: delete load balancer %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// CreateLBNode inserts a new node under a load balancer of the scope's account.
func CreateLBNode(ctx context.Context, db *gorm.DB, sc Scope, n *model.LBNode) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.LoadBalancer
		if err := sc.Where(tx).First(&parent, "id = ?", n.LoadBalancerID).Error; err != nil {
			return fmt.Errorf("parent load balancer %s: %w", n.LoadBalancerID, err)
		}
		n.AccountID = parent.AccountID
		n.ProjectID = parent.ProjectID
		if n.ID == model.NilID {
			n.ID = model.NewID()
		}
		if err := tx.Create(n).Error; err != nil {
			return fmt.Errorf("store: create lb node: %w", err)
		}
		return nil
	})
}

// UpdateLBNode saves changes to an existing node (e.g. new addresses).
//
// The update is narrowed by account as well as by primary key, so a node of
// another account cannot be written even if its identifier is known.
func UpdateLBNode(ctx context.Context, db *gorm.DB, sc Scope, n *model.LBNode) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx.Model(&model.LBNode{})).
			Where("id = ?", n.ID).
			Updates(map[string]any{"label": n.Label, "ipv4": n.IPv4, "ipv6": n.IPv6})
		if res.Error != nil {
			return fmt.Errorf("store: update lb node %s: %w", n.ID, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: update lb node %s: %w", n.ID, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// GetLBNode loads one node by id within the scope's account.
func GetLBNode(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.LBNode, error) {
	var n model.LBNode
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&n, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get lb node %s: %w", id, err)
	}
	return &n, nil
}

// DeleteLBNode deletes a node by id. Nodes cascade with their load balancer, but can
// also be removed individually (e.g. shrinking a three-node cluster).
func DeleteLBNode(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx).Delete(&model.LBNode{}, "id = ?", id)
		if res.Error != nil {
			return fmt.Errorf("store: delete lb node %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: delete lb node %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}
