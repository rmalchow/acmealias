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
// A key is of one of two kinds (design K1), and which flag is used decides
// which:
//
//   - --project makes a PROJECT key: bound to that project by a foreign key,
//     answering its challenges, and deleted with it. This is the common case and
//     what an operator has in front of them.
//   - --grant, repeatable, makes a SYSTEM key holding exactly those grants — a
//     key confined to one domain, or one holding a single action.
//
// They no longer add up. They used to, on the reasoning that authority is a
// union and never a subtraction, and that is still true of grants; what changed
// is that a project key has a LIFETIME as well as an authority. A key that dies
// with one project while carrying grants over something else is a lifetime
// nobody could reason about, so the combination is refused rather than resolved.
//
// The CLI is deliberately the one place a system key can be minted without a
// role: it writes grants directly, which is the legacy shape, and it is the tool
// somebody reaches for when the console cannot help. The API refuses it.
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
		// A PROJECT key (design K1). It used to be two grants written by hand
		// over the project; it is now a foreign key, which is what makes the key
		// die with the project — something a polymorphic grant reference could
		// never do. The authority is identical either way: authz.SubjectFromKey
		// synthesizes exactly those two challenge grants from this column.
		k.Kind = model.APIKeyKindProject
		k.ProjectID = &p.ID
	}
	for _, spec := range specs {
		g, err := parseGrant(spec)
		if err != nil {
			return err
		}
		k.Grants = append(k.Grants, g)
	}
	// --project and --grant no longer add up, and that is the one behaviour this
	// change takes away from the CLI. A key is of one kind: bound to a project,
	// or holding authority of its own. Mixing them would produce a key that dies
	// with a project while carrying grants over something else, which is a
	// lifetime nobody could reason about.
	if k.Kind == model.APIKeyKindProject && len(k.Grants) > 0 {
		return fmt.Errorf("--project makes a project key, whose authority is that project's " +
			"challenges and nothing else; use --grant alone for a key with authority of its own")
	}
	if k.Kind == "" {
		// Everything that is not a project key is a system key: an
		// account-anchored holder of authority, which is what every key was
		// before there were two kinds.
		k.Kind = model.APIKeyKindSystem
	}
	if k.Kind == model.APIKeyKindSystem && len(k.Grants) == 0 {
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
		// SOMETHING PARSES THIS LINE. development/scripts/seed.sh reads the
		// identifier back out of it with sed, because the secret is printed
		// exactly once and there is no second chance to read it — so a failed
		// parse is not a cosmetic bug, it is a minted credential nobody can ever
		// use and a development stack that comes up with no key at all.
		//
		// It happened: this line said "created api key" and became "created
		// project api key" when keys acquired kinds, and the seed's pattern was
		// anchored on the old phrase. The seed now matches on the quoted
		// identifier instead, which is the durable half. Keep `api key "<id>"`
		// intact anyway — it is the shape anything else parsing this will find.
		fmt.Fprintf(out, "created project api key %q (id %s) for project %q: %s\n", k.Identifier, k.ID, describedFor, note)
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
