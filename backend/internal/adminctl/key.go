package adminctl

import (
	"context"
	"fmt"
	"io"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// KeyCreate mints an API key for a load balancer and prints its secret. The
// secret is never stored — only its digest is — so this is the one and only
// time it is ever shown; the output says so plainly.
func KeyCreate(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ref, note string, expiresIn time.Duration) error {
	lb, err := resolveLoadBalancerRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	k, secret, err := apikey.New()
	if err != nil {
		return err
	}
	// The CLI still takes --loadbalancer, because that is what an operator has
	// in front of them. What it creates is a grant naming that load balancer,
	// which is the same authority the dropped column carried and is now sayable
	// alongside wider ones.
	k.Grants = []model.APIKeyGrant{
		{ObjectType: string(authz.TypeLoadBalancer), ObjectID: &lb.ID, Action: "challenge.publish", Inheritable: true},
		{ObjectType: string(authz.TypeLoadBalancer), ObjectID: &lb.ID, Action: "challenge.withdraw", Inheritable: true},
	}
	k.Note = note
	if expiresIn > 0 {
		t := time.Now().UTC().Add(expiresIn)
		k.ExpiresAt = &t
	}
	err = mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.CreateAPIKey(ctx, tx, sc, k); err != nil {
			return nil, err
		}
		// k carries SecretHash; authz.Record scrubs it out by name before the
		// row is written. The secret itself was never in k to begin with.
		return []authz.Event{{
			Action: authz.ActionKeyCreate, ObjectType: authz.ObjectAPIKey,
			ObjectID: &k.ID, ObjectLabel: k.Identifier, ProjectID: &lb.ProjectID, After: k,
		}}, nil
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "created api key %q (id %d) for load balancer %q: %s\n", k.Identifier, k.ID, lb.Slug, note)
	if k.ExpiresAt != nil {
		fmt.Fprintf(out, "expires: %s\n", k.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintln(out, "\nSecret (shown exactly once — it cannot be retrieved again, only revoked and replaced):")
	fmt.Fprintln(out, secret)
	return nil
}

// KeyList prints every key on a load balancer. The secret is never included —
// there is nothing to show, only the digest is stored.
func KeyList(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ref string) error {
	lb, err := resolveLoadBalancerRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	ks, err := store.ListAPIKeysForLoadBalancer(ctx, db, sc, lb.ID)
	if err != nil {
		return err
	}
	if len(ks) == 0 {
		fmt.Fprintln(out, "no api keys")
		return nil
	}
	for _, k := range ks {
		// Expiry is reported before revocation: the expiry sweep disables a
		// key by stamping disabled_at, so checking that first would report
		// every lapsed key as "revoked" by someone.
		status := "active"
		if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
			status = "expired at " + k.ExpiresAt.Format(time.RFC3339)
		} else if k.DisabledAt != nil {
			status = "revoked at " + k.DisabledAt.Format(time.RFC3339)
		}
		fmt.Fprintf(out, "%s (id %d) %q [%s] %s\n", k.Identifier, k.ID, k.Note, status, k.DisplayPrefix+"…")
	}
	return nil
}

// KeyRevoke disables a key by id. Revocation, not deletion: the row still
// identifies past audit entries and challenge attributions.
func KeyRevoke(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, id model.ID) error {
	err := mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		var k model.APIKey
		if err := tx.WithContext(ctx).Preload("Networks").First(&k, "id = ?", id).Error; err != nil {
			return nil, fmt.Errorf("get api key %s: %w", id, err)
		}
		if err := store.RevokeAPIKey(ctx, tx, sc, id); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionKeyRevoke, ObjectType: authz.ObjectAPIKey,
			ObjectID: &id, ObjectLabel: k.Identifier, Before: k,
		}}, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "revoked api key %s\n", id)
	return nil
}
