package store

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateProject inserts a new project.
func CreateProject(ctx context.Context, db *gorm.DB, p *model.Project) error {
	if err := db.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("store: create project: %w", err)
	}
	return nil
}

// GetProject loads a project by id. Returns gorm.ErrRecordNotFound if absent.
func GetProject(ctx context.Context, db *gorm.DB, id int64) (*model.Project, error) {
	var p model.Project
	if err := db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("store: get project %d: %w", id, err)
	}
	return &p, nil
}

// ListProjects returns every project, ordered by slug.
func ListProjects(ctx context.Context, db *gorm.DB) ([]model.Project, error) {
	var ps []model.Project
	if err := db.WithContext(ctx).Order("slug").Find(&ps).Error; err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	return ps, nil
}

// ListProjectsWithLoadBalancers returns every project with its load balancers, each load balancer's
// nodes and each load balancer's domains preloaded — the object graph the console's
// inventory view renders.
func ListProjectsWithLoadBalancers(ctx context.Context, db *gorm.DB) ([]model.Project, error) {
	var ps []model.Project
	err := db.WithContext(ctx).
		Preload("LoadBalancers.Nodes").
		Preload("LoadBalancers.Domains").
		Order("slug").
		Find(&ps).Error
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
// share one query object.
func ListProjectsWithLoadBalancersPage(ctx context.Context, db *gorm.DB, query string, limit, offset int) ([]model.Project, int64, error) {
	base := func() *gorm.DB {
		q := db.WithContext(ctx).Model(&model.Project{})
		if s := strings.TrimSpace(query); s != "" {
			// ILIKE, not LIKE: an operator typing "Shop" expects "shop".
			pattern := likeEscape(s)
			q = q.Where(`slug ILIKE ? ESCAPE '\' OR name ILIKE ? ESCAPE '\'`, pattern, pattern)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("store: count projects: %w", err)
	}

	var ps []model.Project
	err := base().
		Preload("LoadBalancers.Nodes").
		Preload("LoadBalancers.Domains").
		Order("slug").
		Limit(limit).
		Offset(offset).
		Find(&ps).Error
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

// DeleteProject deletes a project by id. The load_balancers foreign key is
// ON DELETE RESTRICT, so this fails with a foreign-key-violation error when
// the project still has load balancers — refusing deletion of a non-empty parent is
// enforced by the database, not by an application-level check.
func DeleteProject(ctx context.Context, db *gorm.DB, id int64) error {
	res := db.WithContext(ctx).Delete(&model.Project{}, id)
	if res.Error != nil {
		return fmt.Errorf("store: delete project %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: delete project %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}
