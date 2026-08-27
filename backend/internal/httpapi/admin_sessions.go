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

// A person's own active sessions, and the ability to end them.
//
// This used to enumerate and revoke EVERYBODY's sessions, gated on the global
// ADMIN role. Design C10 removed the global roles, and nothing per-account can
// replace them here: a session belongs to a person rather than to an account, so
// an account administrator holding power over one would hold power over somebody
// who may not be in their account at all — and the listing would disclose every
// other tenant's subjects and groups, which is the one class of bug store.Scope
// exists to make unrepresentable.
//
// They also resolve NO account, which is not a detail: they mutate through
// a.mutateForPerson rather than a.mutate. A session is not tenant data, and
// "sign me out everywhere" has to work from the picker — the state every sign-in
// now begins in (task 2.2) — and from inside a suspended account, which are the
// two states a.mutate refuses with 409 and 402. A stolen laptop is exactly the
// moment somebody is sitting at the picker.
//
// So the routes answer for the caller and nobody else. What is genuinely lost is
// the operator-facing half of "remove their access now": ending somebody else's
// session is a staff power, it needs the staff plane's separate authentication
// to be safe, and /api/admin/** is where it will live. The tenant-facing half
// did not need this view — a membership removed takes effect when the person
// next selects the account (design P19).

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

// validSessions is the set one person is shown and may revoke: their own, not
// revoked, not past its absolute lifetime, not idle past the timeout. The
// same three rules oidcauth.Authenticate applies per request, so the list
// cannot show a session that would already be refused.
//
// The subject comes from the authenticated session rather than from a query
// parameter, which is what makes this route safe without a permission check:
// there is no request that names somebody else.
func (a *admin) validSessions(ctx context.Context, subject string) ([]model.Session, error) {
	now := time.Now().UTC()
	q := a.db().WithContext(ctx).
		Where("subject = ? AND revoked_at IS NULL AND expires_at > ?", subject, now)
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
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	ss, err := a.validSessions(c.Request().Context(), sess.Subject)
	if err != nil {
		return err
	}
	currentID := sess.ID
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
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	handle := c.Param("handle")
	if len(handle) != 64 {
		return fmt.Errorf("%w: session handle must be the 64-character value from the sessions listing", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	ss, err := a.validSessions(ctx, sess.Subject)
	if err != nil {
		return err
	}
	for _, s := range ss {
		if sessionHandle(s.ID) != handle {
			continue
		}
		if err := a.mutateForPerson(c, func(tx *gorm.DB) ([]authz.Event, error) {
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

// revokeSubjectSessions ends every session the caller holds — "sign me out
// everywhere", after a laptop goes missing.
//
// The ?subject= parameter is kept and is now a CHECK rather than a target: a
// client that names somebody else is refused instead of quietly signing out the
// caller. Dropping the parameter would have made the old call — which named
// another person — silently do something different and equally destructive.
func (a *admin) revokeSubjectSessions(c echo.Context) error {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	subject := sess.Subject
	if named := c.QueryParam("subject"); named != "" && named != subject {
		return echo.NewHTTPError(http.StatusForbidden, map[string]string{
			"message": "sessions belong to the person holding them; this ends your own",
			"code":    "not_your_sessions",
		})
	}
	ctx := c.Request().Context()
	var revoked int64
	if err := a.mutateForPerson(c, func(tx *gorm.DB) ([]authz.Event, error) {
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

// revocationEvent is the audit entry the spec requires: who did it (supplied by
// mutate), the subject whose access was ended, and the time (supplied by
// authz.Record). The session id appears nowhere in it.
func revocationEvent(subject string, count int64) authz.Event {
	return authz.Event{
		Action: authz.ActionSessionRevoke, ObjectType: authz.ObjectSession, ObjectLabel: subject,
		After: map[string]any{"subject": subject, "revoked": count},
	}
}
