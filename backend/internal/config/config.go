// Package config reads acmeservice's settings and validates them before any
// listener opens.
//
// Settings come from layers: built-in defaults, then a base YAML file, then one
// overlay per active profile, then the ACME_* environment (design G1). The
// environment sits on top rather than underneath, which is what makes the file
// mechanism a migration and not a cutover — a deployment configured entirely by
// ACME_*, as every deployment is today, behaves exactly as it did.
//
// Two shapes live here. Config, below, is the runtime type every other package
// sees: flat, and unchanged by this. fileConfig in schema.go is what a
// configuration FILE looks like — eight blocks, design G7 — and the mapping
// between them is one function, fileConfig.toConfig.
package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/v2"
)

// Config is the service's complete runtime configuration. Every
// service-owned variable carries the ACME_ prefix; HTTPREQ_* and LEGO_* are
// lego's own and are read by Traefik, not by us.
type Config struct {
	// Zone is the DNS zone this service is authoritative for, e.g.
	// "acmeservice.cloudpowered.services." (ACME_ZONE).
	Zone string

	// SOA fields for the zone apex (design: "the SOA serial does not change
	// when challenge records change", so Serial is a fixed configured value,
	// not derived from data).
	SOAMName   string        // ACME_SOA_PRIMARY_NS: primary nameserver name
	SOARName   string        // ACME_SOA_HOSTMASTER: responsible-party mailbox, DNS-encoded
	SOASerial  uint32        // ACME_SOA_SERIAL
	SOARefresh time.Duration // ACME_SOA_REFRESH
	SOARetry   time.Duration // ACME_SOA_RETRY
	SOAExpire  time.Duration // ACME_SOA_EXPIRE
	SOAMinimum time.Duration // ACME_SOA_MINIMUM: also the negative-cache TTL (design D7)

	// NSNames are the nameserver names published for the zone
	// (ACME_NS_NAMES, comma-separated; the runbook publishes two, D24.3).
	NSNames []string

	// Listen addresses.
	DNSListenAddr string // ACME_DNS_LISTEN: UDP+TCP, e.g. ":53"

	// ListenAddr is the one HTTP listener (design P16). It serves every path
	// group: the management API, the staff tree, the challenge protocol, the
	// console, and the health and metrics endpoints.
	//
	// It replaces three. The old split existed so a stolen API key could reach
	// no management route even if a routing rule in front of us was wrong; the
	// permission model carries that guarantee now, because a key reaches only
	// what its grants permit.
	//
	// What moves to the reverse proxy is exposure control — /metrics and the
	// staff tree are on the same port as everything else, so a deployment that
	// wants them unreachable from outside must say so in front of the service.
	ListenAddr string // ACME_LISTEN

	// DatabaseURL is a postgres:// connection string (ACME_DATABASE_URL).
	DatabaseURL string

	// AllowAccountCreation opens POST /api/v1/accounts to any authenticated
	// person.
	//
	// Default FALSE, which is the important half. Creating an account means
	// creating a tenant, and a deployment that authenticates a whole
	// organization through its identity provider does not want every one of them
	// able to do that.
	//
	// This is U9's switch from `public-edition`, borrowed early because the route
	// it governs landed here with task 7.4a and an ungated one is a hole. The
	// enforcement is in the handler rather than in the console's navigation,
	// which is U9's actual point: a route hidden from a menu is still on the
	// internet.
	AllowAccountCreation bool

	// ProjectsPerPersonalAccount is how many projects a personal account may
	// hold (ACME_PROJECT_QUOTA, default 5; design C13).
	//
	// A number rather than a constant because it is a pricing decision wearing
	// an engineering hat: pricing changes on a different clock from deployments,
	// and the first time one customer needs six is the day a constant becomes a
	// migration.
	//
	// It applies to PERSONAL accounts only. A team account has no project quota
	// at all — what limits it is its balance, and capping projects on top of
	// metering would charge twice for the same growth, once in money and once in
	// permission.
	//
	// Zero or less means no quota, which is how a deployment turns it off. It is
	// spelled that way rather than with a second boolean because "how many" and
	// "whether" are the same question asked once.
	ProjectsPerPersonalAccount int

	// MigrateOnStart runs migrations at startup unless set to false
	// (ACME_MIGRATE_ON_START), see design D8.
	MigrateOnStart bool

	// AliasUseDomain derives a new alias label from its challenge name —
	// `_acme-challenge.demo.example.com` becomes `demo-example-com` — instead
	// of generating a random one (ACME_ALIAS_USE_DOMAIN, default false; design
	// A2). A derived label is readable in a log line but computable from the
	// domain by anyone, which turns the served zone into a membership oracle,
	// so it is for deployments serving a single organisation only.
	//
	// It governs only aliases created after it takes effect: an existing
	// alias is never re-labelled, whatever this says, because its label is
	// published in a CNAME the service does not control (design A3).
	AliasUseDomain bool

	// ChallengeTTL is the TTL on published TXT answers (ACME_CHALLENGE_TTL).
	ChallengeTTL time.Duration

	// ChallengeLifetime bounds how long a published challenge value is
	// answered before the sweep may remove it (ACME_CHALLENGE_LIFETIME).
	ChallengeLifetime time.Duration

	// ZoneViewRefreshInterval is how often the in-memory zone view reloads
	// from the database on its own (ACME_VIEW_REFRESH_INTERVAL); writes
	// also trigger a synchronous refresh (design D-zoneview).
	ZoneViewRefreshInterval time.Duration

	// SweepInterval is how often expired challenge rows are deleted
	// (ACME_SWEEP_INTERVAL).
	SweepInterval time.Duration

	// InteractionRetention is how long a CLOSED challenge_interactions row
	// (withdrawn_at IS NOT NULL) is kept before the sweep deletes it
	// (ACME_INTERACTION_RETENTION). An OPEN row is never deleted at any age,
	// regardless of this setting.
	//
	// Defaults to 0, meaning keep forever: challenge_interactions holds a
	// customer load balancer's client_ip, and nobody has signed off on a
	// retention period for that yet, so nothing is deleted until an operator
	// sets this deliberately.
	//
	// The recommendation on record (rebuild-console-and-add-interaction-history,
	// task 10.2), pending that sign-off, is 180 days: two full 90-day Let's
	// Encrypt renewal cycles, so "did this domain validate last time, and the
	// time before" is always answerable from the raw rows, while
	// add-audit-and-usage-reporting's daily project snapshot preserves the
	// long-run trend after they age out. To adopt it: ACME_INTERACTION_RETENTION=4320h.
	InteractionRetention time.Duration

	// TrustedProxyCIDRs are the networks whose X-Forwarded-For header is
	// trusted for both API-key CIDR scoping and general RealIP resolution
	// (ACME_TRUSTED_PROXY_CIDRS, comma-separated; design D3).
	TrustedProxyCIDRs []string

	// VerificationResolver is the resolver address (host:port) used for
	// live DNS verification of customer records (ACME_VERIFY_RESOLVER).
	// Empty means use the system resolver.
	VerificationResolver string

	// OIDC settings (design D10).
	OIDCIssuer       string // ACME_OIDC_ISSUER
	OIDCClientID     string // ACME_OIDC_CLIENT_ID
	OIDCClientSecret string // ACME_OIDC_CLIENT_SECRET
	OIDCRedirectURL  string // ACME_OIDC_REDIRECT_URL
	OIDCGroupClaim   string // ACME_OIDC_GROUPS_CLAIM, default "groups"
	OIDCGroupPrefix  string // ACME_OIDC_GROUP_PREFIX, stripped from each group value

	// The staff surface (public-edition U7). One deployment, two hostnames, two
	// applications at the identity provider — a token for one is not a token for
	// the other, which is the property the separation rests on.
	//
	// StaffHost is what makes the split observable to this process: it decides
	// which console the embedded web UI serves and which OIDC client a sign-in
	// uses. Empty means this deployment has no staff surface at all, and
	// everything answers as the customer plane.
	StaffHost             string // ACME_STAFF_HOST, e.g. admin.acmealias.com
	StaffOIDCClientID     string // ACME_STAFF_OIDC_CLIENT_ID
	StaffOIDCClientSecret string // ACME_STAFF_OIDC_CLIENT_SECRET
	StaffOIDCRedirectURL  string // ACME_STAFF_OIDC_REDIRECT_URL

	// There is deliberately no role-group mapping here. ACME_ADMIN_GROUP and its
	// three siblings named the identity-provider group that granted each of four
	// global roles; design C10 removed the global roles, so there is nothing for
	// a group to grant. Authority is a membership row in one account.
	//
	// The group CLAIM is still read (OIDCGroupClaim above) and still recorded on
	// the session and in the audit trail: what the identity provider said about
	// a person is worth keeping even when nothing is decided from it.

	// Session lifetimes (design D10).
	SessionIdleTimeout time.Duration // ACME_SESSION_IDLE_TIMEOUT
	SessionMaxLifetime time.Duration // ACME_SESSION_MAX_LIFETIME

	// CAAIssuers are the CA identifiers CAA records authorize, e.g.
	// "letsencrypt.org" (ACME_CAA_ISSUER, comma-separated). Empty emits no
	// CAA rows at all (design D6).
	CAAIssuers []string

	// LogLevel is one of debug, info, warn, error (ACME_LOG_LEVEL).
	LogLevel string

	// ShutdownGracePeriod bounds how long in-flight work gets to finish on
	// SIGTERM/SIGINT before a forced exit (ACME_SHUTDOWN_GRACE).
	ShutdownGracePeriod time.Duration
}

