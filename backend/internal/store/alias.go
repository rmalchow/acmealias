package store

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateAlias inserts a new alias, keyed by challenge name (design D4), under a
// project of the scope's account.
// LabelTaken reports whether err is the aliases.label unique index refusing an
// insert — that is, two transactions drew or derived the same label and this one
// lost the race.
//
// It matches the CONSTRAINT and not merely the SQLSTATE, and the distinction is
// the whole point of the function. The aliases table has three unique indexes
// and only this one is worth retrying: aliases_challenge_fqdn_key means somebody
// else already owns that challenge name, which is a real conflict a retry would
// turn into an infinite loop of the same refusal, and the caller reports it as
// such. Retrying on 23505 alone would have swallowed that.
func LabelTaken(err error) bool {
	return isUniqueViolation(err) && strings.Contains(err.Error(), "aliases_label_key")
}

func CreateAlias(ctx context.Context, db *gorm.DB, sc Scope, a *model.Alias) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.Project
		if err := sc.Where(tx).First(&parent, "id = ?", a.ProjectID).Error; err != nil {
			return fmt.Errorf("parent project %s: %w", a.ProjectID, err)
		}
		a.AccountID = parent.AccountID
		if a.ID == model.NilID {
			a.ID = model.NewID()
		}
		if err := tx.Create(a).Error; err != nil {
			return fmt.Errorf("store: create alias: %w", err)
		}
		return nil
	})
}

// FindAliasByChallengeName looks up the alias owning a challenge FQDN
// (e.g. "_acme-challenge.example.com"). Returns gorm.ErrRecordNotFound when
// no project has claimed that name yet.
//
// This one deliberately takes no Scope, and the reason is worth stating because it
// looks like an omission. A challenge name is claimed globally: aliases.label and
// aliases.challenge_fqdn are unique across the whole deployment, because the label
// is a name in a zone we publish and two accounts cannot both own it. So resolving
// the spelling has to happen before any account is known.
//
// The account check happens *after*, in the caller, by comparing the resolved
// alias's AccountID against the key's anchor (design P8). Callers on the challenge
// path must do that comparison; ScopedAlias is the wrapper that does it for them.
//
// Reading globally has to be SAID, not merely intended. The row-level security
// policy on `aliases` filters it by `current_setting('app.account_id')`,
// and this query runs before any account is known — so left as a bare read it
// matches nothing at all, and every /present is refused 403 "not authorized for
// that name" because the alias appears not to exist. CrossAccount is the
// deliberately greppable escape for exactly this case, and it belongs here for the
// same reason FindAPIKeyByIdentifier carries it: both resolve the identifier that
// ESTABLISHES the account, so neither can be confined by one.
func FindAliasByChallengeName(ctx context.Context, db *gorm.DB, challengeFQDN string) (*model.Alias, error) {
	var a model.Alias
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Where("challenge_fqdn = ?", challengeFQDN).First(&a).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: find alias by challenge name %q: %w", challengeFQDN, err)
	}
	return &a, nil
}

// ScopedAlias resolves a challenge name and then confirms it belongs to the
// scope's account, returning gorm.ErrRecordNotFound when it does not — so that a
// caller cannot distinguish "another account holds this name" from "nobody does".
func ScopedAlias(ctx context.Context, db *gorm.DB, sc Scope, challengeFQDN string) (*model.Alias, error) {
	a, err := FindAliasByChallengeName(ctx, db, challengeFQDN)
	if err != nil {
		return nil, err
	}
	if a.AccountID != sc.AccountID() {
		return nil, fmt.Errorf("store: find alias by challenge name %q: %w", challengeFQDN, gorm.ErrRecordNotFound)
	}
	return a, nil
}

// ListAliasesForProject returns every alias owned by a project, with their
// domains preloaded, ordered by label.
func ListAliasesForProject(ctx context.Context, db *gorm.DB, sc Scope, projectID model.ID) ([]model.Alias, error) {
	var as []model.Alias
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Preload("Domains").
			Where("project_id = ?", projectID).
			Order("label").
			Find(&as).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list aliases for project %s: %w", projectID, err)
	}
	return as, nil
}

// ListAllAliases returns every alias in every account, for the DNS snapshot
// refresh and the zone view (design D6): every label, whether or not it currently
// has domains or challenge values.
//
// This is a cross-account read and takes no Scope by design. The nameserver
// answers for every account from one process, so the thing that builds its view
// cannot be confined to one; per design P20 this is one of the callers that
// connects as the role exempt from the row-level security policies. It is named
// ListAllAliases rather than ListAliases so that the absence of a scope reads as a
// decision at every call site.
func ListAllAliases(ctx context.Context, db *gorm.DB) ([]model.Alias, error) {
	var as []model.Alias
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Order("label").Find(&as).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list all aliases: %w", err)
	}
	return as, nil
}

// DeleteAlias deletes an alias by id. Domains reference it with
// ON DELETE RESTRICT, so this fails while any domain still points at it;
// challenges cascade.
func DeleteAlias(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx).Delete(&model.Alias{}, "id = ?", id)
		if res.Error != nil {
			return fmt.Errorf("store: delete alias %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: delete alias %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// DomainNamesForAlias lists the domain names an alias currently serves. Used to
// snapshot them into a challenge-interaction row at publication time.
func DomainNamesForAlias(ctx context.Context, db *gorm.DB, sc Scope, aliasID model.ID) ([]string, error) {
	var names []string
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx.Model(&model.Domain{})).
			Where("alias_id = ?", aliasID).
			Order("name").
			Pluck("name", &names).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: domain names for alias %s: %w", aliasID, err)
	}
	return names, nil
}

// GetAlias reads one alias by id within the scope's account.
func GetAlias(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.Alias, error) {
	var a model.Alias
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&a, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get alias %s: %w", id, err)
	}
	return &a, nil
}
