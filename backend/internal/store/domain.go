package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateDomain inserts a new domain, pointing at its (already resolved) alias.
// Find-or-create-the-alias and wildcard-conflict logic belongs to the
// inventory service (design D4), not here — this is the plain insert it
// calls once the alias id is known.
func CreateDomain(ctx context.Context, db *gorm.DB, sc Scope, d *model.Domain) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var parent model.Alias
		if err := sc.Where(tx).First(&parent, "id = ?", d.AliasID).Error; err != nil {
			return fmt.Errorf("parent alias %s: %w", d.AliasID, err)
		}
		d.AccountID = parent.AccountID
		d.ProjectID = parent.ProjectID
		if d.ID == model.NilID {
			d.ID = model.NewID()
		}
		// A domain joining an alias inherits that alias's activation stamps.
		//
		// The delegation is ONE record (domain-activation A1): adding
		// *.example.com beside an activated example.com joins the alias whose
		// CNAME the customer already published, and that published record is
		// exactly as observed for the new row as for the old one. Leaving the
		// new row NULL would give one fact two derivations that disagree — the
		// gate asks the ALIAS (store.DelegationSeen) and would publish for it,
		// while the console reads the ROW and would show "waiting for
		// delegation" and offer a check for something already working. A1 exists
		// to prevent exactly that, so the invariant is stated here instead: every
		// domain on an alias carries the same two stamps, at insert as well as
		// after each of the worker's writes, which are keyed on alias_id too.
		//
		// Rejected: making the console derive from the alias. It would have
		// fixed the display and left the rows disagreeing in the table, which is
		// where the next reader looks.
		var stamps struct {
			LastCheckedAt *time.Time `gorm:"column:last_checked_at"`
			LastSuccessAt *time.Time `gorm:"column:last_success_at"`
		}
		if err := sc.Where(tx).Model(&model.Domain{}).
			Select("max(last_checked_at) as last_checked_at, max(last_success_at) as last_success_at").
			Where("alias_id = ?", d.AliasID).
			Scan(&stamps).Error; err != nil {
			return fmt.Errorf("store: activation stamps on alias %s: %w", d.AliasID, err)
		}
		d.LastCheckedAt, d.LastSuccessAt = stamps.LastCheckedAt, stamps.LastSuccessAt
		if err := tx.Create(d).Error; err != nil {
			return fmt.Errorf("store: create domain: %w", err)
		}
		return nil
	})
}

// GetDomain loads a domain by id within the scope's account.
func GetDomain(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.Domain, error) {
	var d model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&d, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: get domain %s: %w", id, err)
	}
	return &d, nil
}

// FindDomainByName looks up a domain by its exact normalized name, within the
// scope's account.
//
// A name is unique per account and not globally (design P8): two accounts may both
// hold example.com, and which of them can actually validate it is settled by DNS
// delegation rather than by this table. So this must be scoped — an unscoped
// lookup would find another tenant's row and report the name as taken.
func FindDomainByName(ctx context.Context, db *gorm.DB, sc Scope, name string) (*model.Domain, error) {
	var d model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Where("name = ?", name).First(&d).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: find domain %q: %w", name, err)
	}
	return &d, nil
}

// ListDomainsForProject returns every domain in a project, ordered by name.
func ListDomainsForProject(ctx context.Context, db *gorm.DB, sc Scope, projectID model.ID) ([]model.Domain, error) {
	var ds []model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Where("project_id = ?", projectID).
			Order("name").
			Find(&ds).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list domains for project %s: %w", projectID, err)
	}
	return ds, nil
}

// ListDomainsForAlias returns every domain sharing an alias — the set the
// caller must check before deciding whether deleting one leaves the alias
// orphaned (design D4).
func ListDomainsForAlias(ctx context.Context, db *gorm.DB, sc Scope, aliasID model.ID) ([]model.Domain, error) {
	var ds []model.Domain
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Where("alias_id = ?", aliasID).
			Order("name").
			Find(&ds).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list domains for alias %s: %w", aliasID, err)
	}
	return ds, nil
}

