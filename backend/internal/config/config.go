// Package config reads acmeservice's settings from the environment and
// validates them before any listener opens. There is no file overlay: the
// deployment and the dev compose stack already know how to set environment
// variables, and a config library would be more code than the settings we
// have (design D1).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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
	// able to do that — the internal edition has exactly one account and expects
	// to keep it.
	//
	// This is U9's switch from `public-edition`, borrowed early because the route
	// it governs landed here with task 7.4a and an ungated one is a hole. The
	// enforcement is in the handler rather than in the console's navigation,
	// which is U9's actual point: a route hidden from a menu is still on the
	// internet.
	AllowAccountCreation bool

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
	// uses. Empty means this deployment has no staff surface at all, which is the
	// internal edition's case, and everything answers as the customer plane.
	StaffHost             string // ACME_STAFF_HOST, e.g. admin.acmealias.com
	StaffOIDCClientID     string // ACME_STAFF_OIDC_CLIENT_ID
	StaffOIDCClientSecret string // ACME_STAFF_OIDC_CLIENT_SECRET
	StaffOIDCRedirectURL  string // ACME_STAFF_OIDC_REDIRECT_URL

	// Role-group mapping: which Keycloak group grants each role. An unset
	// variable means nobody holds that role (design D10).
	AdminGroup       string // ACME_ADMIN_GROUP
	ConfigAdminGroup string // ACME_CONFIG_ADMIN_GROUP
	KeyRotationGroup string // ACME_KEY_ROTATION_GROUP
	ReadOnlyGroup    string // ACME_READ_ONLY_GROUP

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

// Load reads Config from the environment. It applies defaults for every
// setting that has one and reports a parse error, naming the setting, for
// any variable whose value is present but malformed (e.g. an unparsable
// duration or boolean). It does not check that required values are present
// or make any network call; call Validate for that.
func Load() (Config, error) {
	var errs loadErrors

	dur := func(name string, def time.Duration) time.Duration {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			errs.add(name, err)
			return def
		}
		return d
	}
	boolean := func(name string, def bool) bool {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs.add(name, err)
			return def
		}
		return b
	}
	str := func(name, def string) string {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			return def
		}
		return v
	}
	uint32Val := func(name string, def uint32) uint32 {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			return def
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			errs.add(name, err)
			return def
		}
		return uint32(n)
	}
	csv := func(name string) []string {
		v := os.Getenv(name)
		if strings.TrimSpace(v) == "" {
			return nil
		}
		var out []string
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	}

	cfg := Config{
		Zone: str("ACME_ZONE", ""),

		SOAMName:   str("ACME_SOA_PRIMARY_NS", ""),
		SOARName:   str("ACME_SOA_HOSTMASTER", ""),
		SOASerial:  uint32Val("ACME_SOA_SERIAL", 1),
		SOARefresh: dur("ACME_SOA_REFRESH", time.Hour),
		SOARetry:   dur("ACME_SOA_RETRY", 15*time.Minute),
		SOAExpire:  dur("ACME_SOA_EXPIRE", 7*24*time.Hour),
		SOAMinimum: dur("ACME_SOA_MINIMUM", 30*time.Second),

		NSNames: csv("ACME_NS_NAMES"),

		DNSListenAddr: str("ACME_DNS_LISTEN", ":53"),
		ListenAddr:    str("ACME_LISTEN", ":8080"),

		DatabaseURL:          str("ACME_DATABASE_URL", ""),
		AllowAccountCreation: boolean("ACME_ALLOW_ACCOUNT_CREATION", false),
		MigrateOnStart:       boolean("ACME_MIGRATE_ON_START", true),

		AliasUseDomain: boolean("ACME_ALIAS_USE_DOMAIN", false),

		ChallengeTTL:            dur("ACME_CHALLENGE_TTL", 30*time.Second),
		ChallengeLifetime:       dur("ACME_CHALLENGE_LIFETIME", time.Hour),
		ZoneViewRefreshInterval: dur("ACME_VIEW_REFRESH_INTERVAL", 30*time.Second),
		SweepInterval:           dur("ACME_SWEEP_INTERVAL", 5*time.Minute),
		InteractionRetention:    dur("ACME_INTERACTION_RETENTION", 0),

		TrustedProxyCIDRs: csv("ACME_TRUSTED_PROXY_CIDRS"),

		VerificationResolver: str("ACME_VERIFY_RESOLVER", ""),

		OIDCIssuer:       str("ACME_OIDC_ISSUER", ""),
		OIDCClientID:     str("ACME_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: str("ACME_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  str("ACME_OIDC_REDIRECT_URL", ""),
		OIDCGroupClaim:   str("ACME_OIDC_GROUPS_CLAIM", "groups"),
		OIDCGroupPrefix:  str("ACME_OIDC_GROUP_PREFIX", ""),

		StaffHost:             str("ACME_STAFF_HOST", ""),
		StaffOIDCClientID:     str("ACME_STAFF_OIDC_CLIENT_ID", ""),
		StaffOIDCClientSecret: str("ACME_STAFF_OIDC_CLIENT_SECRET", ""),
		StaffOIDCRedirectURL:  str("ACME_STAFF_OIDC_REDIRECT_URL", ""),

		AdminGroup:       str("ACME_ADMIN_GROUP", ""),
		ConfigAdminGroup: str("ACME_CONFIG_ADMIN_GROUP", ""),
		KeyRotationGroup: str("ACME_KEY_ROTATION_GROUP", ""),
		ReadOnlyGroup:    str("ACME_READ_ONLY_GROUP", ""),

		SessionIdleTimeout: dur("ACME_SESSION_IDLE_TIMEOUT", time.Hour),
		SessionMaxLifetime: dur("ACME_SESSION_MAX_LIFETIME", 8*time.Hour),

		CAAIssuers: csv("ACME_CAA_ISSUER"),

		LogLevel: str("ACME_LOG_LEVEL", "info"),

		ShutdownGracePeriod: dur("ACME_SHUTDOWN_GRACE", 15*time.Second),
	}

	return cfg, errs.errOrNil()
}
