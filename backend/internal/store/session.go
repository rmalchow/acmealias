package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// CreateSession inserts a new console session (design D10). The id is the
// opaque cookie value; the caller generates it.
func CreateSession(ctx context.Context, db *gorm.DB, s *model.Session) error {
	if err := db.WithContext(ctx).Create(s).Error; err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// FindSession looks up a session by id. It does not filter on expiry or
// revocation — the caller decides what to do with a stale session (e.g.
// distinguish "expired" from "not found" in an error message).
func FindSession(ctx context.Context, db *gorm.DB, id string) (*model.Session, error) {
	var s model.Session
	if err := db.WithContext(ctx).First(&s, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("store: find session %q: %w", id, err)
	}
	return &s, nil
}

// TouchSession updates last_seen_at to now.
func TouchSession(ctx context.Context, db *gorm.DB, id string) error {
	err := db.WithContext(ctx).
		Model(&model.Session{}).
		Where("id = ?", id).
		Update("last_seen_at", time.Now().UTC()).Error
	if err != nil {
		return fmt.Errorf("store: touch session %q: %w", id, err)
	}
	return nil
}

// RevokeSession marks a session revoked as of now, by id — used for
// sign-out and for an operator killing another session.
func RevokeSession(ctx context.Context, db *gorm.DB, id string) error {
	res := db.WithContext(ctx).
		Model(&model.Session{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", time.Now().UTC())
	if res.Error != nil {
		return fmt.Errorf("store: revoke session %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: revoke session %q: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

// DeleteSession removes a session row outright.
func DeleteSession(ctx context.Context, db *gorm.DB, id string) error {
	res := db.WithContext(ctx).Delete(&model.Session{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("store: delete session %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: delete session %q: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

// DeleteExpiredSessions deletes every session whose expiry has passed,
// returning the number removed — the sweep counterpart to the challenge
// cleanup in store/challenge.go.
func DeleteExpiredSessions(ctx context.Context, db *gorm.DB) (int64, error) {
	res := db.WithContext(ctx).Where("expires_at < ?", time.Now().UTC()).Delete(&model.Session{})
	if res.Error != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// SelectAccount records which account a session is acting in (design P19).
//
// Unscoped, necessarily: this is what ESTABLISHES the scope for every later
// request on that session, so it runs before one exists. What makes it safe is
// that the caller must have proved membership first — the account is checked
// against account_members, not against anything the request asserted.
//
// Passing nil clears the selection, which is what signing out of an account
// looks like without signing out of the service.
func SelectAccount(ctx context.Context, db *gorm.DB, sessionID string, accountID *model.ID) error {
	res := db.WithContext(ctx).
		Model(&model.Session{}).
		Where("id = ? AND revoked_at IS NULL", sessionID).
		Update("selected_account_id", accountID)
	if res.Error != nil {
		return fmt.Errorf("store: select account for session: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: select account for session: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

// TakeLogoutHint returns a session's stored ID token and clears it, so the
// value is readable once.
//
// Single-use is the point. The token exists to satisfy `id_token_hint` on one
// RP-initiated logout (model.Session.LogoutHint); once that request is built
// there is no reason to keep it, and the session row is about to be revoked
// anyway.
//
// Read-then-clear in two statements rather than one UPDATE ... RETURNING,
// because Postgres RETURNING yields the NEW row — which is the NULL we just
// wrote, not the token we wanted. A CTE can be made to give the old value and
// is not worth the cleverness on a path one browser walks once.
//
// An empty return is normal, not an error: a session created before this column
// existed, or one already signed out, has no hint, and the caller then builds a
// logout request without the parameter.
func TakeLogoutHint(ctx context.Context, db *gorm.DB, id string) (string, error) {
	var s model.Session
	err := db.WithContext(ctx).Select("logout_hint").First(&s, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: read logout hint %q: %w", id, err)
	}
	if s.LogoutHint == nil || *s.LogoutHint == "" {
		return "", nil
	}
	hint := *s.LogoutHint
	if err := db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", id).Update("logout_hint", nil).Error; err != nil {
		// The hint is still returned: failing sign-out over a housekeeping
		// write would report a sign-out that DID happen as one that did not.
		return hint, nil
	}
	return hint, nil
}
