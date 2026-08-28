package store

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// A project is the whole of the inventory below an account. It used to be a
// folder holding load balancers, and everything a customer configured hung off
// the load balancer; the two are one object now (change
// collapse-project-and-loadbalancer, design L1).

// CreateProject inserts a new project into the scope's account.
//
// The account is taken from the scope and not from the struct: a caller that
// filled in AccountID itself would be choosing which tenant to write into, which
// is exactly the decision Scope exists to take away from callers.
func CreateProject(ctx context.Context, db *gorm.DB, sc Scope, p *model.Project) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		p.AccountID = sc.AccountID()
		if p.ID == model.NilID {
			p.ID = model.NewID()
		}
		if err := tx.Create(p).Error; err != nil {
			return fmt.Errorf("store: create project: %w", err)
		}
		return nil
	})
}

// GetProject loads a project by id, within the scope's account, with its nodes
// preloaded — the nodes are what the DNS plan is built from, so every caller
// that reads a project by id wants them.
//
// Returns gorm.ErrRecordNotFound both when the project does not exist and when
// it belongs to another account: the two must be indistinguishable, or the
// endpoint becomes an oracle for what other tenants hold.
func GetProject(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.Project, error) {
	var p model.Project
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Preload("Nodes").First(&p, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get project %s: %w", id, err)
	}
	return &p, nil
}

// ListProjects returns the scope's projects, ordered by slug.
func ListProjects(ctx context.Context, db *gorm.DB, sc Scope) ([]model.Project, error) {
	var ps []model.Project
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Order("slug").Find(&ps).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	return ps, nil
}

// likeEscape wraps a user's free text in wildcards and escapes the two
// characters LIKE treats as wildcards. Without it a slug containing `_` — the
// common case — would match every row.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// trimmed is strings.TrimSpace, named at the one call site that needs the
// filter's empty case to read clearly.
func trimmed(s string) string { return strings.TrimSpace(s) }

// DeleteProject deletes a project by id, within the scope's account.
//
// Aliases and domains reference a project with ON DELETE RESTRICT, so this fails
// with a foreign-key violation while either still exists — refusing to delete a
// non-empty parent is enforced by the database rather than by an
// application-level check. Nodes CASCADE: they carry no state of their own and a
// project without them publishes nothing, so they are part of the thing being
// deleted rather than contents standing in its way.
func DeleteProject(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx).Delete(&model.Project{}, "id = ?", id)
		if res.Error != nil {
			return fmt.Errorf("store: delete project %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: delete project %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}
