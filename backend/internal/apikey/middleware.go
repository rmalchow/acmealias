package apikey

import (
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// ContextKey is where the verified key lands in the Echo context.
const ContextKey = "apikey"

// FromContext returns the key that authenticated the request, or nil when the
// handler was not behind Middleware.
func FromContext(c echo.Context) *model.APIKey {
	k, _ := c.Get(ContextKey).(*model.APIKey)
	return k
}

// Options configure Middleware. Lookup is required; the rest may be zero.
type Options struct {
	Lookup   Lookup
	LastUsed *LastUsed    // nil disables last_used_at bookkeeping
	Logger   *slog.Logger // nil uses slog.Default()
}

// errUnauthorized is the single response every rejection produces — unknown
// identifier, wrong secret, expired, revoked, or a source address outside the
// key's CIDR blocks. The reason is separated out into the log; a caller cannot
// tell the cases apart, which is the point (design D3).
var errUnauthorized = echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")

// Middleware authenticates a request from an API key presented as HTTP Basic
// credentials (identifier as username, secret as password — this is what lego's
// httpreq provider sends), as `Authorization: Bearer <identifier>.<secret>`, or
// as `X-API-Key: <identifier>.<secret>`.
func Middleware(opts Options) echo.MiddlewareFunc {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			deny := func(reason string, args ...any) error {
				c.Response().Header().Set(echo.HeaderWWWAuthenticate, `Basic realm="acmeservice"`)
				log.WarnContext(c.Request().Context(), "api key rejected",
					append([]any{slog.String("reason", reason), slog.String("client_ip", c.RealIP())}, args...)...)
				return errUnauthorized
			}

			identifier, secret, ok := credentials(c.Request())
			if !ok {
				return deny("no credentials presented")
			}

			k, err := Verify(c.Request().Context(), opts.Lookup, identifier, secret, time.Now())
			if err != nil {
				return deny(err.Error())
			}
			if !addressAllowed(k.Networks, c.RealIP()) {
				return deny("source address outside the key's CIDR restrictions",
					slog.String("identifier", k.Identifier))
			}

			if opts.LastUsed != nil {
				opts.LastUsed.Touch(c.Request().Context(), k.ID)
			}
			c.Set(ContextKey, k)
			return next(c)
		}
	}
}

// credentials pulls an identifier/secret pair out of whichever of the three
// accepted transports the request used.
func credentials(r *http.Request) (identifier, secret string, ok bool) {
	if id, sec, basic := r.BasicAuth(); basic {
		return id, sec, id != "" && sec != ""
	}
	if raw, found := strings.CutPrefix(r.Header.Get(echo.HeaderAuthorization), "Bearer "); found {
		return splitToken(raw)
	}
	if raw := r.Header.Get("X-API-Key"); raw != "" {
		return splitToken(raw)
	}
	return "", "", false
}

// splitToken parses the `<identifier>.<secret>` form. Neither half ever contains
// a dot: identifiers are "k_" + hex and secrets are base64url.
func splitToken(raw string) (identifier, secret string, ok bool) {
	id, sec, found := strings.Cut(strings.TrimSpace(raw), ".")
	return id, sec, found && id != "" && sec != ""
}

// addressAllowed reports whether ip falls inside one of the key's CIDR blocks.
// No blocks means unrestricted. An unparseable address or block is treated as a
// non-match, so a malformed value fails closed.
func addressAllowed(networks []model.APIKeyNetwork, ip string) bool {
	if len(networks) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, n := range networks {
		// ponytail: linear scan, re-parsed per request. Keys carry a handful of
		// blocks at most; if that changes, parse them once at lookup time.
		if p, err := netip.ParsePrefix(n.CIDR); err == nil && p.Masked().Contains(addr) {
			return true
		}
	}
	return false
}
