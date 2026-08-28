package adminctl

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// GrantSpec is one grant as an operator types it: TYPE:OBJECT:ACTION, with an
// optional trailing :noinherit.
//
//	load_balancer:01a0…:challenge.publish
//	project:01a0…:challenge.publish            every load balancer in that project
//	*:*:inventory.view                          everything in the account, read-only
//
// OBJECT may be `*`, which means "every object of that type within the account"
// — anchored, never "everywhere", because a grant's identifiers are resolved
// inside the account it belongs to and there is no way to name another's.
type GrantSpec string

// parseGrant turns one spec into a row.
//
// The colon form is deliberately terse rather than a flag per part. A grant is
// four values that only mean anything together, and four flags would let an
// operator submit three of them.
func parseGrant(spec GrantSpec) (model.APIKeyGrant, error) {
	parts := strings.Split(string(spec), ":")
	if len(parts) < 3 || len(parts) > 4 {
		return model.APIKeyGrant{}, fmt.Errorf(
			"grant %q: want TYPE:OBJECT:ACTION with an optional :noinherit", spec)
	}
	objectType, object, action := parts[0], parts[1], parts[2]
	if objectType == "" || object == "" || action == "" {
		return model.APIKeyGrant{}, fmt.Errorf("grant %q: no part may be empty", spec)
	}

	g := model.APIKeyGrant{ObjectType: objectType, Action: action, Inheritable: true}
	if len(parts) == 4 {
		if parts[3] != "noinherit" {
			return model.APIKeyGrant{}, fmt.Errorf(
				"grant %q: the fourth part is `noinherit` or nothing, not %q", spec, parts[3])
		}
		g.Inheritable = false
	}
	if object != "*" {
		id, err := model.ParseID(object)
		if err != nil {
			return model.APIKeyGrant{}, fmt.Errorf("grant %q: %q is not an identifier", spec, object)
		}
		g.ObjectID = &id
	}
	return g, nil
}

// KeyCreate mints an API key and prints its secret. The secret is never stored
// — only its digest is — so this is the one and only time it is ever shown; the
// output says so plainly.
//
// A key is a subject holding grants, anchored to the account (design P8). Two
// ways to say what it may reach:
//
//   - --project, which is the common case and what an operator has in front of
//     them. It creates the two challenge grants over that project: the same
//     authority the dropped api_keys.load_balancer_id column carried.
//   - --grant, repeatable, for anything wider or narrower — a key confined to
//     one domain, or one holding a single action.
//
// Both may be given; the grants add up, because authority is a union and never a
// subtraction.
func KeyCreate(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ref, note string, expiresIn time.Duration, specs []GrantSpec) error {
	k, secret, err := apikey.New()
	if err != nil {
		return err
	}

	var describedFor string
	if ref != "" {
		p, err := resolveProjectRef(ctx, db, sc, ref)
		if err != nil {
			return err
		}
		describedFor = p.Slug
		k.Grants = append(k.Grants,
			model.APIKeyGrant{ObjectType: string(authz.TypeProject), ObjectID: &p.ID, Action: authz.ActionChallengePublish, Inheritable: true},
			model.APIKeyGrant{ObjectType: string(authz.TypeProject), ObjectID: &p.ID, Action: authz.ActionChallengeWithdraw, Inheritable: true},
		)
	}
	for _, spec := range specs {
		g, err := parseGrant(spec)
		if err != nil {
			return err
		}
		k.Grants = append(k.Grants, g)
	}
	if len(k.Grants) == 0 {
		// A key with no grants authenticates and can do nothing. That is a
		// coherent row and a useless credential, and minting one silently is
		// how an operator ends up debugging a 403 that was decided here.
		return fmt.Errorf("a key needs some authority: pass --project, or --grant TYPE:OBJECT:ACTION")
	}

	k.Note = note
	if expiresIn > 0 {
		t := time.Now().UTC().Add(expiresIn)
		k.ExpiresAt = &t
	}
	err = mutate(ctx, db, sc, func(tx *gorm.DB) ([]authz.Event, error) {
		// AllowAnyGrant, for the reason internal/adminctl/actor.go gives for its
		// actor: the CLI runs with the connection string, so whoever can run it
		// can write the row directly. A subset check here would obstruct the
		// honest path and leave the dishonest one open, and the audit trail
		// would claim a constraint that did not exist.
		if err := store.CreateAPIKey(ctx, tx, sc, k, store.AllowAnyGrant); err != nil {
			return nil, err
		}
		// k carries SecretHash; authz.Record scrubs it out by name before the
		// row is written. The secret itself was never in k to begin with.
		return []authz.Event{{
			Action: authz.ActionKeyCreate, ObjectType: authz.ObjectAPIKey,
			ObjectID: &k.ID, ObjectLabel: k.Identifier, After: k,
		}}, nil
	})
	if err != nil {
		return err
	}

	if describedFor != "" {
		fmt.Fprintf(out, "created api key %q (id %s) for load balancer %q: %s\n", k.Identifier, k.ID, describedFor, note)
	} else {
		fmt.Fprintf(out, "created api key %q (id %s): %s\n", k.Identifier, k.ID, note)
	}
	for _, g := range k.Grants {
		fmt.Fprintf(out, "  grant: %s\n", authz.GrantFromRow(g))
	}
	if k.ExpiresAt != nil {
		fmt.Fprintf(out, "expires: %s\n", k.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintln(out, "\nSecret (shown exactly once — it cannot be retrieved again, only revoked and replaced):")
	fmt.Fprintln(out, secret)
	return nil
}

// KeyList prints every key whose authority reaches a project. The secret is
// never included — there is nothing to show, only the digest is stored.
func KeyList(ctx context.Context, db *gorm.DB, sc store.Scope, out io.Writer, ref string) error {
	p, err := resolveProjectRef(ctx, db, sc, ref)
	if err != nil {
		return err
	}
	ks, err := store.ListAPIKeysForProject(ctx, db, sc, p.ID)
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
		fmt.Fprintf(out, "%s (id %s) %q [%s] %s\n", k.Identifier, k.ID, k.Note, status, k.DisplayPrefix+"…")
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
