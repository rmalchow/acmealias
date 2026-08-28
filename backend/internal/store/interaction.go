package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/queryobs"
)

// The challenge-interaction history: one row per publication, opened by /present
// and closed by /cleanup, with DNS query observations folded in along the way.
//
// Everything here is best-effort from the caller's point of view. A history row
// that cannot be written must never fail a /present — the certificate matters,
// the record of it does not — so the challenge API logs a failure here and
// carries on. That is a deliberate asymmetry, not an oversight.

// ValueDigest is what the history stores instead of the challenge value. The
// value is a secret shared with the CA for the length of one validation; the
// digest is enough to tell two concurrent authorizations at one name apart,
// which is all it was ever needed for on this path.
func ValueDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// OpenInteraction records a publication.
//
// Idempotent in the way /present is idempotent: lego retries, and a retry of a
// value that is still live must not open a second interaction. A live row for
// the same (alias, digest) is therefore reused, and only its publication time is
// left alone — the interaction began when the CA first asked for it.
func OpenInteraction(ctx context.Context, db *gorm.DB, in *model.ChallengeInteraction) error {
	var existing model.ChallengeInteraction
	err := db.WithContext(ctx).
		Where("alias_id = ? AND value_digest = ? AND withdrawn_at IS NULL", in.AliasID, in.ValueDigest).
		First(&existing).Error
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return fmt.Errorf("store: look up live interaction: %w", err)
	}
	if err := db.WithContext(ctx).Create(in).Error; err != nil {
		return fmt.Errorf("store: open interaction: %w", err)
	}
	return nil
}

// CloseInteraction stamps the withdrawal of every live row for one value, and
// reports how many of them close as OutcomeValidated (queried at least once)
// versus OutcomeUnqueried (never queried) — the fact the caller needs to
// increment obs.Metrics.ChallengeInteractions correctly, read off the rows
// being closed rather than a second query afterwards, since a row's
// query_count can still change between the write and any later read.
//
// "Every" rather than "the": a cleanup for a value that somehow opened twice
// should leave nothing live behind, and closing rows that are already closed is
// not an error — clients retry cleanups.
func CloseInteraction(ctx context.Context, db *gorm.DB, aliasID model.ID, value string) (validated, unqueried int, err error) {
	var closed []model.ChallengeInteraction
	res := db.WithContext(ctx).
		Model(&closed).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "query_count"}}}).
		Where("alias_id = ? AND value_digest = ? AND withdrawn_at IS NULL", aliasID, ValueDigest(value)).
		Update("withdrawn_at", time.Now())
	if res.Error != nil {
		return 0, 0, fmt.Errorf("store: close interaction: %w", res.Error)
	}
	for _, row := range closed {
		if row.QueryCount > 0 {
			validated++
		} else {
			unqueried++
		}
	}
	return validated, unqueried, nil
}

// ApplyQueryObservations folds a batch of counted DNS queries into the history.
//
// Attribution is by OVERLAP with the window the counts were accumulated over,
// not by "is this row live now". That distinction is the whole correctness of
// this function: a DNS-01 validation lives for a few seconds — publish, get
// queried, clean up — while the flush runs on the zone-view interval, tens of
// seconds apart. Attributing to rows that are still live at flush time therefore
// credits almost nothing, and every successful validation would be reported as
// "never queried", which is precisely the signal this history exists to give.
// (It did exactly that before this was written as an overlap.)
//
// A row overlaps the window when it was published before the window ended and
// was not withdrawn before the window began. Both bounds come from the counter,
// never from time.Now(), so a slow or retried flush cannot shift them.
//
// One query at a name with two live values is attributed to BOTH rows on
// purpose: a certificate covering example.com and *.example.com validates two
// authorizations at one challenge name, a single query returns both TXT values,
// and both were genuinely observed by it.
//
// The alias id is resolved (obs is already keyed by id, via AliasIDsByLabel)
// but that is not the same as attribution succeeding: an alias can exist with
// no interaction row overlapping the window, e.g. a stray query after the
// interaction closed. unmatched counts those cases so the caller can report
// them separately from a genuinely unresolved label — the two mean different
// things (see obs.AttributedFalseNoInteraction).
func ApplyQueryObservations(ctx context.Context, db *gorm.DB, obs map[model.ID]queryobs.Observation, from, to time.Time) (unmatched int, err error) {
	for aliasID, o := range obs {
		if o.Count <= 0 {
			continue
		}
		first, last := o.First, o.Last
		if first.IsZero() {
			first = to
		}
		if last.IsZero() {
			last = to
		}
		res := db.WithContext(ctx).
			Model(&model.ChallengeInteraction{}).
			Where("alias_id = ? AND published_at <= ? AND (withdrawn_at IS NULL OR withdrawn_at >= ?)",
				aliasID, to, from).
			Updates(map[string]any{
				"query_count":      gorm.Expr("query_count + ?", o.Count),
				"last_queried_at":  last,
				"first_queried_at": gorm.Expr("coalesce(first_queried_at, ?)", first),
			})
		if res.Error != nil {
			return unmatched, fmt.Errorf("store: apply query observations for alias %d: %w", aliasID, res.Error)
		}
		if res.RowsAffected == 0 {
			unmatched++
		}
	}
	return unmatched, nil
}

