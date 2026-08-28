package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/invitation"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// ErrAlreadyMember refuses an invitation to somebody who is already in the
// account.
//
// Refused AT ISSUE and not at acceptance, which is the opposite of how the
// blocklist is handled and for a reason worth keeping straight: membership of
// THIS account is already visible to the person inviting, through the roster, so
// telling them is not a disclosure about a third party. Deferring it to
// acceptance would just waste a message and hand the recipient a dead end.
var ErrAlreadyMember = errors.New("store: already a member of this account")

// ErrInvitationOutstanding refuses a second live invitation for one address in
// one account. The database says the same thing through invitations_pending_idx;
// this is what turns the unique violation into a sentence somebody can act on
// ("re-send the one that exists").
var ErrInvitationOutstanding = errors.New("store: an invitation to this address is already outstanding")

// IssueInvitation writes an invitation and returns it with the plaintext token,
// which is the ONLY time that token exists outside the recipient's mailbox.
//
// The caller must send it and then forget it; nothing here can recover it
// afterwards, by design.
//
// Two refusals happen here rather than in the handler because both need the same
// transaction the insert runs in: an address that already holds a membership,
// and an address that already holds an outstanding invitation. Checking either
// in a handler and inserting afterwards is a race that produces a duplicate at
// exactly the moment two administrators invite the same person.
func IssueInvitation(ctx context.Context, db *gorm.DB, sc Scope, email string, invitedBy model.ID, now time.Time) (*model.Invitation, string, error) {
	normalized, err := invitation.NormalizeEmail(email)
	if err != nil {
		return nil, "", err
	}
	token, hash, err := invitation.NewToken()
	if err != nil {
		return nil, "", err
	}
	inv := &model.Invitation{
		// The identifier is generated HERE and not left to the column default.
		// model.ID is a [16]byte, which is never nil as far as GORM is
		// concerned, so an unset one is written as the all-zero UUID rather
		// than omitted — and the second insert then collides on the primary key
		// with a message about a duplicate that reads like the pending-address
		// index and is not.
		ID:         model.NewID(),
		AccountID:  sc.AccountID(),
		Email:      normalized,
		TokenHash:  hash,
		InvitedBy:  invitedBy,
		ExpiresAt:  now.Add(invitation.Lifetime),
		LastSentAt: now,
	}
	err = sc.Run(ctx, db, func(tx *gorm.DB) error {
		already, err := addressIsMember(ctx, tx, sc, normalized)
		if err != nil {
			return err
		}
		if already {
			return fmt.Errorf("%w: %s", ErrAlreadyMember, normalized)
		}
		if err := tx.Create(inv).Error; err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s", ErrInvitationOutstanding, normalized)
			}
			return fmt.Errorf("store: issue invitation: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return inv, token, nil
}

// addressIsMember reports whether a user holding this address is already in the
// scope's account — INCLUDING a roleless one, because "already in the account"
// is about the membership row and not about what it confers. Inviting somebody
// who is in the account waiting for a role would produce a second membership and
// a very confusing roster.
//
// The users table carries no row-level security (it is global — a person exists
// before belonging to any account), so this joins from account_members, which
// does. The scope predicate on that side is what confines the answer.
func addressIsMember(ctx context.Context, db *gorm.DB, sc Scope, normalizedEmail string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).
		Table("account_members as m").
		Joins("join users u on u.id = m.user_id").
		Where("m.account_id = ? AND lower(u.email) = ?", sc.AccountID(), normalizedEmail).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("store: look for an existing membership: %w", err)
	}
	return n > 0, nil
}

// OutstandingInvitations lists what the account is waiting on, for the roster.
//
// Readable by every member without a grant, like the member list itself: knowing
// who has been asked into the room is not a privilege, and a pending invitation
// nobody can see is how an account acquires a member nobody remembers inviting.
//
// Expired rows are excluded here rather than filtered by the caller, so that
// "outstanding" means the same thing in the roster as it does at the token.
func OutstandingInvitations(ctx context.Context, db *gorm.DB, sc Scope, now time.Time) ([]model.Invitation, error) {
	var out []model.Invitation
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).
			Where("accepted_at is null and revoked_at is null and declined_at is null and expires_at > ?", now).
			Order("created_at").
			Find(&out).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: outstanding invitations: %w", err)
	}
	return out, nil
}

