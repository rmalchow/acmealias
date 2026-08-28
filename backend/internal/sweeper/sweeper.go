// Package sweeper periodically deletes expired challenge rows.
//
// Expiry is enforced at read time: store.ActiveByAlias already filters out
// expired rows, so the zone view stops answering a value the instant it
// expires regardless of when the sweep next runs. The sweep is housekeeping
// — it reclaims table space and gives the active-challenge count and sweep
// metrics something to report — not a correctness mechanism.
package sweeper

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Run ticks every interval, deleting expired challenge rows and recording the
// count removed on m.SweepRemoved. It blocks until ctx is cancelled, so the
// caller runs it in its own goroutine and cancels ctx as part of shutdown.
//
// interactionRetention is config.Config.InteractionRetention
// (ACME_INTERACTION_RETENTION): how long a CLOSED interaction row is kept
// after withdrawal before this sweep deletes it. 0 means keep forever, and
// is the default until a human signs off on a shorter period.
func Run(ctx context.Context, db *gorm.DB, interval, interactionRetention time.Duration, m *obs.Metrics, log *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			Sweep(ctx, db, interactionRetention, m, log)
		}
	}
}

// interactionStaleAfter is how long after publication an interaction that was
// never cleaned up is treated as abandoned. Comfortably beyond any challenge
// lifetime this service issues, because closing one early would misreport a
// validation that was merely slow.
const interactionStaleAfter = 24 * time.Hour

// Sweep runs one deletion pass. It is exported so callers (and tests) can
// trigger a sweep outside of Run's ticker.
//
// interactionRetention <= 0 (the default) skips the closed-interaction
// deletion entirely — see Run's doc comment and config.Config.InteractionRetention.
func Sweep(ctx context.Context, db *gorm.DB, interactionRetention time.Duration, m *obs.Metrics, log *slog.Logger) {
	n, err := store.DeleteExpired(ctx, db)
	if err != nil {
		log.Error("sweep failed", "error", err)
		return
	}
	if m != nil {
		m.SweepRemoved.Add(float64(n))
	}

	// Same pass closes interaction rows whose challenge is long gone without a
	// /cleanup ever arriving — otherwise the console reports an abandoned
	// publication as still in flight forever. The margin over the challenge
	// lifetime is deliberate: a row is only stale once its value could not
	// possibly still be answerable.
	//
	// The three counters, and why one would have been wrong: an acme-dns
	// publication has no withdrawal in its protocol, so expiring on this timer is
	// its NORMAL ending rather than an abandonment (design N4). Those rows are
	// counted as what they are, which is also where the validated/unqueried
	// increments for that dialect come from — /cleanup is where the httpreq ones
	// are made, and an acme-dns client never calls it.
	closed, err := store.AbandonStaleInteractions(ctx, db, interactionStaleAfter)
	if err != nil {
		log.Error("stale interactions not closed", "error", err)
		return
	}
	abandoned := closed.Abandoned
	if m != nil {
		for outcome, n := range map[string]int64{
			obs.OutcomeAbandoned: closed.Abandoned,
			obs.OutcomeValidated: closed.Validated,
			obs.OutcomeUnqueried: closed.Unqueried,
		} {
			if n > 0 {
				m.ChallengeInteractions.WithLabelValues(outcome).Add(float64(n))
			}
		}
	}

	// Retention: delete CLOSED interactions past the configured age. A no-op
	// at the keep-forever default (see DeleteOldClosedInteractions). Open
	// interactions are never touched here, at any age.
	purged, err := store.DeleteOldClosedInteractions(ctx, db, interactionRetention)
	if err != nil {
		log.Error("old closed interactions not purged", "error", err)
		return
	}

	expired, err := ExpireKeys(ctx, db, m)
	if err != nil {
		log.Error("expired api keys not recorded", "error", err)
		return
	}

	lapsed, err := RecordExpiredInvitations(ctx, db, m)
	if err != nil {
		log.Error("expired invitations not recorded", "error", err)
		return
	}

	log.Debug("sweep completed", "removed", n, "abandoned", abandoned, "interactions purged", purged,
		"keys expired", expired, "invitations lapsed", lapsed)
}

