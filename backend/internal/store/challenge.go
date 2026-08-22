package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Challenge records (design D6).
//
// Two rules carry the whole group, and breaking either breaks wildcard issuance in
// a way that looks like a random flake:
//
//   - Present ADDS a value at an alias; it never replaces the values already there.
//     A certificate covering example.com and *.example.com produces two
//     authorizations at ONE challenge name, and the CA expects both TXT values to
//     be answerable at the same time.
//   - Cleanup deletes only the row matching (alias_id, value) exactly. Deleting
//     every value for the name would withdraw a sibling authorization still in
//     flight.
//
// Idempotency of a repeated present is enforced by the unique (alias_id, value)
// constraint through ON CONFLICT, never by a read-then-write: two concurrent
// identical presents must leave exactly one row and neither may report an error.
//
// ponytail: expiry is compared against the application clock (time.Now) here and
// written from it in Present, so all three of Present, ActiveByAlias and
// DeleteExpired agree with each other regardless of how the database's clock is
// set. Ceiling: a second process on a badly skewed host would disagree about what
// is expired. Upgrade path is to move both the write and the comparisons to the
// server clock (now() + make_interval(secs => ?)), which only works if it is done
// in all three places at once.

// ChallengeValue is one answerable TXT value: what the DNS side needs and nothing
// more.
type ChallengeValue struct {
	Value     string
	ExpiresAt time.Time
}

// Present publishes value at aliasID for the given lifetime and returns the stored
// record. Publishing a value that is already present at that alias refreshes its
// expiry and its attribution instead of inserting a duplicate — one row either way.
//
// publishedBy is the API key identifier (e.g. "k_7f3a91c4") as plain text, not a
// foreign key: revoking or deleting the key must not delete or invalidate the
// records it published (design D6). An empty string stores NULL.
// No Scope: challenges carry no account_id and no row-level security policy
// (design P20). A challenge row is only ever reached through an alias that was
// itself resolved under a scope, so the fence sits one level up — and this is the
// certificate-issuance critical path, which the change promises not to slow down.
func Present(ctx context.Context, db *gorm.DB, aliasID model.ID, value, publishedBy string, lifetime time.Duration) (*model.Challenge, error) {
	if aliasID == model.NilID {
		return nil, fmt.Errorf("store: present: alias id is empty")
	}
	if value == "" {
		return nil, fmt.Errorf("store: present: value is empty")
	}
	if lifetime <= 0 {
		return nil, fmt.Errorf("store: present: lifetime must be positive, got %s", lifetime)
	}

	var by *string
	if publishedBy != "" {
		by = &publishedBy
	}
	now := time.Now()
	c := model.Challenge{
		AliasID:     aliasID,
		Value:       value,
		PublishedBy: by,
		CreatedAt:   now,
		ExpiresAt:   now.Add(lifetime),
	}
	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "alias_id"}, {Name: "value"}},
		// created_at is deliberately not refreshed: the record keeps reporting
		// when it was first published.
		DoUpdates: clause.AssignmentColumns([]string{"expires_at", "published_by"}),
	}).Create(&c).Error
	if err != nil {
		return nil, fmt.Errorf("store: present value at alias %s: %w", aliasID, err)
	}
	return &c, nil
}

// Cleanup withdraws exactly the one value at aliasID — never the alias's other
// values. Withdrawing a value that is not there is not an error: ACME clients
// retry cleanups, and the record may already have expired and been swept.
func Cleanup(ctx context.Context, db *gorm.DB, aliasID model.ID, value string) error {
	err := db.WithContext(ctx).
		Where("alias_id = ? AND value = ?", aliasID, value).
		Delete(&model.Challenge{}).Error
	if err != nil {
		return fmt.Errorf("store: cleanup value at alias %s: %w", aliasID, err)
	}
	return nil
}

// ActiveByAlias returns every alias label mapped to its non-expired values — the
// query behind the in-memory zone view.
//
// Every label is a key, including labels with no values: an alias with an empty
// slice is a name that exists and answers NOERROR-empty, whereas a label missing
// from the map does not exist and answers NXDOMAIN (design D7). Expired rows are
// filtered here rather than waited on, so a value stops being answerable the
// instant it expires, whenever the sweep next happens to run.
// This is a cross-account read by construction: one nameserver answers for every
// account, so the view it serves cannot be confined to one (design P20).
func ActiveByAlias(ctx context.Context, db *gorm.DB) (map[string][]ChallengeValue, error) {
	var rows []struct {
		Label     string
		Value     *string
		ExpiresAt *time.Time
	}
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Table("aliases").
			Select("aliases.label, challenges.value, challenges.expires_at").
			Joins("left join challenges on challenges.alias_id = aliases.id and challenges.expires_at > ?", time.Now()).
			Order("aliases.label, challenges.value").
			Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: active challenges by alias: %w", err)
	}

	out := make(map[string][]ChallengeValue, len(rows))
	for _, r := range rows {
		if _, ok := out[r.Label]; !ok {
			out[r.Label] = []ChallengeValue{}
		}
		if r.Value == nil || r.ExpiresAt == nil {
			continue // alias with no active values
		}
		out[r.Label] = append(out[r.Label], ChallengeValue{Value: *r.Value, ExpiresAt: *r.ExpiresAt})
	}
	return out, nil
}