// RevokeInvitation marks an invitation revoked; its token stops resolving
// immediately.
//
// The row is retained rather than deleted. It is evidence of what was offered to
// whom, which is the whole reason the audit trail treats invitations as
// credentials.
func RevokeInvitation(ctx context.Context, db *gorm.DB, sc Scope, id model.ID, now time.Time) (*model.Invitation, error) {
	var inv model.Invitation
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		if err := sc.Where(tx).First(&inv, "id = ?", id).Error; err != nil {
			return err
		}
		if inv.AcceptedAt != nil || inv.RevokedAt != nil || inv.DeclinedAt != nil {
			return fmt.Errorf("%w: this invitation has already been answered", invitation.ErrInvalid)
		}
		return sc.Where(tx).Model(&model.Invitation{}).
			Where("id = ?", id).Update("revoked_at", now).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: revoke invitation %s: %w", id, err)
	}
	inv.RevokedAt = &now
	return &inv, nil
}

// ResendInvitation issues a NEW token for an existing invitation and invalidates
// the previous one, in one transaction.
//
// Invalidating the old one is the point, and it is not tidiness. Re-sending is
// what an administrator does when told "I never got it" — and the message that
// was never got is exactly the one that might have gone somewhere unintended. Two
// live tokens for one offer would also make revoking mean "revoke all of them"
// and leave the invitee holding two links that behave differently.
//
// The expiry restarts. The fourteen days are a property of the offer as made, and
// this is the offer being made again.
func ResendInvitation(ctx context.Context, db *gorm.DB, sc Scope, id model.ID, now time.Time) (*model.Invitation, string, error) {
	token, hash, err := invitation.NewToken()
	if err != nil {
		return nil, "", err
	}
	var inv model.Invitation
	err = sc.Run(ctx, db, func(tx *gorm.DB) error {
		if err := sc.Where(tx).First(&inv, "id = ?", id).Error; err != nil {
			return err
		}
		if inv.AcceptedAt != nil || inv.RevokedAt != nil || inv.DeclinedAt != nil {
			return fmt.Errorf("%w: this invitation has already been answered", invitation.ErrInvalid)
		}
		return sc.Where(tx).Model(&model.Invitation{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"token_hash":   hash,
				"expires_at":   now.Add(invitation.Lifetime),
				"last_sent_at": now,
			}).Error
	})
	if err != nil {
		return nil, "", fmt.Errorf("store: re-send invitation %s: %w", id, err)
	}
	inv.TokenHash, inv.ExpiresAt, inv.LastSentAt = hash, now.Add(invitation.Lifetime), now
	return &inv, token, nil
}

// ResolveInvitation finds the invitation a token addresses, WITHOUT consuming it.
//
// CROSS-ACCOUNT, AND DELIBERATELY SO. There is no account to scope to: the person
// resolving the token is not a member of the account yet — that is the entire
// point of an invitation — so a scoped read would return nothing, silently,
// which is what row-level security does when it has no scope. What confines this
// read is not the policy but the lookup: an unguessable 256-bit token, stored
// only as a hash.
//
// A reviewer meeting CrossAccount anywhere else in the invitation path should
// assume it is a mistake. Here and in InvitationsForAddress it is the design.
//
// Every failure returns invitation.ErrInvalid, with the real reason wrapped for
// the log only. An unknown token, an expired one, a revoked one and one already
// answered must be indistinguishable to the caller — otherwise the endpoint
// answers "was this token ever real", which is the one thing unguessability is
// meant to make unanswerable.
func ResolveInvitation(ctx context.Context, db *gorm.DB, token string, now time.Time) (*model.Invitation, *model.Account, error) {
	if token == "" {
		return nil, nil, fmt.Errorf("%w: no token", invitation.ErrInvalid)
	}
	hash := invitation.HashToken(token)
	var inv model.Invitation
	var acc model.Account
	err := CrossAccount(ctx, db, func(tx *gorm.DB) error {
		if err := tx.First(&inv, "token_hash = ?", hash).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: no invitation for that token", invitation.ErrInvalid)
			}
			return fmt.Errorf("store: resolve invitation: %w", err)
		}
		return tx.First(&acc, "id = ?", inv.AccountID).Error
	})
	if err != nil {
		return nil, nil, err
	}
	if !invitation.TokenMatches(token, inv.TokenHash) {
		// Unreachable while the lookup above is an equality on the hash, and
		// kept because it stops being unreachable the moment somebody changes
		// how the row is found.
		return nil, nil, fmt.Errorf("%w: token does not match", invitation.ErrInvalid)
	}
	if err := usableInvitation(&inv, now); err != nil {
		return nil, nil, err
	}
	return &inv, &acc, nil
}

