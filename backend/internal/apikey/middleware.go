package apikey

import (
	"context"
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

	// NodeAddresses resolves one project's node addresses AT THIS MOMENT, for a
	// project key carrying the address switch (design K3). It is a function
	// rather than something copied onto the key precisely so that adding a node
	// changes what the key permits with no further action.
	//
	// nil means the switch cannot be evaluated, and a key carrying it is then
	// REFUSED rather than let through: a limit that cannot be checked is not a
	// limit, and the whole point of the switch is that somebody asked for one.
	NodeAddresses func(ctx context.Context, accountID, projectID model.ID) ([]string, error)

	// NoteRefusal records the address a key was just refused for, so the console
	// can show it (design K5). Best-effort; nil records nothing.
	NoteRefusal func(ctx context.Context, id model.ID, ip string)
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
				opts.noteRefusal(c, k)
				return deny("source address outside the key's CIDR restrictions",
					slog.String("identifier", k.Identifier))
			}
			if ok, reason := opts.nodeLimitAllows(c, k); !ok {
				opts.noteRefusal(c, k)
				return deny(reason, slog.String("identifier", k.Identifier))
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

// nodeLimitAllows evaluates the project-node address switch (design K3, K4).
//
// IT DELIBERATELY DOES NOT SHARE A CODE PATH WITH addressAllowed, and the reason
// is that the same emptiness means opposite things in the two. An empty CIDR
// list means the customer never asked for a limit, so everything is allowed.
// An empty NODE SET means the customer asked for "only from my nodes" and has
// no nodes — so nothing is allowed. Folding them together would produce a key
// that says it is restricted and is not, which is the worst of the four
// possible outcomes because it is the one nobody would notice.
func (o Options) nodeLimitAllows(c echo.Context, k *model.APIKey) (bool, string) {
	if k.Kind != model.APIKeyKindProject || !k.RestrictToProjectNodes {
		return true, ""
	}
	if k.ProjectID == nil {
		// The database forbids this shape. Reaching it means something upstream
		// is wrong, and a limit that cannot name its project is not a limit.
		return false, "key is limited to its project's node addresses and names no project"
	}
	if o.NodeAddresses == nil {
		return false, "key is limited to its project's node addresses and this listener cannot resolve them"
	}
	addrs, err := o.NodeAddresses(c.Request().Context(), k.AccountID, *k.ProjectID)
	if err != nil {
		// Fail closed. A lookup that failed is not evidence that the address is
		// permitted, and answering as though it were would turn a database
		// blip into a hole in the one restriction somebody explicitly asked for.
		return false, "key is limited to its project's node addresses and the lookup failed: " + err.Error()
	}
	if len(addrs) == 0 {
		return false, "key is limited to its project's node addresses and the project has none"
	}
	if !nodeAddressAllowed(addrs, c.RealIP()) {
		return false, "source address is not one of the project's node addresses"
	}
	return true, ""
}

// nodeAddressAllowed compares the address a request ARRIVED FROM against the
// addresses configured on the project's nodes.
//
// The two are the same only when the node speaks to us directly (design K5).
// Behind NAT, a CDN or an egress gateway a node whose ipv4 is a private address
// arrives from something else entirely, and the switch refuses every request
// while looking correctly configured. That is not solvable here and is not
// papered over: the switch is opt-in, and NoteRefusal records what was actually
// seen so the console can say so.
//
// Exact addresses, not prefixes: a node carries one address, not a network.
func nodeAddressAllowed(addrs []string, ip string) bool {
	got, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	got = got.Unmap()
	for _, a := range addrs {
		want, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		if want.Unmap() == got {
			return true
		}
	}
	return false
}

// noteRefusal records the address, best-effort. It runs for every address
// refusal — the hand-typed list and the switch alike — because "why is this key
// being refused" is the same question either way.
func (o Options) noteRefusal(c echo.Context, k *model.APIKey) {
	if o.NoteRefusal == nil || k == nil {
		return
	}
	o.NoteRefusal(c.Request().Context(), k.ID, c.RealIP())
}