// AliasIDsByLabel turns a batch of observations keyed by alias label into one
// keyed by alias id, dropping labels that no longer exist — a label can be
// deleted between a query being answered from the in-memory view and the
// observation being flushed, and an unmatched observation is discarded rather
// than being an error.
//
// The number dropped is returned so the caller can count it: silently discarding
// observations would hide a label-resolution bug as "the CA never queried
// anything", which is the same symptom as the failure this history reports.
func AliasIDsByLabel(ctx context.Context, db *gorm.DB, obs map[string]queryobs.Observation) (map[model.ID]queryobs.Observation, int, error) {
	if len(obs) == 0 {
		return nil, 0, nil
	}
	labels := make([]string, 0, len(obs))
	for label := range obs {
		labels = append(labels, label)
	}

	var rows []struct {
		ID    model.ID
		Label string
	}
	// CrossAccount, because this runs on the zone-view flush ticker: a batch of
	// observations covers whatever the nameserver was asked about, across every
	// account, and there is no session and so no `app.account_id` to confine it
	// to. `aliases` carries the tenant_isolation policy, so a
	// bare read here resolves NO labels at all — and the failure is silent in the
	// worst way, because every observation is then counted as "dropped" and the
	// history reports that the CA never queried anything. That is exactly the
	// misreading the comment above warns about.
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		return tx.
			Model(&model.Alias{}).
			Select("id, label").
			Where("label IN ?", labels).
			Scan(&rows).Error
	})
	if err != nil {
		return nil, 0, fmt.Errorf("store: resolve alias labels: %w", err)
	}

	out := make(map[model.ID]queryobs.Observation, len(rows))
	for _, r := range rows {
		out[r.ID] = obs[r.Label]
	}
	return out, len(obs) - len(out), nil
}

// StaleClosure is what one sweep of the open interactions did, split by what the
// rows MEAN rather than by how many were touched.
//
// The split exists because two protocols end a publication in two different
// ways, and one number for both would have made the honest end look like the
// broken one — see AbandonStaleInteractions.
type StaleClosure struct {
	// Abandoned is a client that should have withdrawn and did not.
	Abandoned int64
	// Validated is an acme-dns publication that expired on its timer having
	// been queried: the certificate authority came and looked.
	Validated int64
	// Unqueried is an acme-dns publication that expired having never been
	// queried, which is the same signal it is in any other dialect — suspect the
	// customer's CNAME.
	Unqueried int64
}

// AbandonStaleInteractions closes interactions whose challenge is gone without a
// cleanup ever arriving.
//
// A client that dies mid-validation, or one whose /cleanup never lands, leaves a
// row open forever: the sweeper deletes the expired challenge value, but nothing
// closes the interaction, so the console would report it as "live" or "pending"
// for the rest of time. That is worse than saying nothing, because the two
// states an operator acts on — still in flight, and finished without ever being
// queried — become indistinguishable from a row that has simply been forgotten.
//
// A row is stale once its publication is older than the challenge lifetime plus
// a margin: past that point the value cannot still be answerable, so whatever
// happened has happened.
//
// AN ACME-DNS ROW IS CLOSED BUT NOT MARKED ABANDONED (design N4, task 5.2).
// "Abandoned" means a client that should have withdrawn did not, and that cannot
// be true of a protocol with no withdrawal operation: acme-dns has only
// /update, so a client speaking it NEVER tells us it is finished, and expiring
// on the timer is its normal and only ending. Marking those rows abandoned would
// paint every correct acme-dns renewal red in the interaction history, and a
// column that flags healthy behaviour as a fault is a column people learn to
// ignore — taking the genuine abandonments down with it.
//
// So the distinction is made HERE, once, in the row's own flag, rather than in
// the two places that derive an outcome from it (httpapi.outcomeOf and
// reporting.outcomeOf). Both of those then keep working unchanged: an acme-dns
// row closed here reads as `validated` when it was queried and `unqueried` when
// it was not, which is exactly what it is.
func AbandonStaleInteractions(ctx context.Context, db *gorm.DB, olderThan time.Duration) (StaleClosure, error) {
	cutoff := time.Now().Add(-olderThan)
	var out StaleClosure

	// The acme-dns rows are counted before they are closed, because after the
	// update they are indistinguishable from any other withdrawn row.
	stale := func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&model.ChallengeInteraction{}).
			Where("withdrawn_at IS NULL AND published_at < ?", cutoff)
	}
	if err := stale(db.WithContext(ctx)).
		Where("fqdn_form = ? AND query_count > 0", FormAcmeDNS).
		Count(&out.Validated).Error; err != nil {
		return StaleClosure{}, fmt.Errorf("store: count expired acme-dns interactions: %w", err)
	}
	if err := stale(db.WithContext(ctx)).
		Where("fqdn_form = ? AND query_count = 0", FormAcmeDNS).
		Count(&out.Unqueried).Error; err != nil {
		return StaleClosure{}, fmt.Errorf("store: count expired acme-dns interactions: %w", err)
	}

	res := stale(db.WithContext(ctx)).
		Updates(map[string]any{
			"withdrawn_at": time.Now(),
			// The flag is the expression, so the two kinds of ending are decided
			// in the same statement that closes them and cannot drift apart.
			"abandoned": gorm.Expr("fqdn_form <> ?", FormAcmeDNS),
		})
	if res.Error != nil {
		return StaleClosure{}, fmt.Errorf("store: abandon stale interactions: %w", res.Error)
	}
	out.Abandoned = res.RowsAffected - out.Validated - out.Unqueried
	return out, nil
}