// ExpireKeys disables API keys whose expiry has passed and records one
// apikey.expire entry each, attributed to the system actor.
//
// Expiry itself is already enforced at read time — an expired key stops
// authenticating the moment it lapses, whether or not this ever runs. What is
// missing without this pass is the *record*: "a key passed its expiry without
// human action" is a credential event, and nobody is around to write it down.
//
// Stamping disabled_at is what makes the pass idempotent: it is the flag that
// says this key has already been accounted for, so a restart or a second run
// writes nothing further. It changes no behaviour — the key was already
// refused for being expired — and the CLI listing reports expiry ahead of
// revocation so the stamp is not mistaken for someone's decision.
func ExpireKeys(ctx context.Context, db *gorm.DB, m *obs.Metrics) (int, error) {
	now := time.Now().UTC()
	var keys []model.APIKey
	err := db.WithContext(ctx).Preload("Networks").
		Where("disabled_at IS NULL AND expires_at IS NOT NULL AND expires_at <= ?", now).
		Find(&keys).Error
	if err != nil {
		return 0, err
	}

	recorded := 0
	for _, k := range keys {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The guard on disabled_at is re-applied here so two instances
			// sweeping at once record the expiry exactly once between them.
			res := tx.Model(&model.APIKey{}).
				Where("id = ? AND disabled_at IS NULL", k.ID).
				Update("disabled_at", k.ExpiresAt)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil
			}
			// A key has no project — it is anchored to the
			// account and reaches whatever its grants name, which may be
			// several projects or none. The audit entry therefore carries no
			// project filter, which is honest: there is no single project this
			// expiry belongs to.
			networks := make([]string, 0, len(k.Networks))
			for _, n := range k.Networks {
				networks = append(networks, n.CIDR)
			}
			recorded++
			return authz.Record(ctx, tx, m, authz.SystemActor().InAccount(k.AccountID), authz.Event{
				Action: authz.ActionKeyExpire, ObjectType: authz.ObjectAPIKey,
				ObjectID: &k.ID, ObjectLabel: k.Identifier,
				Before: map[string]any{
					"identifier": k.Identifier, "grants": grantSummary(k),
					"networks": networks, "expiresAt": k.ExpiresAt, "active": true,
				},
				After: map[string]any{
					"identifier": k.Identifier, "grants": grantSummary(k),
					"networks": networks, "expiresAt": k.ExpiresAt, "active": false,
				},
			})
		})
		if err != nil {
			return recorded, err
		}
	}
	return recorded, nil
}

// grantSummary renders what a key could do, for the audit entry recording that
// it stopped being able to. The entry has to stay readable after the key row is
// gone, so the grants are copied in as text rather than referenced.
func grantSummary(k model.APIKey) []string {
	out := make([]string, 0, len(k.Grants))
	for _, g := range k.Grants {
		out = append(out, authz.GrantFromRow(g).String())
	}
	return out
}

// invitationBatch bounds one pass. Not configuration: it caps how long a tick
// may run and how large one transaction burst gets, which is a property of the
// process rather than behaviour anybody tunes. The query is ordered by expiry,
// so a batch smaller than a backlog works through it oldest-first across ticks.
const invitationBatch = 256

// RecordExpiredInvitations writes one invitation.expire audit entry per
// invitation that has lapsed, attributed to the system actor
// (account-invitations task 9.3).
//
// LIKE ExpireKeys ABOVE, THIS CHANGES NO BEHAVIOUR. An expired invitation
// already stops working the moment it lapses — store.usableInvitation compares
// ExpiresAt to now on every read, and always did. What was missing is the
// RECORD. Every other thing that can happen to an invitation writes one:
// created, resent, revoked, accepted, declined. Lapsing was the one outcome
// that left no trace, so an account looking at its own history saw an
// invitation appear and then nothing, with no way to tell "it expired" from
// "the audit trail is incomplete".
//
// `invitation.expire` has existed in the vocabulary since the change was
// designed, with a comment saying the system actor writes it. Nothing ever did.
//
// Attribution is the system actor because nobody acted: time did. That is the
// same reason apikey.expire uses it, and it matters for reading the trail —
// an entry attributed to a person is a decision somebody made and can be asked
// about, and this is not one.
func RecordExpiredInvitations(ctx context.Context, db *gorm.DB, m *obs.Metrics) (int, error) {
	now := time.Now().UTC()
	due, err := store.DueExpiredInvitations(ctx, db, now, invitationBatch)
	if err != nil {
		return 0, err
	}

	recorded := 0
	for _, inv := range due {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The mark is claimed first and the entry written only if this
			// caller was the one that set it, so two processes sweeping
			// together record the expiry once between them.
			won, err := store.MarkInvitationExpiryRecorded(ctx, tx, inv.ID, now)
			if err != nil || !won {
				return err
			}
			recorded++
			return authz.Record(ctx, tx, m, authz.SystemActor().InAccount(inv.AccountID), authz.Event{
				Action: authz.ActionInvitationExpire, ObjectType: authz.ObjectInvitation,
				ObjectID: &inv.ID, ObjectLabel: inv.Email,
				// The address and when it lapsed, and nothing else. Never the
				// token or its hash: an audit entry is read by more people than
				// the row it describes, and this one outlives the invitation.
				Before: map[string]any{"email": inv.Email, "expiresAt": inv.ExpiresAt},
			})
		})
		if err != nil {
			return recorded, err
		}
	}
	return recorded, nil
}