// loadErrors accumulates "setting: reason" strings so Load can report every
// malformed variable at once instead of stopping at the first one.
type loadErrors []string

func (e *loadErrors) add(setting string, err error) {
	*e = append(*e, fmt.Sprintf("%s: %v", setting, err))
}

func (e loadErrors) errOrNil() error {
	if len(e) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  %s", strings.Join(e, "\n  "))
}

// Load reads Config from the configuration layers described in the package
// comment. It applies defaults for every setting that has one and reports a
// parse error, naming the setting, for any value that is present but malformed
// (e.g. an unparsable duration or boolean). It does not check that required
// values are present or make any network call; call Validate for that.
//
// The signature is the one it has always had, so no caller moves (task 4.1).
func Load() (Config, error) { return load(Options{}) }

// load is Load with the loader's knobs exposed, so a test can point at a
// specific file and a specific environment instead of the process's own.
func load(opts Options) (Config, error) {
	var errs loadErrors

	f, sources, err := loadWithSources[fileConfig](withDefaults(opts, &errs))
	if err != nil {
		// A layer that cannot be read at all — a configured base file that is
		// absent (G4), or a file that is not YAML — is fatal and has no
		// per-setting story to tell, so it short-circuits rather than joining
		// the accumulated list.
		return Config{}, err
	}

	// Reported here rather than by the caller because Load is the only place
	// that knows what it read. It goes to the default logger, which at this
	// point is still the standard one writing to stderr: Load runs before
	// obs.NewLogger, since the log level is one of the settings being read
	// (see cmd/acmeservice.serve, which reports configuration failures the
	// same way and for the same reason).
	logSources(sources)

	return f.toConfig(), errs.errOrNil()
}

