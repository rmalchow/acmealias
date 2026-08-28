package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateAPIKey inserts a new key, its digest already computed by the caller
// (design D3 — the plaintext secret is never persisted). Any CIDR networks
// on k.Networks are inserted along with it.
// implied are grants the key will hold WITHOUT a row of its own — a project
// key's challenge authority is synthesized in authz.SubjectFromKey from
// api_keys.project_id, so there is nothing in k.Grants for the subset rule to
// find. They are checked here rather than in the handler on purpose: the check
// is a required parameter precisely so a new write path cannot forget it by
// omission, and a kind whose authority the store never sees would have walked
// straight past that (design K1, task 2.1).
func CreateAPIKey(ctx context.Context, db *gorm.DB, sc Scope, k *model.APIKey, check GrantCheck, implied ...Grant) error {
	if check == nil {
		return fmt.Errorf("store: a key was created with no grant check; " +
			"pass AllowAnyGrant if that is genuinely intended and say why")
	}
	for _, g := range k.Grants {
		if err := check(Grant{
			ObjectType: g.ObjectType, ObjectID: g.ObjectID,
			Action: g.Action, Inheritable: g.Inheritable,
		}); err != nil {
			return err
		}
	}
	for _, g := range implied {
		if err := check(g); err != nil {
			return err
		}
	}
	if k.Kind == "" {
		return fmt.Errorf("store: an api key needs a kind (%q or %q)",
			model.APIKeyKindProject, model.APIKeyKindSystem)
	}
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		k.AccountID = sc.AccountID()
		if k.ID == model.NilID {
			k.ID = model.NewID()
		}
		for i := range k.Networks {
			if k.Networks[i].ID == model.NilID {
				k.Networks[i].ID = model.NewID()
			}
		}
		for i := range k.Grants {
			k.Grants[i].ID = model.NilID
			k.Grants[i].AccountID = sc.AccountID()
		}
		// Omit("Role"): the role is loaded onto the key so the response can name
		// it, and it is an EXISTING row. Without this GORM would treat it as an
		// association to save and upsert the role on every key creation — a
		// write nobody asked for, on the table whose whole job is to be the one
		// place authority is defined. Networks and Grants are not omitted,
		// deliberately: those rows belong to the key and are created with it.
		if err := tx.Omit("Role").Create(k).Error; err != nil {
			return fmt.Errorf("store: create api key: %w", err)
		}
		return nil
	})
}

// ListAPIKeysForProject returns the PROJECT keys bound to one project, with
// their networks preloaded, ordered by creation time.
//
// It used to answer a question about grants — "which keys' authority reaches
// this project" — because a key's relationship to a project was a grant and
// nothing else. A project key names its project with a real foreign key now, so
// this is a plain equality, and the keys it returns are exactly the ones that
// die with the project.
//
// What it deliberately does NOT return is a system key whose role happens to
// reach this project. Those belong to the account and outlive every project in
// it; showing them here would leave somebody asking, later and angrily, why
// deleting a project revoked their automation (design K8). They are listed in
// the account's settings, by ListSystemAPIKeys.
//
// The project's own row is read first so a caller cannot enumerate another
// account's keys by naming a project identifier it guessed.
func ListAPIKeysForProject(ctx context.Context, db *gorm.DB, sc Scope, projectID model.ID) ([]model.APIKey, error) {
	var ks []model.APIKey
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		var p model.Project
		if err := sc.Where(tx).First(&p, "id = ?", projectID).Error; err != nil {
			return err
		}
		return sc.Where(tx).
			Preload("Networks").
			Where("kind = ? and project_id = ?", model.APIKeyKindProject, p.ID).
			Order("created_at").
			Find(&ks).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list api keys for project %s: %w", projectID, err)
	}
	return ks, nil
}

// ListSystemAPIKeys returns the account's system keys — the members that are not
// people — with their role and any legacy grants, ordered by creation time.
//
// Both shapes appear here: a key holding a role, and a key from before there
// were two kinds, holding its own grants. The console renders whichever it
// finds, because a legacy key still works and hiding it would be the worst
// possible way to describe an active credential.
func ListSystemAPIKeys(ctx context.Context, db *gorm.DB, sc Scope) ([]model.APIKey, error) {
	var ks []model.APIKey
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Preload("Networks").
			Preload("Grants").
			Preload("Role").
			Where("kind = ?", model.APIKeyKindSystem).
			Order("created_at").
			Find(&ks).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list system api keys: %w", err)
	}
	return ks, nil
}