// usableInvitation reports why an invitation cannot be answered, or nil.
//
// Expiry is evaluated HERE, at read time, rather than written by a sweep. An
// invitation is therefore dead the moment it is dead, whether or not any job has
// run — the same reasoning that keeps the metering ticks from being load-bearing.
// A sweep may reclaim expired rows on the retention schedule; nothing depends on
// it having done so.
func usableInvitation(inv *model.Invitation, now time.Time) error {
	switch {
	case inv.RevokedAt != nil:
		return fmt.Errorf("%w: revoked at %s", invitation.ErrInvalid, inv.RevokedAt.UTC())
	case inv.DeclinedAt != nil:
		return fmt.Errorf("%w: declined at %s", invitation.ErrInvalid, inv.DeclinedAt.UTC())
	case inv.AcceptedAt != nil:
		return fmt.Errorf("%w: accepted at %s", invitation.ErrInvalid, inv.AcceptedAt.UTC())
	case !inv.ExpiresAt.After(now):
		return fmt.Errorf("%w: expired at %s", invitation.ErrInvalid, inv.ExpiresAt.UTC())
	}
	return nil
}

// InvitationsForAddress lists every outstanding invitation issued to one
// address, across every account.
//
// This is the second way in, and it is not a fallback (design V11). The link in
// the mail is the path we advertise and it is the path that fails: the message
// lands in spam, a corporate gateway rewrites the URL and breaks it, somebody
// reads it on a phone and signs in on a laptop. So a person who authenticates as
// the invited address sees their invitations whether or not they ever opened
// one.
//
// IT GRANTS NOTHING THE TOKEN ROUTE DOES NOT. Both paths require authenticating
// as the invited address, which is the whole of the binding; the token only ever
// addressed the row. Being signed in as the address is strictly stronger evidence
// than holding a link sent to it.
//
// Cross-account for the same reason ResolveInvitation is: the question spans
// accounts the caller does not belong to. What confines it is the caller's own
// verified address, which the caller must have proven — never a parameter.
func InvitationsForAddress(ctx context.Context, db *gorm.DB, email string, now time.Time) ([]model.Invitation, []model.Account, error) {
	normalized, err := invitation.NormalizeEmail(email)
	if err != nil {
		return nil, nil, err
	}
	var invs []model.Invitation
	var accs []model.Account
	err = CrossAccount(ctx, db, func(tx *gorm.DB) error {
		if err := tx.
			Where("email = ? and accepted_at is null and revoked_at is null and declined_at is null and expires_at > ?",
				normalized, now).
			Order("created_at").
			Find(&invs).Error; err != nil {
			return err
		}
		if len(invs) == 0 {
			return nil
		}
		ids := make([]model.ID, 0, len(invs))
		for _, i := range invs {
			ids = append(ids, i.AccountID)
		}
		return tx.Find(&accs, "id in ?", ids).Error
	})
	if err != nil {
		return nil, nil, fmt.Errorf("store: invitations for %s: %w", normalized, err)
	}
	return invs, accs, nil
}