// DeleteExpired removes rows whose expiry has passed and reports how many went,
// for the periodic sweep and its metric. Expired rows are already unanswerable
// before this runs; the sweep only reclaims the space.
func DeleteExpired(ctx context.Context, db *gorm.DB) (int64, error) {
	res := db.WithContext(ctx).
		Where("expires_at <= ?", time.Now()).
		Delete(&model.Challenge{})
	if res.Error != nil {
		return 0, fmt.Errorf("store: delete expired challenges: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ListActive returns the non-expired records with their alias preloaded, newest
// first — the live-challenges screen.
// ListActivePage is ListActive narrowed by a substring and cut to one page,
// plus the number of live challenges the filter matched.
//
// The filter reaches across the join to aliases, so it needs an explicit one:
// Preload runs as its own query and cannot be filtered on. The join is
// many-to-one — a challenge has at most one alias — so it cannot duplicate a
// row and the count needs no DISTINCT.
//
// `base` is called twice rather than reused: GORM builds onto the statement
// it is given, so counting and then selecting off ONE query object leaks the
// count's clauses into the select. That is not hypothetical — it produced
// "for SELECT DISTINCT, ORDER BY expressions must appear in select list"
// here before this was two calls.
// The console's view of live challenges IS scoped, even though the challenges
// table is not: it joins to aliases, which carries account_id and a policy, and
// filters on it. The join is mandatory rather than conditional for exactly that
// reason — an unjoined listing would show every tenant's live values.
func ListActivePage(ctx context.Context, db *gorm.DB, sc Scope, query string, limit, offset int) ([]model.Challenge, int64, error) {
	var (
		total int64
		cs    []model.Challenge
	)
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			q := tx.
				Model(&model.Challenge{}).
				Joins("JOIN aliases ON aliases.id = challenges.alias_id").
				Where("aliases.account_id = ?", sc.AccountID()).
				Where("challenges.expires_at > ?", time.Now())
			if s := strings.TrimSpace(query); s != "" {
				pattern := likeEscape(s)
				q = q.Where(`aliases.label ILIKE ? ESCAPE '\' OR aliases.challenge_fqdn ILIKE ? ESCAPE '\' OR challenges.published_by ILIKE ? ESCAPE '\'`,
					pattern, pattern, pattern)
			}
			return q
		}
		if err := base().Count(&total).Error; err != nil {
			return fmt.Errorf("count active challenges: %w", err)
		}
		return base().
			Preload("Alias").
			Order("challenges.created_at desc, challenges.id desc").
			Limit(limit).
			Offset(offset).
			Find(&cs).Error
	})
	if err != nil {
		return nil, 0, fmt.Errorf("store: list active challenges: %w", err)
	}
	return cs, total, nil
}

// ListActive is ListActivePage without paging or filtering, scoped the same way.
func ListActive(ctx context.Context, db *gorm.DB, sc Scope) ([]model.Challenge, error) {
	var cs []model.Challenge
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Model(&model.Challenge{}).
			Joins("JOIN aliases ON aliases.id = challenges.alias_id").
			Where("aliases.account_id = ?", sc.AccountID()).
			Where("challenges.expires_at > ?", time.Now()).
			Preload("Alias").
			Order("challenges.created_at desc, challenges.id desc").
			Find(&cs).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: list active challenges: %w", err)
	}
	return cs, nil
}

// CountActive reports how many challenge values are answerable right now, across
// every account. It backs a process-level gauge rather than any tenant-facing
// screen, which is why it is deliberately unscoped: the number describes what
// this nameserver is holding, not what any one customer has.
func CountActive(ctx context.Context, db *gorm.DB) (int64, error) {
	var n int64
	err := db.WithContext(ctx).
		Model(&model.Challenge{}).
		Where("expires_at > ?", time.Now()).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("store: count active challenges: %w", err)
	}
	return n, nil
}
