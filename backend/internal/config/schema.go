package config

// The configuration schema (design G7): what a configuration FILE looks like,
// and which ACME_* variable reaches which path inside it (G5).
//
// This is a second struct rather than a reshaped Config, deliberately. Config
// is the RUNTIME type: flat, named after what each setting does, passed by
// value into a dozen packages, and rendered by LogValue into the
// effective-configuration endpoint the console reads. Nesting Config itself
// would move every one of those call sites and change a published response
// shape — for nothing, because G7 is a statement about the file, and the file
// is what fileConfig describes. The two meet in one place, toConfig, and that
// place is the only thing a future rename has to touch.
//
// The grouping in design G7 names the eight blocks and the settings whose home
// was in doubt. The remaining settings were placed in the block they belong to
// by subject: the SOA fields and the CAA issuers are zone content, the sweep
// and the retention window are about challenge rows, migrate-on-start is about
// the database, the shutdown grace bounds the listeners' drain, and the role
// groups and session lifetimes sit with the identity provider that produces
// them (they are design D10's group in Config already).

import (
	"reflect"
	"strings"
	"time"
)

type fileConfig struct {
	Zone      zoneBlock      `koanf:"zone"`
	Listeners listenersBlock `koanf:"listeners"`
	Database  databaseBlock  `koanf:"database"`
	OIDC      oidcBlock      `koanf:"oidc"`
	Staff     staffBlock     `koanf:"staff"`
	Challenge challengeBlock `koanf:"challenge"`
	Limits    limitsBlock    `koanf:"limits"`
	Log       logBlock       `koanf:"log"`
}

// zoneBlock is everything about the zone this service is authoritative for:
// its name, the SOA it answers with, the nameservers and CAA issuers it
// publishes, and how often the in-memory view of it reloads.
type zoneBlock struct {
	Name                string        `koanf:"name"`
	SOAPrimaryNS        string        `koanf:"soa_primary_ns"`
	SOAHostmaster       string        `koanf:"soa_hostmaster"`
	SOASerial           uint32        `koanf:"soa_serial"`
	SOARefresh          time.Duration `koanf:"soa_refresh"`
	SOARetry            time.Duration `koanf:"soa_retry"`
	SOAExpire           time.Duration `koanf:"soa_expire"`
	SOAMinimum          time.Duration `koanf:"soa_minimum"`
	NSNames             []string      `koanf:"ns_names"`
	CAAIssuers          []string      `koanf:"caa_issuers"`
	ViewRefreshInterval time.Duration `koanf:"view_refresh_interval"`
}

func (zoneBlock) Defaults() map[string]any {
	return map[string]any{
		"soa_serial":            uint32(1),
		"soa_refresh":           time.Hour,
		"soa_retry":             15 * time.Minute,
		"soa_expire":            7 * 24 * time.Hour,
		"soa_minimum":           30 * time.Second,
		"view_refresh_interval": 30 * time.Second,
	}
}

type listenersBlock struct {
	DNS  string `koanf:"dns"`
	HTTP string `koanf:"http"`

	// ShutdownGrace bounds how long in-flight work gets to finish before a
	// forced exit. It lives with the listeners because what it actually
	// bounds is their drain.
	ShutdownGrace time.Duration `koanf:"shutdown_grace"`
}

func (listenersBlock) Defaults() map[string]any {
	return map[string]any{
		"dns":            ":53",
		"http":           ":8080",
		"shutdown_grace": 15 * time.Second,
	}
}

type databaseBlock struct {
	URL string `koanf:"url"`

	// Role and Password are read by the deployment's bootstrap, not by this
	// binary: development/postgres-init creates the unprivileged application
	// role from ACME_DB_ROLE and ACME_DB_PASSWORD, and the binary only ever
	// sees the assembled URL. They are in the schema because G7 puts them
	// there and because a configuration file that shows half the database
	// settings is a file an operator has to leave to check the other half.
	// They are deliberately NOT carried into Config: binding them would be a
	// new setting, and the proposal adds none.
	Role     string `koanf:"role"`
	Password string `koanf:"password"`

	MigrateOnStart bool `koanf:"migrate_on_start"`
}

func (databaseBlock) Defaults() map[string]any {
	return map[string]any{"migrate_on_start": true}
}

