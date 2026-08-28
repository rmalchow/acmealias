package adminctl

import (
	"context"
	"fmt"
	"io"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// ProjectDNS renders a project's DNS plan: a human-readable table, followed
// by the zone-file snippet inventory.RenderZoneSnippet produces — the same
// rendering the console's DNS plan view uses, so the CLI output is what the
// acceptance suite asserts against (task 20.3).
func ProjectDNS(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, opts inventory.PlanOptions, ref string) error {
	p, err := resolveProjectRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	plan, err := inventory.Plan(ctx, db, sc, authz.CLISubject(sc.AccountID()), p.ID, opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "DNS plan for %s (%s)\n", plan.ProjectSlug, plan.ProjectName)
	if len(plan.Records) == 0 {
		fmt.Fprintln(out, "no records")
	} else {
		fmt.Fprintf(out, "%-40s %-6s %-6s %s\n", "NAME", "TTL", "TYPE", "VALUE")
		for _, r := range plan.Records {
			fmt.Fprintf(out, "%-40s %-6d %-6s %s\n", r.Name, r.TTL, r.Type, r.Value)
		}
	}

	fmt.Fprintln(out, "\nZone snippet:")
	fmt.Fprint(out, inventory.RenderZoneSnippet(plan))
	return nil
}
