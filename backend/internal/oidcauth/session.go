package oidcauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// SessionCookieName is the opaque session cookie. Its value is a session id
// and nothing else: the row it names holds the state, so it can be revoked —
// which is the whole reason sessions are not a self-contained signed cookie.
const SessionCookieName = "acme_session"

// touchInterval is how stale last_seen_at may get before Authenticate writes
// it back.
//
// ponytail: without it every authenticated GET costs a write. The cost is that
// the idle timeout is fuzzy by up to this much — irrelevant against a
// timeout measured in tens of minutes. If the idle timeout is ever configured
// shorter than this, touch on every request instead.
const touchInterval = time.Minute

// ErrNoSession means the request carried no session cookie: not signed in.
var ErrNoSession = errors.New("oidcauth: no session")

// ErrSessionInvalid means the request carried a session cookie that is no
// longer good — unknown, revoked, idle too long, or past its absolute
// lifetime. All four are the same thing to the caller: unauthenticated.
var ErrSessionInvalid = errors.New("oidcauth: session invalid")

// Authenticate resolves the session cookie on a request. It returns
// ErrNoSession or ErrSessionInvalid when the request is unauthenticated; the
// caller answers 401 and points at the login URL.
//
// Both timeouts are enforced here, on every request, so revocation and expiry
// take effect on the next request rather than at some sweep interval.
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (*model.Session, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return nil, ErrNoSession
	}

	sess, err := store.FindSession(ctx, s.db, c.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSessionInvalid, err)
	}

	now := time.Now().UTC()
	if err := checkSession(sess, s.cfg.SessionIdleTimeout, now); err != nil {
		return nil, err
	}

	if now.Sub(sess.LastSeenAt) > touchInterval {
		if err := store.TouchSession(ctx, s.db, sess.ID); err != nil {
			return nil, err
		}
		sess.LastSeenAt = now
	}
	return sess, nil
}

// checkSession is the whole validity rule, as a pure function: revoked, past
// its absolute lifetime, or idle too long.
func checkSession(sess *model.Session, idle time.Duration, now time.Time) error {
	switch {
	case sess.RevokedAt != nil:
		return fmt.Errorf("%w: revoked", ErrSessionInvalid)
	case !now.Before(sess.ExpiresAt):
		return fmt.Errorf("%w: past its absolute lifetime", ErrSessionInvalid)
	case idle > 0 && now.Sub(sess.LastSeenAt) >= idle:
		return fmt.Errorf("%w: idle for longer than %s", ErrSessionInvalid, idle)
	}
	return nil
}

// Logout revokes the session the request carries and clears its cookie.
// Revoking rather than deleting keeps the row for the audit trail; the sweep
// (store.DeleteExpiredSessions) removes it once it has expired.
func (s *Service) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, clearCookie(SessionCookieName))

	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return nil // already signed out; nothing to revoke
	}
	if err := store.RevokeSession(ctx, s.db, c.Value); err != nil {
		return err
	}
	return nil
}

// randomToken returns 256 bits of crypto/rand as URL-safe base64 — used for
// session ids, state and nonce alike.
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("oidcauth: crypto/rand failed: " + err.Error()) // unreachable on Go 1.24+; a failure here means no safe session id exists
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// newCookie builds a cookie with the attributes design D10 requires:
// HttpOnly, Secure, SameSite=Lax and host-only (no Domain, so it is never
// sent to a sibling hostname).
func newCookie(name, value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearCookie expires a cookie set by newCookie. The attributes must match or
// the browser keeps the original.
func clearCookie(name string) *http.Cookie {
	c := newCookie(name, "", 0)
	c.MaxAge = -1
	return c
}
