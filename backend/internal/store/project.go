package store

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

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

// GetProject loads a project by id, within the scope's account. Returns
// gorm.ErrRecordNotFound both when the project does not exist and when it belongs
// to another account: the two must be indistinguishable, or the endpoint becomes
// an oracle for what other tenants hold.
func GetProject(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.Project, error) {
	var p model.Project
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&p, "id = ?", id).Error
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

// ListProjectsWithLoadBalancers returns the scope's projects with their load
// balancers, each load balancer's nodes and each load balancer's domains preloaded
// — the object graph the console's inventory view renders.
func ListProjectsWithLoadBalancers(ctx context.Context, db *gorm.DB, sc Scope) ([]model.Project, error) {
	var ps []model.Project
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Preload("LoadBalancers.Nodes").
			Preload("LoadBalancers.Domains").
			Order("slug").
			Find(&ps).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list projects with load balancers: %w", err)
	}
	return ps, nil
}

// ListProjectsWithLoadBalancersPage is ListProjectsWithLoadBalancers narrowed by a substring
// of slug or name and cut to one page, plus the number of rows the filter
// matched in total.
//
// `base` is called twice on purpose — see the note in ListActivePage: GORM
// builds onto the statement it is handed, so a count and a select must not
// share one query object. Both calls carry the scope, so the count cannot
// disagree with the page about which rows exist.
func ListProjectsWithLoadBalancersPage(ctx context.Context, db *gorm.DB, sc Scope, query string, limit, offset int) ([]model.Project, int64, error) {
	var (
		total int64
		ps    []model.Project
	)
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			q := sc.Where(tx.Model(&model.Project{}))
			if s := strings.TrimSpace(query); s != "" {
				// ILIKE, not LIKE: an operator typing "Shop" expects "shop".
				pattern := likeEscape(s)
				q = q.Where(`slug ILIKE ? ESCAPE '\' OR name ILIKE ? ESCAPE '\'`, pattern, pattern)
			}
			return q
		}
		if err := base().Count(&total).Error; err != nil {
			return fmt.Errorf("count projects: %w", err)
		}
		return base().
			Preload("LoadBalancers.Nodes").
			Preload("LoadBalancers.Domains").
			Order("slug").
			Limit(limit).
			Offset(offset).
			Find(&ps).Error
	})
	if err != nil {
		return nil, 0, fmt.Errorf("store: list projects with load balancers: %w", err)
	}
	return ps, total, nil
}

// likeEscape wraps a user's free text in wildcards and escapes the two
// characters LIKE treats as wildcards. Without it a slug containing `_` — the
// common case — would match every row.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// DeleteProject deletes a project by id, within the scope's account. The
// load_balancers foreign key is ON DELETE RESTRICT, so this fails with a
// foreign-key-violation error when the project still has load balancers —
// refusing deletion of a non-empty parent is enforced by the database, not by an
// application-level check.
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