// AcceptInvitation creates the membership and consumes the token, in one
// transaction.
//
// The membership holds NO ROLE (account-invitations V6). The person is in the
// account and is authorised for nothing in it until an administrator assigns one
// — which is a legal state, not a broken row, and which every account-scoped
// screen has to explain rather than render as an empty account.
//
// The caller must already have established that `user` holds the invited address
// as a VERIFIED one. This function does not and cannot check that: it sees a user
// row, and "the address on this row was proven" is a fact about the identity
// provider. Passing a user who merely claims the address is how bound becomes
// bearer, so the check belongs at the edge, where the claim's provenance is
// visible.
func AcceptInvitation(ctx context.Context, db *gorm.DB, acc *model.Account, inv *model.Invitation, user *model.User, now time.Time) error {
	sc := ScopeForAccount(acc)
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		// Re-read under the lock. Between resolving the token and accepting it,
		// the invitation may have been revoked, or the same person may have
		// clicked accept twice — and both of those must lose here rather than
		// producing a second membership.
		var fresh model.Invitation
		if err := sc.Where(tx).Clauses(lockingClause()).First(&fresh, "id = ?", inv.ID).Error; err != nil {
			return fmt.Errorf("store: accept invitation: %w", err)
		}
		if err := usableInvitation(&fresh, now); err != nil {
			return err
		}
		var n int64
		if err := sc.Where(tx).Model(&model.AccountMember{}).
			Where("user_id = ?", user.ID).Count(&n).Error; err != nil {
			return fmt.Errorf("store: accept invitation: %w", err)
		}
		if n > 0 {
			// Somebody who became a member by another route between being
			// invited and accepting. Refused rather than made a second
			// membership, and the existing one is left exactly as it is.
			return fmt.Errorf("%w: %s", ErrAlreadyMember, fresh.Email)
		}
		if err := AddMemberWithoutRole(ctx, tx, sc, user.ID); err != nil {
			return err
		}
		return sc.Where(tx).Model(&model.Invitation{}).
			Where("id = ?", fresh.ID).
			Updates(map[string]any{"accepted_at": now, "accepted_by": user.ID}).Error
	})
}

// DeclineInvitation records a refusal, and CONSUMES the invitation exactly as
// accepting does.
//
// Consuming it is the part somebody would leave out. If a decline left the token
// live, "declined" would be a state a stray click could undo — and the person who
// declined would find themselves a member of an account they had refused.
//
// Declining is not a block. The account may invite the same address again, and
// invitations_pending_idx has declined_at in its predicate precisely so that it
// can: "not now" and "never" are different answers, and nothing here can tell
// which was meant.
//
// It exists at all because ignoring an invitation already expires it, and the two
// look identical to the account while meaning opposite things — never saw it
// (re-send, or telephone) versus saw it and said no (re-sending is a nuisance
// aimed at somebody who already answered). Fourteen days of silence answers
// neither question, and an administrator's most likely response to silence is to
// invite again.
func DeclineInvitation(ctx context.Context, db *gorm.DB, acc *model.Account, inv *model.Invitation, now time.Time) error {
	sc := ScopeForAccount(acc)
	return sc.Run(ctx, db, func(tx *gorm.DB) error {
		var fresh model.Invitation
		if err := sc.Where(tx).Clauses(lockingClause()).First(&fresh, "id = ?", inv.ID).Error; err != nil {
			return fmt.Errorf("store: decline invitation: %w", err)
		}
		if err := usableInvitation(&fresh, now); err != nil {
			return err
		}
		return sc.Where(tx).Model(&model.Invitation{}).
			Where("id = ?", fresh.ID).Update("declined_at", now).Error
	})
}

// InvitationLimits bounds how much mail one tenant can make this service send
// to addresses of their choosing (account-invitations V9).
//
// A zero or negative value turns one off, which is the shape every other quota
// in this service uses. Turning off PerAddressPerDay is the one that should be
// argued for in writing: it is the only limit that survives somebody creating
// accounts, and any authenticated person may create accounts freely.
type InvitationLimits struct {
	PerAccountPerHour int
	PerAddressPerDay  int
	ResendInterval    time.Duration
}

// ErrRateLimited refuses an invitation for exceeding a limit. RetryAfter says
// when the operation becomes possible again — not as a courtesy, but because
// "you are doing that too much" without a time is an instruction to retry in a
// loop, which is what the limit was trying to stop.
type ErrRateLimited struct {
	Limit      string
	RetryAfter time.Time
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("store: %s; retry after %s", e.Limit, e.RetryAfter.UTC().Format(time.RFC3339))
}

