package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// A node is one machine of the customer's whose addresses the project's DNS plan
// publishes. The table is still `lb_nodes` and the struct is still LBNode: the
// name describes what the row is about — the customer's load balancing — rather
// than the level of our inventory it used to hang off, and renaming a table to
// remove a level the row never mentioned would be a migration bought with
// nothing.

// CreateLBNode inserts a new node under a project of the scope's account.
//
// The account is stamped onto the row from the parent rather than taken from the
// caller. The composite foreign key would refuse a row whose account disagreed
// with its project's, so this is belt and braces — but it is the belt that
// produces a comprehensible error instead of a constraint violation.
func CreateLBNode(ctx context.Context, db *gorm.DB, sc Scope, n *model.LBNode) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.Project
		if err := sc.Where(tx).First(&parent, "id = ?", n.ProjectID).Error; err != nil {
			return fmt.Errorf("parent project %s: %w", n.ProjectID, err)
		}
		n.AccountID = parent.AccountID
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

// ListLBNodesForProject returns a project's nodes, ordered by label.
func ListLBNodesForProject(ctx context.Context, db *gorm.DB, sc Scope, projectID model.ID) ([]model.LBNode, error) {
	var ns []model.LBNode
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Where("project_id = ?", projectID).Order("label").Find(&ns).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list lb nodes for project %s: %w", projectID, err)
	}
	return ns, nil
}

// DeleteLBNode deletes a node by id. Nodes cascade with their project, but can
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
