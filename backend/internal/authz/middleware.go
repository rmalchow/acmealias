package authz

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// SessionContextKey is where Authenticated leaves the session it resolved.
//
// It used to be accompanied by a set of roles derived from the session's group
// claim on every request. There is no such thing now (design C10): authority is
// per account and is resolved once the request's account is known, which is
// downstream of this middleware and cannot be known here.
const SessionContextKey = "authz.session"

// SessionFromContext returns the session that authenticated the request, or nil
// when the handler was not behind Authenticated.
func SessionFromContext(c echo.Context) *model.Session {
	s, _ := c.Get(SessionContextKey).(*model.Session)
	return s
}

// Authenticator resolves a request's console session. *oidcauth.Service
// satisfies it; the interface is here only to keep authz from importing it.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (*model.Session, error)
}

// Guard establishes who is asking. It no longer decides what they may do.
type Guard struct {
	Auth Authenticator
	Log  *slog.Logger
}

// Authenticated resolves the session and refuses a request without one.
//
// It used to do a second thing: derive roles from the session's group claim and
// refuse — with a 403 naming the groups it had observed — an authenticated user
// who landed in none of the four configured groups. That refusal is gone, and
// its absence is the point rather than an omission. Every authenticated person
// now has somewhere to be: their personal account is materialized on first
// sign-in (design C1), so "signed in and permitted nothing" is not a state the
// service can be configured into. What replaces the diagnostic is the account
// picker, which is reached on every sign-in and is never empty.
//
// The route-level permission middleware went with it. Permission checks live in
// internal/inventory beside the operation, where they can name the object and
// where `adminctl` runs the same code (design P7) — a rule enforced at the door
// was a rule the CLI walked straight past.
func (g *Guard) Authenticated() echo.MiddlewareFunc {
	log := g.Log
	if log == nil {
		log = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			sess, err := g.Auth.Authenticate(req.Context(), req)
			if err != nil {
				log.DebugContext(req.Context(), "console request unauthenticated",
					slog.String("reason", err.Error()))
				return echo.NewHTTPError(http.StatusUnauthorized, "not signed in")
			}
			c.Set(SessionContextKey, sess)
			return next(c)
		}
	}
}
