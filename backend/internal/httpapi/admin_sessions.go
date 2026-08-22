package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Active sessions, ADMIN only (design D10, identity-and-roles).
//
// This view exists because it is the only immediate lever in the system:
// roles come from the group claim read at sign-in, so removing someone from a
// Keycloak group does not disturb a session they already hold. Without
// revocation the documented "remove their access now" procedure has a step
// nobody can perform.

// sessionView never carries the session id. The id IS the credential — it is
// the whole value of the cookie — so listing it would turn a screen only an
// administrator can open into a screen that hands out everyone else's
// credentials. The handle below is what the revoke route takes instead.
type sessionView struct {
	Handle      string    `json:"handle"`
	Subject     string    `json:"subject"`
	DisplayName string    `json:"displayName"`
	Groups      []string  `json:"groups"`
	CreatedAt   time.Time `json:"createdAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Current     bool      `json:"current"`
}

// sessionHandle is a one-way name for a session: sha256 of the id, hex. It
// identifies a row for revocation and is useless as a cookie value, which is
// exactly the property the spec asks for.
func sessionHandle(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// validSessions is the set an administrator is shown and may revoke: not
// revoked, not past its absolute lifetime, not idle past the timeout. The
// same three rules oidcauth.Authenticate applies per request, so the list
// cannot show a session that would already be refused.
func (a *admin) validSessions(ctx context.Context) ([]model.Session, error) {
	now := time.Now().UTC()
	q := a.db().WithContext(ctx).
		Where("revoked_at IS NULL AND expires_at > ?", now)
	if idle := a.opts.Config.SessionIdleTimeout; idle > 0 {
		q = q.Where("last_seen_at > ?", now.Add(-idle))
	}
	var ss []model.Session
	if err := q.Order("created_at DESC").Find(&ss).Error; err != nil {
		return nil, err
	}
	return ss, nil
}

func (a *admin) listSessions(c echo.Context) error {
	ss, err := a.validSessions(c.Request().Context())
	if err != nil {
		return err
	}
	var currentID string
	if s := authz.SessionFromContext(c); s != nil {
		currentID = s.ID
	}
	out := make([]sessionView, 0, len(ss))
	for _, s := range ss {
		groups := []string(s.Groups)
		if groups == nil {
			groups = []string{}
		}
		out = append(out, sessionView{
			Handle: sessionHandle(s.ID), Subject: s.Subject, DisplayName: s.DisplayName,
			Groups: groups, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt,
			ExpiresAt: s.ExpiresAt, Current: s.ID == currentID,
		})
	}
	return c.JSON(http.StatusOK, out)
}

// revokeSession ends one session, named by its handle.
//
// ponytail: the handle is matched by hashing every currently valid session
// rather than by a stored digest column. Ceiling: O(valid sessions) per
// revoke, which for a console used by a handful of operators is a few dozen
// rows. Upgrade path: store the digest beside the id and match in SQL.
func (a *admin) revokeSession(c echo.Context) error {
	handle := c.Param("handle")
	if len(handle) != 64 {
		return fmt.Errorf("%w: session handle must be the 64-character value from the sessions listing", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	ss, err := a.validSessions(ctx)
	if err != nil {
		return err
	}
	for _, s := range ss {
		if sessionHandle(s.ID) != handle {
			continue
		}
		if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
			if err := store.RevokeSession(ctx, tx, s.ID); err != nil {
				return nil, err
			}
			return []authz.Event{revocationEvent(s.Subject, 1)}, nil
		}); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]any{"revoked": 1, "subject": s.Subject})
	}
	return echo.NewHTTPError(http.StatusNotFound, "no valid session with that handle")
}

// revokeSubjectSessions ends every session one person holds — the second half
// of "remove them from the group AND revoke their sessions". It touches only
// that subject's rows: everyone else stays signed in.
func (a *admin) revokeSubjectSessions(c echo.Context) error {
	subject := c.QueryParam("subject")
	if subject == "" {
		return fmt.Errorf("%w: DELETE /sessions needs a ?subject=… naming whose sessions to end", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	var revoked int64
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		res := tx.WithContext(ctx).
			Model(&model.Session{}).
			Where("subject = ? AND revoked_at IS NULL", subject).
			Update("revoked_at", time.Now().UTC())
		if res.Error != nil {
			return nil, res.Error
		}
		revoked = res.RowsAffected
		return []authz.Event{revocationEvent(subject, revoked)}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"revoked": revoked, "subject": subject})
}

// revocationEvent is the audit entry the spec requires: the administrator
// (supplied by mutate), the subject whose access was ended, and the time
// (supplied by authz.Record). The session id appears nowhere in it.
func revocationEvent(subject string, count int64) authz.Event {
	return authz.Event{
		Action: authz.ActionSessionRevoke, ObjectType: authz.ObjectSession, ObjectLabel: subject,
		After: map[string]any{"subject": subject, "revoked": count},
	}
}
