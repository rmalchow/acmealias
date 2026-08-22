package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
)

// The three HTTP surfaces, each on its own Echo instance and its own listener
// (design D9). The separation is the point: a stolen API key reaches no
// management route even if a routing rule in front of us is wrong, and health
// and metrics stay off both public listeners whatever the proxy is told to
// forward. Nothing here mounts a route belonging to another listener, and the
// tests assert exactly that.
//
//	challenge  :8080  POST /present, /cleanup                 public
//	admin      :8081  /api/v1/**, the console                 our networks only
//	internal   :9090  /healthz, /readyz, /metrics             never exposed

// challengeBodyLimit is the strict cap on a challenge request body. lego sends
// two short strings; anything larger is a mistake or an attack, and rejecting
// it before the JSON decoder runs costs nothing.
const challengeBodyLimit = "8K"

// ServerOptions carry everything the three listeners need. Challenge, Admin
// and Metrics are required; Console, Liveness and Readiness are omitted only
// by tests that do not exercise them.
type ServerOptions struct {
	Challenge ChallengeOptions
	Admin     AdminOptions

	// Console serves the embedded single-page application on the admin
	// listener, under the management API rather than over it: Echo prefers
	// the longer static prefix, so RegisterAdmin's /api/* catch-all wins for
	// anything under /api/ and this handler never sees it.
	Console http.Handler

	// Liveness and Readiness are mounted on the internal listener only, and
	// unauthenticated — that placement is what the service-operations spec
	// requires and what this package is where it happens.
	Liveness  http.Handler
	Readiness http.Handler

	Metrics *obs.Metrics
	Log     *slog.Logger
}

// Servers holds the three built Echo instances. They are handlers only: the
// caller opens the listeners, which is what lets startup report a bind failure
// before it reports itself ready.
type Servers struct {
	Challenge *echo.Echo
	Admin     *echo.Echo
	Internal  *echo.Echo
}

// NewServers builds all three. It returns an error rather than panicking
// because RegisterChallenge runs the fail-closed trusted-proxy check (design
// D3): a CIDR-restricted key with no trusted proxy configured stops the
// service starting instead of displaying a protection that is not there.
//
// ctx is the startup context, used only for that check.
func NewServers(ctx context.Context, o ServerOptions) (*Servers, error) {
	challenge := newEcho("challenge", o.Metrics)
	challenge.Use(middleware.BodyLimit(challengeBodyLimit))
	if err := RegisterChallenge(ctx, challenge, o.Challenge); err != nil {
		return nil, err
	}

	admin := newEcho("admin", o.Metrics)
	// RegisterAdmin first: it installs the JSON error handler and the /api/*
	// catch-all the console fallback must not shadow.
	if err := RegisterAdmin(admin, o.Admin); err != nil {
		return nil, err
	}
	if o.Console != nil {
		console := echo.WrapHandler(o.Console)
		admin.GET("/*", console)
		admin.HEAD("/*", console)
	}

	internal := newEcho("internal", o.Metrics)
	if o.Liveness != nil {
		internal.GET("/healthz", echo.WrapHandler(o.Liveness))
	}
	if o.Readiness != nil {
		internal.GET("/readyz", echo.WrapHandler(o.Readiness))
	}
	if o.Metrics != nil {
		internal.GET("/metrics", echo.WrapHandler(promhttp.HandlerFor(o.Metrics.Registry, promhttp.HandlerOpts{})))
	}

	return &Servers{Challenge: challenge, Admin: admin, Internal: internal}, nil
}

// newEcho is the middleware every listener shares: panic recovery, and the
// request counter labelled with which listener answered.
func newEcho(listener string, m *obs.Metrics) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	if m != nil {
		e.Use(countRequests(m, listener))
	}
	return e
}

// countRequests records every request by listener, matched route and status.
//
// It reports c.Path() — the route pattern, "/api/v1/loadbalancers/:id", never the
// concrete path — so a metric label can never grow one series per object id.
// Errors are handed to the error handler here rather than returned, because
// the response status does not exist until that has run and the status is
// half of what this counts.
func countRequests(m *obs.Metrics, listener string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if err := next(c); err != nil {
				c.Error(err)
			}
			route := c.Path()
			if route == "" {
				route = "unmatched"
			}
			m.HTTPRequests.WithLabelValues(listener, route, strconv.Itoa(c.Response().Status)).Inc()
			return nil
		}
	}
}
