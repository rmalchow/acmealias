package adminctl

import (
	"context"
	"fmt"
	"os"
	"os/user"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// cliActor identifies the operator running this process, as "user@host" — the
// closest the CLI has to a session subject. It has no groups and no roles: the
// CLI runs with database credentials, so the authority behind a CLI change is
// "had the connection string", and recording an empty role list says that
// honestly rather than implying a role nobody checked.
func cliActor() authz.Actor {
	name := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return authz.Actor{DisplayName: name + "@" + host, Source: authz.SourceCLI}
}

// mutate runs one CLI mutation and the audit entries describing it in a single
// transaction, through the same writer the console uses — so CLI rows get the
// same before/after payloads and the same secret scrubbing.
//
// A failed audit write rolls the mutation back and is returned to the
// operator, who sees a non-zero exit and no change. That is the console's
// policy, and the reason it applies here too is that the CLI is the surface
// with the *most* authority, not the least.
func mutate(ctx context.Context, db *gorm.DB, sc store.Scope, fn func(tx *gorm.DB) ([]authz.Event, error)) error {
	actor := cliActor()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		events, err := fn(tx)
		if err != nil {
			return err
		}
		for _, e := range events {
			// nil metrics, deliberately: the CLI is a short-lived process
			// with no registry and no /metrics endpoint to scrape, so a
			// counter incremented here would be discarded at exit. CLI
			// changes remain visible in the audit trail itself, which is
			// where anyone auditing them looks.
			if err := authz.Record(ctx, tx, nil, actor, e); err != nil {
				return fmt.Errorf("nothing was applied: the change could not be recorded in the audit trail: %w", err)
			}
		}
		return nil
	})
}
