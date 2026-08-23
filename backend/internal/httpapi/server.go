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

// ServerOptions carry everything the listener needs. Challenge, Admin and
// Metrics are required; Console, Liveness and Readiness are omitted only by
// tests that do not exercise them.
type ServerOptions struct {
	Challenge ChallengeOptions
	Admin     AdminOptions

	// Console serves the embedded single-page application on the admin
	// listener, under the management API rather than over it: Echo prefers
	// the longer static prefix, so RegisterAdmin's /api/* catch-all wins for
	// anything under /api/ and this handler never sees it.
	Console http.Handler

	// Liveness and Readiness are unauthenticated, and since design P16 they sit
	// on the same listener as everything else.
	Liveness  http.Handler
	Readiness http.Handler

	Metrics *obs.Metrics
	Log     *slog.Logger
}

// Servers holds the built Echo instances. They are handlers only: the caller
// opens the listeners, which is what lets startup report a bind failure before
// it reports itself ready.
//
// There is one now where there were three (design P16). The old split existed so
// that "a stolen API key reaches no management route even if a routing rule in
// front of us is wrong" — a guarantee now carried by the permission model
// instead of by network topology, because a key reaches only what its grants
// permit.
//
// The trade is explicit and worth stating: network-level exposure control moves
// to the reverse proxy. In particular /metrics is now on the same listener as
// everything else, so a deployment that wants it unreachable from outside has to
// say so in front of the service rather than relying on a second port nobody
// published.
//
// Satellites will get their own listener, because mutual TLS is the one thing
// paths cannot express: the handshake completes before any path is known, so a
// client-certificate requirement is a property of a listener.
type Servers struct {
	// Public serves every path group: /api/v1/**, /api/admin/**,
	// /acme/httpreq/**, the bare /present and /cleanup, the console, and the
	// health and metrics endpoints.
	Public *echo.Echo

	// Satellite is nil until `satellite-sync` fills it. It exists here so the
	// shape of the answer is visible: a second listener, not a path prefix.
	Satellite *echo.Echo
}

// NewServers builds all three. It returns an error rather than panicking
// because RegisterChallenge runs the fail-closed trusted-proxy check (design
// D3): a CIDR-restricted key with no trusted proxy configured stops the
// service starting instead of displaying a protection that is not there.
//
// ctx is the startup context, used only for that check.
func NewServers(ctx context.Context, o ServerOptions) (*Servers, error) {
	e := newEcho("public", o.Metrics)

	// The management API first: it installs the JSON error handler and the
	// /api/* catch-all that the console fallback must not shadow.
	if err := RegisterAdmin(e, o.Admin); err != nil {
		return nil, err
	}

	// The challenge protocol carries its own body limit rather than the whole
	// listener doing so. A publish body is two short strings; the management API
	// takes grant lists and role definitions, and holding those to 8K would be a
	// limit nobody could see the reason for.
	if err := RegisterChallenge(ctx, e, o.Challenge); err != nil {
		return nil, err
	}

	if o.Liveness != nil {
		e.GET("/healthz", echo.WrapHandler(o.Liveness))
	}
	if o.Readiness != nil {
		e.GET("/readyz", echo.WrapHandler(o.Readiness))
	}
	if o.Metrics != nil {
		e.GET("/metrics", echo.WrapHandler(promhttp.HandlerFor(o.Metrics.Registry, promhttp.HandlerOpts{})))
	}

	// Last, so the longer static prefixes above win: Echo prefers them, and the
	// /api/* catch-all answers anything under /api/ that no route claimed.
	if o.Console != nil {
		console := echo.WrapHandler(o.Console)
		e.GET("/*", console)
		e.HEAD("/*", console)
	}

	return &Servers{Public: e}, nil
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