// CheckInvitationRate reports whether another invitation to this address may be
// issued from this account now.
//
// COUNTED OUT OF THE invitations TABLE ITSELF, with no counter, bucket or
// separate store. Two alternatives were rejected:
//
//   - An in-memory token bucket. Cheaper and wrong in the way that matters: it
//     is per-process and it is lost on restart, so the cross-account limit —
//     the only one an attacker cannot walk around by creating accounts — would
//     be reset by a deploy, a crash or a second replica. A limit whose state a
//     restart clears is a limit an attacker can clear.
//   - A dedicated counters table. More machinery for a number this table
//     already carries: every issued invitation is a row with created_at, and
//     that IS the record of a message having been sent.
//
// Revoked and declined rows still count. The message went out; the recipient's
// mailbox does not care what happened to the offer afterwards, and neither does
// the limit.
//
// The per-address count is CROSS-ACCOUNT and so is deliberately unscoped: the
// whole question is "how much mail has this person received from us today", and
// no one account can answer it. Nothing about another account is returned — only
// a count — so this discloses nothing to anybody.
func CheckInvitationRate(ctx context.Context, db *gorm.DB, sc Scope, email string, lim InvitationLimits, now time.Time) error {
	normalized, err := invitation.NormalizeEmail(email)
	if err != nil {
		return err
	}
	return CrossAccount(ctx, db, func(tx *gorm.DB) error {
		if lim.PerAccountPerHour > 0 {
			since := now.Add(-time.Hour)
			var n int64
			if err := tx.Model(&model.Invitation{}).
				Where("account_id = ? AND created_at > ?", sc.AccountID(), since).
				Count(&n).Error; err != nil {
				return fmt.Errorf("store: count this account's recent invitations: %w", err)
			}
			if n >= int64(lim.PerAccountPerHour) {
				return &ErrRateLimited{
					Limit:      fmt.Sprintf("this account has issued %d invitations in the last hour, which is the limit", n),
					RetryAfter: now.Add(time.Hour),
				}
			}
		}
		if lim.PerAddressPerDay > 0 {
			since := now.Add(-24 * time.Hour)
			var n int64
			if err := tx.Model(&model.Invitation{}).
				Where("email = ? AND created_at > ?", normalized, since).
				Count(&n).Error; err != nil {
				return fmt.Errorf("store: count recent invitations to an address: %w", err)
			}
			if n >= int64(lim.PerAddressPerDay) {
				// Deliberately vague about WHERE the other invitations came
				// from. The person hitting this limit may be an administrator
				// of one account who has no business learning that two other
				// accounts also invited this person today.
				return &ErrRateLimited{
					Limit:      "that address has already been invited as often as we allow in a day",
					RetryAfter: now.Add(24 * time.Hour),
				}
			}
		}
		return nil
	})
}

// CheckResendRate bounds using the re-send button as a send button.
//
// It reads last_sent_at rather than counting rows, because a re-send does not
// create one — the whole point of V7 is that one offer is one row with one live
// token.
func CheckResendRate(inv *model.Invitation, lim InvitationLimits, now time.Time) error {
	if lim.ResendInterval <= 0 {
		return nil
	}
	next := inv.LastSentAt.Add(lim.ResendInterval)
	if now.Before(next) {
		return &ErrRateLimited{
			Limit:      "this invitation was re-sent too recently",
			RetryAfter: next,
		}
	}
	return nil
}

// GetInvitation reads one invitation of the scope's account, in whatever state
// it is in.
//
// Deliberately does NOT filter to outstanding: its callers are the account's own
// operations — revoke, re-send — which need to see an already-answered
// invitation in order to refuse acting on it with a message that says why,
// rather than with a not-found that reads as "somebody deleted it".
func GetInvitation(ctx context.Context, db *gorm.DB, sc Scope, id model.ID) (*model.Invitation, error) {
	var inv model.Invitation
	err := sc.Run(ctx, db, func(tx *gorm.DB) error {
		return sc.Where(tx).First(&inv, "id = ?", id).Error
	})
	if err != nil {
		return nil, fmt.Errorf("store: read invitation %s: %w", id, err)
	}
	return &inv, nil
}