// DeleteDomain deletes a domain by id. It does not touch the alias — the
// caller (inventory service) decides whether the alias is now orphaned and
// deletes it separately, since that also needs to cascade the DNS snapshot
// refresh (design D6).
func DeleteDomain(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) error {
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		res := sc.Where(tx).Delete(&model.Domain{}, "id = ?", id)
		if res.Error != nil {
			return fmt.Errorf("store: delete domain %s: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: delete domain %s: %w", id, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// --- activation (domain-activation A1) --------------------------------------

// DueDelegation is one delegation the activation worker should look at: the
// alias a customer's `_acme-challenge` CNAME has to point at, and the name it
// has to be published under.
//
// The unit is the ALIAS and not the domain, because the delegation is one
// record and a wildcard shares it with its apex (A1). Asking once and applying
// the answer to every domain on the alias is what makes "an observation of that
// delegation activates both" true by construction rather than by two lookups
// that could disagree.
type DueDelegation struct {
	AliasID       model.ID `gorm:"column:alias_id"`
	ChallengeFQDN string   `gorm:"column:challenge_fqdn"`
	Label         string   `gorm:"column:label"`
}

// DueDelegations returns, at most limit of them, the delegations carrying at
// least one domain whose next check is due.
//
// The two cadences are derived from last_success_at rather than stored (A1):
// a domain that has never been seen is due `fresh` after the last look, one that
// has is due `settled` after it. A domain that has never been looked at at all
// is due immediately, which is what `last_checked_at is null` says and what the
// `nulls first` half of domains_activation_due_idx is for.
//
// Ordering puts the delegations somebody is waiting on first: a group holding
// an unactivated domain outranks one where every domain is settled, and within
// each the longest-unlooked-at comes first. Without that, a deployment with more
// settled domains than a batch can hold would spend every tick re-checking
// domains nothing depends on while a customer watches a new one stay offline.
//
// The aggregates are exact rather than approximate because every domain on an
// alias carries the SAME two stamps: the two writes below are keyed on alias_id,
// and CreateDomain gives a joining domain its alias's stamps. Without that
// invariant `min(last_checked_at)` would quietly mis-sort a mixed group, since
// min() ignores NULLs and would report the older stamp rather than the missing
// one.
//
// It reads across accounts by definition — the worker runs on a ticker with no
// session and therefore no app.account_id, and `domains` and `aliases` both
// carry the tenant_isolation policy, so a bare statement here would select from
// an empty view of both and find nothing at all, for ever, without erroring.
func DueDelegations(ctx context.Context, db *gorm.DB, now time.Time, fresh, settled time.Duration, limit int) ([]DueDelegation, error) {
	var out []DueDelegation
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.Raw(`
		select a.id as alias_id, a.challenge_fqdn, a.label
		  from aliases a
		  join domains d on d.alias_id = a.id
		 where d.last_checked_at is null
		    or (d.last_success_at is     null and d.last_checked_at < ?)
		    or (d.last_success_at is not null and d.last_checked_at < ?)
		 group by a.id, a.challenge_fqdn, a.label
		 order by bool_or(d.last_success_at is null) desc,
		          min(d.last_checked_at) nulls first
		 limit ?`,
			now.Add(-fresh), now.Add(-settled), limit).Scan(&out).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: delegations due an activation check: %w", err)
	}
	return out, nil
}

// RecordDelegationSeen stamps both timestamps on every domain sharing the
// alias: we looked, and we saw it. Setting last_success_at is the whole of
// activation — there is no flag to raise beside it (A1).
//
// Idempotent by shape rather than by a guard (A5): two instances checking the
// same delegation in the same minute write the same two values, and the second
// write is indistinguishable from the first having happened twice.
func RecordDelegationSeen(ctx context.Context, db *gorm.DB, aliasID model.ID, at time.Time) (int64, error) {
	return stampDelegation(ctx, db, aliasID,
		`update domains set last_checked_at = ?, last_success_at = ? where alias_id = ?`, at, at, aliasID)
}

// RecordDelegationMissing stamps only last_checked_at.
//
// IT MUST NEVER TOUCH last_success_at, and that is the point of the whole
// change (A3): activation is one-way. A customer migrating DNS providers or
// rebuilding a zone produces a delegation that blips, and clearing the
// activation on that would stop their renewals silently for weeks. Getting in
// requires proof; staying in is not revocable by a lookup.
//
// The stamp is written for a lookup that FAILED as well as for one that came
// back "no such record", because last_checked_at means "when we last looked"
// and we did. Skipping it for an inconclusive answer would also starve the
// queue: an unresolvable name keeps its null-or-ancient last_checked_at, sorts
// to the front of the next batch, and crowds out every domain behind it for
// ever.
func RecordDelegationMissing(ctx context.Context, db *gorm.DB, aliasID model.ID, at time.Time) (int64, error) {
	return stampDelegation(ctx, db, aliasID,
		`update domains set last_checked_at = ? where alias_id = ?`, at, aliasID)
}

// stampDelegation runs one of the two updates above cross-account and reports
// how many domains rode on the delegation — see DueDelegations for why the
// policies have to be lifted.
func stampDelegation(ctx context.Context, db *gorm.DB, aliasID model.ID, sql string, args ...any) (int64, error) {
	var n int64
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		res := tx.Exec(sql, args...)
		n = res.RowsAffected
		return res.Error
	})
	if err != nil {
		return 0, fmt.Errorf("store: record activation check for alias %s: %w", aliasID, err)
	}
	return n, nil
}

// DelegationSeen reports whether the delegation this alias stands for has ever
// been observed — which is the whole of "is this domain active" (A1). There is
// no flag to read beside last_success_at, and no counter to interpret.
//
// It is asked of the ALIAS rather than of one domain because the delegation is
// one record: `example.com` and `*.example.com` share a challenge name, and one
// observation activates both (A1). An alias carrying no domains at all answers
// false — nothing there is active, because there is nothing that could have
// been activated, and offline-until-seen is the direction this change picked.
//
// This is the gate's ONLY question, and it is a table read. The lookup that
// produced the answer happened out of band, in the activation worker, possibly
// minutes ago. /present must never resolve a name: it is called by lego in the
// middle of an issuance, and making certificate issuance depend on a third
// party's resolver being reachable is a far worse failure than the abuse the
// gate prevents.
//
// Scoped, unlike the worker's three queries above. By the time the challenge
// path asks this, the alias has been resolved and compared against the key's
// own account, so there is an account to read within — and lifting tenant
// isolation on the hot path to save a predicate would be widening the one query
// that runs on every renewal in the deployment.
func DelegationSeen(ctx context.Context, db *gorm.DB, sc Scope, aliasID model.ID) (bool, error) {
	var n int64
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).Model(&model.Domain{}).
			Where("alias_id = ? and last_success_at is not null", aliasID).
			Count(&n).Error
	})
	if err != nil {
		return false, fmt.Errorf("store: activation state of alias %s: %w", aliasID, err)
	}
	return n > 0, nil
}
