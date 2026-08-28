package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// CheckTenantIsolation verifies that the connection this process holds is one
// the row-level security policies actually apply to.
//
// This exists because the tenant_isolation policies are silently inert under
// two very ordinary misconfigurations, and both of them look like a working
// deployment:
//
//   - A SUPERUSER bypasses every policy unconditionally. Nothing is logged and
//     no query fails; tenant isolation simply is not enforced. The default
//     PostgreSQL role in most container images is a superuser, so this is the
//     likely accident rather than an exotic one.
//   - A role with BYPASSRLS does the same, deliberately, which is right for the
//     cross-account jobs and wrong for the process serving tenants.
//
// The owner of a table also bypasses its policies — unless the table is FORCEd,
// which the schema does, so ownership alone is safe. Superuser is not, and no amount
// of FORCE changes that.
//
// It is called at startup and refuses to continue, rather than warning. A
// warning in a log nobody reads is exactly how a deployment ends up serving
// every tenant's data through one bug, which is the outcome the whole two-
// mechanism design exists to make impossible.
func CheckTenantIsolation(ctx context.Context, db *gorm.DB) error {
	var row struct {
		Rolname      string
		Rolsuper     bool
		Rolbypassrls bool
	}
	err := db.WithContext(ctx).
		Raw("select rolname, rolsuper, rolbypassrls from pg_roles where rolname = current_user").
		Scan(&row).Error
	if err != nil {
		return fmt.Errorf("store: check tenant isolation: %w", err)
	}
	if row.Rolname == "" {
		return fmt.Errorf("store: check tenant isolation: current_user resolves to no role")
	}
	switch {
	case row.Rolsuper:
		return fmt.Errorf("store: refusing to serve as PostgreSQL superuser %q: "+
			"a superuser bypasses every row-level security policy, so tenant isolation "+
			"would not be enforced. Create an ordinary role with SELECT, INSERT, UPDATE "+
			"and DELETE on the schema and connect as that", row.Rolname)
	case row.Rolbypassrls:
		return fmt.Errorf("store: refusing to serve as role %q, which holds BYPASSRLS: "+
			"row-level security would not apply. That attribute belongs to the "+
			"cross-account jobs, not to the process serving tenants", row.Rolname)
	}
	return nil
}

// PoliciesInstalled reports whether the tenant tables carry the tenant_isolation
// policy, and that it is forced. It backs a readiness check
// and the acceptance suite: a schema migrated by an older binary would otherwise
// serve happily with no isolation at all.
func PoliciesInstalled(ctx context.Context, db *gorm.DB) error {
	tables := []string{"projects", "load_balancers", "lb_nodes", "aliases", "domains", "api_keys"}
	for _, t := range tables {
		var row struct {
			Relrowsecurity      bool
			Relforcerowsecurity bool
			Policies            int
		}
		err := db.WithContext(ctx).Raw(`
			select c.relrowsecurity, c.relforcerowsecurity,
			       (select count(*) from pg_policy p where p.polrelid = c.oid) as policies
			  from pg_class c
			  join pg_namespace n on n.oid = c.relnamespace
			 where c.relname = ? and n.nspname = current_schema()`, t).Scan(&row).Error
		if err != nil {
			return fmt.Errorf("store: check policies on %s: %w", t, err)
		}
		if !row.Relrowsecurity || !row.Relforcerowsecurity || row.Policies == 0 {
			return fmt.Errorf("store: table %s is not protected by row-level security "+
				"(enabled=%v forced=%v policies=%d) — run the migrations",
				t, row.Relrowsecurity, row.Relforcerowsecurity, row.Policies)
		}
	}
	return nil
}
