package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Applying a compiled authorization predicate to a query (design P13).
//
// The predicate arrives as columns and values rather than as SQL, so the package
// that compiles it knows nothing about tables and this one knows nothing about
// grants. What connects them is the materialized ancestors of design P3: an
// inherited grant reduces to `project_id IN (…)` on the target table rather than
// to a join or a subquery, which is why the fold produces something a single
// WHERE clause can hold.

// ACL is a compiled predicate, in the shape this package applies. It mirrors
// authz.Predicate deliberately: passing the authz type directly would make store
// import authz, and authz already imports store.
type ACL struct {
	// All means every row within the scope qualifies.
	All bool
	// Columns maps a column on the target table to the identifiers permitted
	// through it. The terms are OR-ed.
	Columns map[string][]model.ID
}

// Empty reports whether the predicate matches nothing.
//
// This is the case that must not be mistaken for "no filter". A subject holding
// no grant for an action gets an empty predicate, and a listing built from it
// returns zero rows — which is the correct answer, and the opposite of what
// dropping the filter would produce.
func (a ACL) Empty() bool { return !a.All && len(a.Columns) == 0 }

// Where narrows a query by the predicate.
func (a ACL) Where(db *gorm.DB) *gorm.DB {
	if a.All {
		return db
	}
	if a.Empty() {
		// A predicate that matches nothing has to be expressed, not skipped.
		// `WHERE false` is the honest rendering: the query still runs, still
		// counts, and still returns an empty page rather than the whole table.
		return db.Where("false")
	}
	q := db.Session(&gorm.Session{NewDB: true})
	first := true
	for col, ids := range a.Columns {
		if len(ids) == 0 {
			continue
		}
		if first {
			q = q.Where(col+" IN ?", ids)
			first = false
			continue
		}
		q = q.Or(col+" IN ?", ids)
	}
	return db.Where(q)
}

// ListProjectsACL is the ACL-gated listing (task 2.5).
//
// The count and the page run the SAME predicate, and that is not tidiness. A
// total that counted rows the caller cannot see would leak the existence of
// invisible objects through a number — undoing the property that returning
// nothing was protecting.
func ListProjectsACL(ctx context.Context, db *gorm.DB, sc Scope, acl ACL, query string, limit, offset int) ([]model.Project, int64, error) {
	var (
		total int64
		ps    []model.Project
	)
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			q := acl.Where(sc.Where(tx.Model(&model.Project{})))
			if s := trimmed(query); s != "" {
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
		return nil, 0, fmt.Errorf("store: list projects: %w", err)
	}
	return ps, total, nil
}

// GetProjectACL is ListProjectsACL narrowed by id, returning nil when the object
// does not exist AND when the caller may not see it (task 2.6).
//
// One code path for both, which is what makes the non-disclosure property
// structural: the query cannot distinguish the two cases either, so no caller
// can accidentally reveal the difference by handling them separately.
func GetProjectACL(ctx context.Context, db *gorm.DB, sc Scope, acl ACL, id model.ID) (*model.Project, error) {
	var p model.Project
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return acl.Where(sc.Where(tx)).First(&p, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get project %s: %w", id, err)
	}
	return &p, nil
}

// ListLoadBalancersACL is the ACL-gated listing for one project.
func ListLoadBalancersACL(ctx context.Context, db *gorm.DB, sc Scope, acl ACL, projectID model.ID) ([]model.LoadBalancer, error) {
	var lbs []model.LoadBalancer
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return acl.Where(sc.Where(tx)).
			Preload("Nodes").
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

// GetLoadBalancerACL is the single-row read, gated identically.
func GetLoadBalancerACL(ctx context.Context, db *gorm.DB, sc Scope, acl ACL, id model.ID) (*model.LoadBalancer, error) {
	var lb model.LoadBalancer
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return acl.Where(sc.Where(tx)).Preload("Nodes").First(&lb, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get load balancer %s: %w", id, err)
	}
	return &lb, nil
}