// OIDCClient is the shape of one OIDC application. The console has one and the
// staff plane has another, and until now the only thing saying so was two
// prefixes that happened to rhyme (G7, task 2.2). Sharing the type is what
// makes the claim structural: a field added here appears on both surfaces.
//
// The staff plane binds three of these six today — client id, secret and
// redirect URL — because it shares the console's issuer and group mapping.
// The other three exist on staff.oidc and are read by nothing; setting one is
// not an error, it simply has no consumer, and G5's "rename nothing" is why
// this change does not invent the variables that would give them one.
type OIDCClient struct {
	Issuer       string `koanf:"issuer"`
	ClientID     string `koanf:"client_id"`
	ClientSecret string `koanf:"client_secret"`
	RedirectURL  string `koanf:"redirect_url"`
	GroupsClaim  string `koanf:"groups_claim"`
	GroupPrefix  string `koanf:"group_prefix"`
}

// oidcBlock is the console's OIDC client plus how long a session minted from it
// lives.
//
// It used to carry four *_group keys naming the identity-provider group behind
// each global role. Design C10 removed both the roles and the mapping: nothing
// an identity provider says grants authority any more, so there is no key here
// that could.
type oidcBlock struct {
	OIDCClient `koanf:",squash"`

	SessionIdleTimeout time.Duration `koanf:"session_idle_timeout"`
	SessionMaxLifetime time.Duration `koanf:"session_max_lifetime"`
}

func (oidcBlock) Defaults() map[string]any {
	return map[string]any{
		"groups_claim":         "groups",
		"session_idle_timeout": time.Hour,
		"session_max_lifetime": 8 * time.Hour,
	}
}

type staffBlock struct {
	Host string     `koanf:"host"`
	OIDC OIDCClient `koanf:"oidc"`
}

type challengeBlock struct {
	TTL                  time.Duration `koanf:"ttl"`
	Lifetime             time.Duration `koanf:"lifetime"`
	SweepInterval        time.Duration `koanf:"sweep_interval"`
	InteractionRetention time.Duration `koanf:"interaction_retention"`
	TrustedProxyCIDRs    []string      `koanf:"trusted_proxy_cidrs"`
	VerifyResolver       string        `koanf:"verify_resolver"`
	AliasUseDomain       bool          `koanf:"alias_use_domain"`

	// The two activation cadences (domain-activation A2). They live in the
	// challenge block because what they gate is whether a challenge is
	// published at all, and because the resolver they are asked through —
	// verify_resolver, two lines up — is already here.
	ActivationFreshInterval   time.Duration `koanf:"activation_fresh_interval"`
	ActivationSettledInterval time.Duration `koanf:"activation_settled_interval"`
}

func (challengeBlock) Defaults() map[string]any {
	return map[string]any{
		"ttl":            30 * time.Second,
		"lifetime":       time.Hour,
		"sweep_interval": 5 * time.Minute,
		// Minutes, because the person who has just been shown the record to
		// publish is publishing it right now, and minutes is the difference
		// between "it worked" and "did I get it wrong?" (A2). Daily for a
		// settled domain, because nothing acts on that answer (A3) — it is
		// chosen for how soon we would like to TELL somebody their delegation
		// broke, against thirty days of renewal slack.
		"activation_fresh_interval":   5 * time.Minute,
		"activation_settled_interval": 24 * time.Hour,
		// Zero means keep forever. challenge_interactions holds a customer
		// load balancer's client_ip and nobody has signed off on a retention
		// period for it, so nothing is deleted until an operator says so.
		"interaction_retention": time.Duration(0),
	}
}

// limitsBlock is how much of the service one account may have. Account
// creation is here rather than under oidc because it answers the same
// question as the quota — how many of these come into existence — and not the
// question of who someone is.
type limitsBlock struct {
	// ProjectsPerPersonalAccount is personal-and-team-accounts C13's quota.
	// It is configuration and not a constant because it is a pricing
	// decision, pricing changes on a different clock from deployments, and
	// the first time one customer needs six is the day a constant becomes a
	// migration. Team accounts have no project quota at all; credit is their
	// only limit, so nothing here applies to them.
	//
	// It is read now. G7 put it in the schema and deliberately left it
	// inert, saying that the change which ENFORCES the quota is the one that
	// carries it onto the runtime Config and names its override; this is that
	// change (task 3.5a). toConfig projects it, and ACME_PROJECT_QUOTA is the
	// flat override beside the nested ACME_LIMITS_PROJECTS_PER_PERSONAL_ACCOUNT
	// the schema resolves on its own.
	ProjectsPerPersonalAccount int `koanf:"projects_per_personal_account"`

	AllowAccountCreation bool `koanf:"allow_account_creation"`
}

func (limitsBlock) Defaults() map[string]any {
	return map[string]any{"projects_per_personal_account": 5}
}

type logBlock struct {
	Level string `koanf:"level"`
}

func (logBlock) Defaults() map[string]any {
	return map[string]any{"level": "info"}
}