// FormAcmeDNS is the challenge_interactions.fqdn_form value written by the
// acme-dns dialect, named here because this package reads it back and
// internal/httpapi writes it. It is a string on both sides and would otherwise
// be a literal in two files that must agree.
const FormAcmeDNS = "acme-dns"

// DeleteOldClosedInteractions deletes CLOSED interactions (withdrawn_at IS NOT
// NULL) whose withdrawal is older than olderThan. An OPEN interaction is never
// touched here regardless of age — a row still open after the retention
// period is a bug worth seeing in the console, not garbage worth sweeping,
// and AbandonStaleInteractions is what closes a genuinely abandoned one.
//
// olderThan <= 0 means keep forever, and the caller (the sweeper) is expected
// to skip calling this at all in that case rather than rely on the query
// below matching nothing — see config.Config.InteractionRetention, which
// defaults to 0 for exactly this reason: challenge_interactions carries a
// customer load balancer's client_ip, and nobody has signed off on a
// retention period for it yet.
func DeleteOldClosedInteractions(ctx context.Context, db *gorm.DB, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-olderThan)
	res := db.WithContext(ctx).
		Where("withdrawn_at IS NOT NULL AND withdrawn_at < ?", cutoff).
		Delete(&model.ChallengeInteraction{})
	if res.Error != nil {
		return 0, fmt.Errorf("store: delete old closed interactions: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ListInteractionsForProject returns one page of a project's history, newest first,
// with the number of rows matching the filter.
//
// `onlyUnqueried` is the filter that earns its place: an interaction nobody ever
// queried is the signature of a customer CNAME that was never published, and
// being able to list exactly those turns a vague "renewals are failing" into a
// list of domains to go and fix.
func ListInteractionsForProject(
	ctx context.Context, db *gorm.DB, sc Scope, projectID model.ID, query string, onlyUnqueried bool, limit, offset int,
) ([]model.ChallengeInteraction, int64, error) {
	// The interaction history carries no account_id and no foreign keys, on
	// purpose: a history that disappears when its subject is deleted
	// cannot answer the question it exists for. Confinement therefore happens one
	// level up — the project named here must belong to the scope, and the
	// listing is refused as not-found when it does not.
	//
	// The consequence, stated rather than discovered: history for a project
	// that has since been deleted is not reachable through this listing, because
	// there is no row left to prove which account it belonged to. It is still in
	// the table, and reaching it is a staff-plane question rather than a tenant
	// one.
	if err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		var p model.Project
		return sc.Where(tx).First(&p, "id = ?", projectID).Error
	}); err != nil {
		return nil, 0, fmt.Errorf("store: list interactions for project %s: %w", projectID, err)
	}

	base := func() *gorm.DB {
		q := db.WithContext(ctx).
			Model(&model.ChallengeInteraction{}).
			Where("project_id = ?", projectID)
		if s := strings.TrimSpace(query); s != "" {
			pattern := likeEscape(s)
			q = q.Where(
				`alias_label ILIKE ? ESCAPE '\' OR challenge_fqdn ILIKE ? ESCAPE '\' `+
					`OR key_id ILIKE ? ESCAPE '\' OR host(client_ip) ILIKE ? ESCAPE '\' `+
					`OR array_to_string(domain_names, ',') ILIKE ? ESCAPE '\'`,
				pattern, pattern, pattern, pattern, pattern)
		}
		if onlyUnqueried {
			q = q.Where("query_count = 0")
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("store: count interactions: %w", err)
	}

	var out []model.ChallengeInteraction
	err := base().
		Order("published_at desc, id desc").
		Limit(limit).
		Offset(offset).
		Find(&out).Error
	if err != nil {
		return nil, 0, fmt.Errorf("store: list interactions: %w", err)
	}
	return out, total, nil
}
