package authz

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Context keys under which Authenticated leaves the session and the roles it
// derived for this request.
const (
	SessionContextKey = "authz.session"
	RolesContextKey   = "authz.roles"
)

// SessionFromContext returns the session that authenticated the request, or nil
// when the handler was not behind Authenticated.
func SessionFromContext(c echo.Context) *model.Session {
	s, _ := c.Get(SessionContextKey).(*model.Session)
	return s
}

// RolesFromContext returns the roles derived for this request. It returns an
// empty set — which permits nothing — when the handler was not behind
// Authenticated, so a route wired up wrongly refuses rather than admits.
func RolesFromContext(c echo.Context) RoleSet {
	r, _ := c.Get(RolesContextKey).(RoleSet)
	if r == nil {
		return RoleSet{}
	}
	return r
}

// Authenticator resolves a request's console session. *oidcauth.Service
// satisfies it; the interface is here only to keep authz from importing it.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (*model.Session, error)
}

// Guard turns a session into a role set on every request.
type Guard struct {
	Auth    Authenticator
	Mapping Mapping
	Log     *slog.Logger
}

// NoRoleBody is the 403 body an authenticated user in none of the configured
// groups receives.
//
// It names the groups that were observed on purpose. That list against the
// four ACME_*_GROUP variables is the only way to diagnose the two mistakes
// that actually happen — a typo, and Keycloak's "Full group path" left switched
// on, which turns "admins" into "/acme/admins". The configured group names are
// not echoed back: they go to the log, where the operator already is, rather
// than to a caller who has just been refused.
type NoRoleBody struct {
	Message        string   `json:"message"`
	ObservedGroups []string `json:"observed_groups"`
}

// Authenticated resolves the session, derives roles from it and the current
// mapping, and refuses an authenticated user who holds no role at all.
//
// Roles are derived per request and left in the Echo context, never stored on
// the session row: the session records the groups as seen at sign-in, and the
// mapping is read from the running configuration, so neither is cached here.
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

			roles := g.Mapping.RolesFor(sess.Groups)
			if roles.Empty() {
				log.WarnContext(req.Context(), "authenticated user holds no role",
					slog.String("subject", sess.Subject),
					slog.Any("observed_groups", []string(sess.Groups)),
					slog.Any("mapping", g.Mapping))
				return echo.NewHTTPError(http.StatusForbidden, NoRoleBody{
					Message:        "you are signed in but none of your groups is mapped to a role; check the group-to-role configuration",
					ObservedGroups: sess.Groups,
				})
			}

			c.Set(SessionContextKey, sess)
			c.Set(RolesContextKey, roles)
			return next(c)
		}
	}
}

// Require enforces one permission on a route. Mount it inside Authenticated.
func Require(action Action) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			roles := RolesFromContext(c)
			if !Can(roles, action) {
				return echo.NewHTTPError(http.StatusForbidden,
					"your roles ("+joinRoles(roles)+") do not permit "+string(action))
			}
			return next(c)
		}
	}
}

func joinRoles(roles RoleSet) string {
	if roles.Empty() {
		return "none"
	}
	return strings.Join(roles.List(), ", ")
}