// toConfig flattens the loaded file schema onto the runtime Config. It is the
// single seam between the two shapes; everything downstream still sees the
// flat struct it has always seen.
func (f fileConfig) toConfig() Config {
	return Config{
		Zone: f.Zone.Name,

		SOAMName:   f.Zone.SOAPrimaryNS,
		SOARName:   f.Zone.SOAHostmaster,
		SOASerial:  f.Zone.SOASerial,
		SOARefresh: f.Zone.SOARefresh,
		SOARetry:   f.Zone.SOARetry,
		SOAExpire:  f.Zone.SOAExpire,
		SOAMinimum: f.Zone.SOAMinimum,

		NSNames:    f.Zone.NSNames,
		CAAIssuers: f.Zone.CAAIssuers,

		DNSListenAddr:       f.Listeners.DNS,
		ListenAddr:          f.Listeners.HTTP,
		ShutdownGracePeriod: f.Listeners.ShutdownGrace,

		DatabaseURL:    f.Database.URL,
		MigrateOnStart: f.Database.MigrateOnStart,

		AllowAccountCreation:       f.Limits.AllowAccountCreation,
		ProjectsPerPersonalAccount: f.Limits.ProjectsPerPersonalAccount,

		AliasUseDomain:          f.Challenge.AliasUseDomain,
		ChallengeTTL:            f.Challenge.TTL,
		ChallengeLifetime:       f.Challenge.Lifetime,
		ZoneViewRefreshInterval: f.Zone.ViewRefreshInterval,
		SweepInterval:           f.Challenge.SweepInterval,
		InteractionRetention:    f.Challenge.InteractionRetention,
		TrustedProxyCIDRs:       f.Challenge.TrustedProxyCIDRs,
		VerificationResolver:    f.Challenge.VerifyResolver,

		ActivationFreshInterval:   f.Challenge.ActivationFreshInterval,
		ActivationSettledInterval: f.Challenge.ActivationSettledInterval,

		OIDCIssuer:       f.OIDC.Issuer,
		OIDCClientID:     f.OIDC.ClientID,
		OIDCClientSecret: f.OIDC.ClientSecret,
		OIDCRedirectURL:  f.OIDC.RedirectURL,
		OIDCGroupClaim:   f.OIDC.GroupsClaim,
		OIDCGroupPrefix:  f.OIDC.GroupPrefix,

		StaffHost:             f.Staff.Host,
		StaffOIDCClientID:     f.Staff.OIDC.ClientID,
		StaffOIDCClientSecret: f.Staff.OIDC.ClientSecret,
		StaffOIDCRedirectURL:  f.Staff.OIDC.RedirectURL,

		SessionIdleTimeout: f.OIDC.SessionIdleTimeout,
		SessionMaxLifetime: f.OIDC.SessionMaxLifetime,

		LogLevel: f.Log.Level,
	}
}

// leafKind is how a value has to be read back out of the merged tree. It
// exists so a malformed value can be reported against the environment
// variable an operator recognises — see typeCheck.
type leafKind int

const (
	kindString leafKind = iota
	kindBool
	kindDuration
	kindUint32
	kindInt
	kindList
)

// envBinding is one ACME_* variable and the path it reaches (task 2.4).
type envBinding struct {
	env  string
	path string
	kind leafKind
}