// withDefaults fills in the two options this package always sets. The schema
// does the environment mapping wherever a variable's name already spells its
// path — ACME_OIDC_CLIENT_ID reaches oidc.client_id unaided — and the aliases
// cover the rest, derived from the binding table rather than written out so
// the two cannot drift (G5, task 2.4).
func withDefaults(opts Options, errs *loadErrors) Options {
	opts.EnvAliases = envAliases()
	opts.Inspect = typeCheck(errs, isSetIn(opts))
	return opts
}

// isSetIn reports, per variable, whether the environment this load reads
// actually sets it. typeCheck needs it to name the right thing: see there.
//
// It asks Options.Lookup, not Options.Environ, because Lookup is the view that
// can answer a question about one name — and normalize() makes the two the
// same environment, so the answer matches what envProvider overlaid. Empty
// counts as unset, which is envProvider's rule too.
func isSetIn(opts Options) func(string) bool {
	lookup := opts.Lookup
	if lookup == nil && opts.Environ == nil {
		lookup = os.LookupEnv
	}
	if lookup == nil {
		lookup = lookupFrom(opts.Environ)
	}
	return func(name string) bool {
		v, ok := lookup(name)
		return ok && v != ""
	}
}

// typeCheck reads back every leaf an ACME_* variable can reach and converts
// the ones that arrived as text. It is the Inspect hook of the loader, and it
// exists for two things the unmarshaller cannot do.
//
// It names WHERE THE VALUE CAME FROM. mapstructure would report `cannot parse
// 'challenge.ttl'`, which is a path in a file the deployment may not even
// have; the old loader reported `ACME_CHALLENGE_TTL: time: invalid duration
// "5 minutes"`, error strings the runbook and cmd/acmeservice quote by name,
// and that contract is kept here — but only for a value the environment
// actually set. Naming the variable unconditionally gets it exactly as wrong
// in the other direction: `ttl: "5 minutes"` typed into config.yaml would send
// the operator hunting for an ACME_CHALLENGE_TTL nobody exported. So isSet
// decides, and a file-borne value is reported against its path, which is the
// thing the operator can go and find.
//
// And it treats an empty value as absent, restoring the default. Every setting
// behaved that way before this change; a file written as
// `level: "${ACME_LOG_LEVEL:info}"` cannot produce an empty string, but one
// written as `level: ""` can, and the two should not mean different things.
// A secret is the deliberate exception: it has no default, so empty stays
// empty and Validate is what refuses to start.
//
// A malformed value keeps the default rather than the zero value, so the
// returned Config is still usable for anything the caller does before it
// looks at the error — which is what the old loader promised by returning the
// struct alongside it.
func typeCheck(errs *loadErrors, isSet func(string) bool) func(*koanf.Koanf) error {
	defaults := map[string]any{}
	collectDefaults(reflect.TypeOf(fileConfig{}), "", defaults)

	return func(k *koanf.Koanf) error {
		var setErr error
		reset := func(path string, zero any) {
			v, ok := defaults[path]
			if !ok {
				v = zero
			}
			if err := k.Set(path, v); err != nil && setErr == nil {
				setErr = fmt.Errorf("config: reset %q: %w", path, err)
			}
		}
		set := func(path string, v any) {
			if err := k.Set(path, v); err != nil && setErr == nil {
				setErr = fmt.Errorf("config: set %q: %w", path, err)
			}
		}

		for _, b := range envBindings {
			// Anything that is not text is already the type it should be: a
			// YAML scalar the parser typed (G2), or a Go default.
			raw, ok := k.Get(b.path).(string)
			if !ok {
				continue
			}

			// The name an error is reported against: the variable when the
			// operator set it, the path when the value came from a file.
			setting := b.path
			if isSet(b.env) {
				setting = b.env
			}

			switch b.kind {
			case kindString:
				if raw == "" {
					reset(b.path, "")
				}
			case kindBool:
				if raw == "" {
					reset(b.path, false)
					continue
				}
				v, err := strconv.ParseBool(raw)
				if err != nil {
					errs.add(setting, err)
					reset(b.path, false)
					continue
				}
				set(b.path, v)
			case kindDuration:
				if raw == "" {
					reset(b.path, time.Duration(0))
					continue
				}
				v, err := time.ParseDuration(raw)
				if err != nil {
					errs.add(setting, err)
					reset(b.path, time.Duration(0))
					continue
				}
				set(b.path, v)
			case kindUint32:
				if raw == "" {
					reset(b.path, uint32(0))
					continue
				}
				v, err := strconv.ParseUint(raw, 10, 32)
				if err != nil {
					errs.add(setting, err)
					reset(b.path, uint32(0))
					continue
				}
				set(b.path, uint32(v))
			case kindInt:
				// A plain count — a quota, today. Zero on an unparseable
				// value rather than the default, for the same reason every
				// branch here does: an operator who typed a number wrong
				// should see the setting refused, not silently get the
				// number they did not write.
				if raw == "" {
					reset(b.path, 0)
					continue
				}
				v, err := strconv.Atoi(raw)
				if err != nil {
					errs.add(setting, err)
					reset(b.path, 0)
					continue
				}
				set(b.path, v)
			case kindList:
				// A list written as one string — from a placeholder, or from
				// an operator who typed the environment form into a file.
				// Left alone, mapstructure's weak typing wraps it in a
				// ONE-ELEMENT slice, so "ns1,ns2" becomes the single
				// nameserver "ns1,ns2" and the zone answers with a name
				// nobody configured.
				if v := splitList(raw); v != nil {
					set(b.path, v)
				} else {
					reset(b.path, []string(nil))
				}
			}
		}
		return setErr
	}
}