// NodeAddressesForProject returns the addresses configured on one project's
// nodes, as they are AT THIS MOMENT (design K3).
//
// This is the whole of the "computed, not copied" address limit: the union is
// resolved when a request arrives, so adding a node changes what a key permits
// with no further action and there is nothing to keep in step. Copying the list
// at creation is the obvious implementation and it reintroduces the exact
// staleness the feature exists to remove — the copy is right until a node is
// added, and then the symptom is a renewal that silently does not happen,
// surfacing weeks later as an expired certificate.
//
// Unscoped, for the reason FindAPIKeyByIdentifier is: this runs on the
// authentication path, before a scope exists, against a project the KEY names
// through a foreign key the database enforces to be in the key's own account. It
// therefore names the account in the query itself, because code under
// CrossAccount has no tenant filtering and a subquery that forgets to say which
// account it means is a subquery that reads every account's rows.
func NodeAddressesForProject(ctx context.Context, db *gorm.DB, accountID, projectID model.ID) ([]string, error) {
	var nodes []model.LBNode
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Where("project_id = ? and account_id = ?", projectID, accountID).
			Find(&nodes).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: node addresses for project %s: %w", projectID, err)
	}
	out := make([]string, 0, len(nodes)*2)
	for _, n := range nodes {
		if n.IPv4 != nil && *n.IPv4 != "" {
			out = append(out, *n.IPv4)
		}
		if n.IPv6 != nil && *n.IPv6 != "" {
			out = append(out, *n.IPv6)
		}
	}
	return out, nil
}

// NoteAddressRefusal records the address a key was most recently refused for.
//
// The refusal on the wire is the single opaque 401 design D3 requires — a caller
// must not be able to tell "wrong secret" from "wrong address" — so the console
// is where somebody finds out that their node is behind NAT and arriving from an
// address no node carries (design K5). This is what it reads.
//
// Best-effort bookkeeping, like TouchLastUsed, and unscoped for the same reason:
// it runs on the authentication path against the row that is establishing the
// account. Losing a write costs a display value and nothing else.
func NoteAddressRefusal(ctx context.Context, db *gorm.DB, id model.ID, ip string) error {
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Model(&model.APIKey{}).
			Where("id = ?", id).
			Updates(map[string]any{"last_refused_ip": ip, "last_refused_at": time.Now().UTC()}).Error
	})
	if err != nil {
		return fmt.Errorf("store: note address refusal for api key %s: %w", id, err)
	}
	return nil
}

// ListAPIKeys returns every key in the account, with networks and grants.
func ListAPIKeys(ctx context.Context, db *gorm.DB, sc Scope) ([]model.APIKey, error) {
	var ks []model.APIKey
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Preload("Networks").Preload("Grants").Order("created_at").Find(&ks).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	return ks, nil
}

// FindAPIKeyByIdentifier looks up a key by its public identifier (e.g.
// "k_7f3a91c4"), with its CIDR networks preloaded, for request
// authentication.
//
// Unscoped, necessarily: this is what *establishes* which account a request
// belongs to, so it runs before any scope exists. The key's own AccountID is the
// anchor every later query in that request is confined to, and the caller builds
// the scope from it rather than from anything the request said.
func FindAPIKeyByIdentifier(ctx context.Context, db *gorm.DB, identifier string) (*model.APIKey, error) {
	var k model.APIKey
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Preload("Networks").
			// Grants ride along with the key, so the challenge handler adds no
			// query to check a permission (constraint 2). This is the one read
			// on that path and it was already happening.
			Preload("Grants").
			// And the role, for a system key, for exactly the same reason: a
			// system key IS its role, and resolving that with a second query
			// would put a round trip on the publish path (design K6).
			Preload("Role").
			Preload("Role.Grants").
			Where("identifier = ?", identifier).
			First(&k).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: find api key %q: %w", identifier, err)
	}
	return &k, nil
}

// RevokeAPIKey marks a key disabled as of now. It does not delete the row,
// so the key still identifies past audit entries and challenge attributions.
func RevokeAPIKey(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx.Model(&model.APIKey{})).
			Where("id = ? AND disabled_at IS NULL", id).
			Update("disabled_at", time.Now().UTC())
		if res.Error != nil {
			return fmt.Errorf("store: revoke api key %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: revoke api key %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// TouchLastUsed records that a key was just used to authenticate a request.
// Best-effort bookkeeping: a missed update loses only a display timestamp,
// never correctness, so the caller need not treat an error here as fatal.
//
// Unscoped for the same reason as FindAPIKeyByIdentifier: it runs on the
// authentication path, against the row that is itself establishing the account.
func TouchLastUsed(ctx context.Context, db *gorm.DB, id model.ID) error {
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Model(&model.APIKey{}).
			Where("id = ?", id).
			Update("last_used_at", time.Now().UTC()).Error
	})
	if err != nil {
		return fmt.Errorf("store: touch last used for api key %s: %w", id, err)
	}
	return nil
}