// envBindings is the whole of the flat namespace, mapped onto the nested one.
// Forty-two variables, none renamed (G5, task 2.5).
//
// The table is not the mapping mechanism — the schema is, and envAliases below
// derives from this table only the entries the schema cannot reach on its own.
// ACME_OIDC_GROUPS_CLAIM finds oidc.groups_claim because the loader knows the
// shape; ACME_SOA_PRIMARY_NS cannot, because nothing in the name says `zone`,
// and G5 forbids guessing where to split a name that could split four ways.
//
// What the table IS for is the other half of the old loader's contract: every
// error names the setting the operator set. `ACME_CHALLENGE_TTL:
// time: invalid duration "5 minutes"` is actionable; `cannot parse
// 'challenge.ttl'` sends them looking for a file that may not exist.
var envBindings = []envBinding{
	{"ACME_ZONE", "zone.name", kindString},
	{"ACME_SOA_PRIMARY_NS", "zone.soa_primary_ns", kindString},
	{"ACME_SOA_HOSTMASTER", "zone.soa_hostmaster", kindString},
	{"ACME_SOA_SERIAL", "zone.soa_serial", kindUint32},
	{"ACME_SOA_REFRESH", "zone.soa_refresh", kindDuration},
	{"ACME_SOA_RETRY", "zone.soa_retry", kindDuration},
	{"ACME_SOA_EXPIRE", "zone.soa_expire", kindDuration},
	{"ACME_SOA_MINIMUM", "zone.soa_minimum", kindDuration},
	{"ACME_NS_NAMES", "zone.ns_names", kindList},
	{"ACME_CAA_ISSUER", "zone.caa_issuers", kindList},
	{"ACME_VIEW_REFRESH_INTERVAL", "zone.view_refresh_interval", kindDuration},

	{"ACME_DNS_LISTEN", "listeners.dns", kindString},
	{"ACME_LISTEN", "listeners.http", kindString},
	{"ACME_SHUTDOWN_GRACE", "listeners.shutdown_grace", kindDuration},

	{"ACME_DATABASE_URL", "database.url", kindString},
	{"ACME_DB_ROLE", "database.role", kindString},
	{"ACME_DB_PASSWORD", "database.password", kindString},
	{"ACME_MIGRATE_ON_START", "database.migrate_on_start", kindBool},

	{"ACME_OIDC_ISSUER", "oidc.issuer", kindString},
	{"ACME_OIDC_CLIENT_ID", "oidc.client_id", kindString},
	{"ACME_OIDC_CLIENT_SECRET", "oidc.client_secret", kindString},
	{"ACME_OIDC_REDIRECT_URL", "oidc.redirect_url", kindString},
	{"ACME_OIDC_GROUPS_CLAIM", "oidc.groups_claim", kindString},
	{"ACME_OIDC_GROUP_PREFIX", "oidc.group_prefix", kindString},
	{"ACME_SESSION_IDLE_TIMEOUT", "oidc.session_idle_timeout", kindDuration},
	{"ACME_SESSION_MAX_LIFETIME", "oidc.session_max_lifetime", kindDuration},

	{"ACME_STAFF_HOST", "staff.host", kindString},
	{"ACME_STAFF_OIDC_CLIENT_ID", "staff.oidc.client_id", kindString},
	{"ACME_STAFF_OIDC_CLIENT_SECRET", "staff.oidc.client_secret", kindString},
	{"ACME_STAFF_OIDC_REDIRECT_URL", "staff.oidc.redirect_url", kindString},

	{"ACME_CHALLENGE_TTL", "challenge.ttl", kindDuration},
	{"ACME_CHALLENGE_LIFETIME", "challenge.lifetime", kindDuration},
	{"ACME_SWEEP_INTERVAL", "challenge.sweep_interval", kindDuration},
	{"ACME_INTERACTION_RETENTION", "challenge.interaction_retention", kindDuration},
	{"ACME_TRUSTED_PROXY_CIDRS", "challenge.trusted_proxy_cidrs", kindList},
	{"ACME_VERIFY_RESOLVER", "challenge.verify_resolver", kindString},
	{"ACME_ALIAS_USE_DOMAIN", "challenge.alias_use_domain", kindBool},
	{"ACME_ACTIVATION_FRESH_INTERVAL", "challenge.activation_fresh_interval", kindDuration},
	{"ACME_ACTIVATION_SETTLED_INTERVAL", "challenge.activation_settled_interval", kindDuration},

	{"ACME_ALLOW_ACCOUNT_CREATION", "limits.allow_account_creation", kindBool},
	{"ACME_PROJECT_QUOTA", "limits.projects_per_personal_account", kindInt},

	{"ACME_LOG_LEVEL", "log.level", kindString},
}

// envAliases is the subset of envBindings the schema cannot resolve on its
// own, computed rather than written out so the two can never disagree.
//
// A variable needs an alias exactly when its name, stripped of the prefix, is
// not the flattened form of its path — which is every variable whose group was
// not already spelled into its name. ACME_OIDC_CLIENT_ID needs none;
// ACME_SOA_PRIMARY_NS does, because "soa_primary_ns" names no leaf. ACME_ZONE
// is the case that shows why the alias must be consulted FIRST: "zone" does
// match the schema, as the whole zone block, and mapping a string onto a block
// is a worse failure than mapping onto nothing.
func envAliases() map[string]string {
	sc := envSchema()
	aliases := map[string]string{}
	for _, b := range envBindings {
		rest := strings.ToLower(strings.TrimPrefix(b.env, EnvPrefix))
		if path, ok := sc.flat[rest]; ok && path == b.path {
			continue
		}
		aliases[b.env] = b.path
	}
	return aliases
}

// envSchema is the reflected key schema of fileConfig. It is rebuilt per call
// rather than cached: it is walked twice per process, and a package-level
// cache would be one more thing to reason about at init time.
func envSchema() *schema { return buildSchema(reflect.TypeOf(fileConfig{})) }
