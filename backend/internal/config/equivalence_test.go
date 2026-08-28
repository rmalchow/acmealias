package config

import (
	"bufio"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The load-bearing test of the layered-configuration change (task 5.1, design
// G1): with NO configuration file present, a deployment setting only ACME_*
// variables must produce exactly the configuration it produced before.
//
// "Before" is not a snapshot of a struct here, it is the previous loader
// itself — legacyLoad below is the body of config.Load as of commit b0cf3b6,
// copied verbatim except that os.LookupEnv became an injected lookup so both
// implementations read the same map. A snapshot would have to be regenerated
// by hand every time a default changes and would then only prove that someone
// regenerated it; keeping the old code means the two are compared on every
// environment a test cares to invent, including the ones nobody thought of
// when the snapshot was taken.
//
// The copy is deliberately frozen. It is not a second implementation to keep
// in step with the first: when this change is archived it goes with it.

// legacyLoad is the pre-change loader, verbatim, parameterised on its
// environment. Do not "fix" anything in it — its bugs, if it has any, are the
// behaviour this change promised not to alter.
//
// One edit has been made since it was frozen, and it is not a fix. The four
// ACME_*_GROUP settings were REMOVED from Config by design C10, so the four
// lines that read them no longer compile. A setting that does not exist cannot
// be loaded equivalently, and leaving them here would mean keeping the field on
// Config purely so a frozen copy could assign to it. Their fixture values are
// removed from the two environments below for the same reason; nothing else
// about them changed.
func legacyLoad(lookup LookupFunc) (Config, error) {
	var errs loadErrors

	dur := func(name string, def time.Duration) time.Duration {
		v, ok := lookup(name)
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
		v, ok := lookup(name)
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
		v, ok := lookup(name)
		if !ok || v == "" {
			return def
		}
		return v
	}
	uint32Val := func(name string, def uint32) uint32 {
		v, ok := lookup(name)
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
		v, _ := lookup(name)
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

		SessionIdleTimeout: dur("ACME_SESSION_IDLE_TIMEOUT", time.Hour),
		SessionMaxLifetime: dur("ACME_SESSION_MAX_LIFETIME", 8*time.Hour),

		CAAIssuers: csv("ACME_CAA_ISSUER"),

		LogLevel: str("ACME_LOG_LEVEL", "info"),

		ShutdownGracePeriod: dur("ACME_SHUTDOWN_GRACE", 15*time.Second),
	}

	return cfg, errs.errOrNil()
}

// envFromMap turns a test environment into the two forms the loader wants: the
// "K=V" list the env provider reads, and the lookup the file resolution and
// placeholder expansion use. Both come from the same map, so the new loader
// and legacyLoad see one environment and nothing from the process's own.
func envFromMap(env map[string]string) ([]string, LookupFunc) {
	environ := make([]string, 0, len(env))
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}
	// koanf iterates the provider's output, but the order the environment is
	// presented in should not matter; sorting makes a failure reproducible.
	sort.Strings(environ)

	return environ, func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

// diffConfig lists the fields on which two Configs disagree. reflect.DeepEqual
// answers "not equal" and a %+v dump of forty fields is not much better; what
// a failure here has to say is WHICH setting moved.
// settingsTheLegacyLoaderNeverHad are Config fields added AFTER the frozen copy
// above, which therefore cannot produce a value for them and cannot be expected
// to.
//
// This is the mirror image of the note on legacyLoad about the four removed
// ACME_*_GROUP settings, and it is deliberately a short, named list rather than
// a general "ignore new fields" rule: the promise this test makes is that the
// settings that existed before the layered loader still load identically, and a
// rule that quietly excused every new field would let a REGRESSION in an old
// one hide behind a rename.
//
// projects_per_personal_account was in the file schema already, loaded and
// discarded (design C13). Task 3.5a carries it onto Config with a default of
// five, so the legacy loader — which knows nothing about it — reads zero.
var settingsTheLegacyLoaderNeverHad = map[string]string{
	"ProjectsPerPersonalAccount": "added by personal-and-team-accounts task 3.5a; the frozen loader predates the field",

	// account-invitations task group 4. The frozen loader predates inviting
	// anybody, so it has no opinion about how often one may.
	"InvitationsPerAccountPerHour": "added by account-invitations; the frozen loader predates the field",
	"InvitationsPerAddressPerDay":  "added by account-invitations; the frozen loader predates the field",
	"InvitationResendInterval":     "added by account-invitations; the frozen loader predates the field",
	"ActivationFreshInterval":      "added by domain-activation task 2.4; the frozen loader predates the field",

	// transactional-email task 3.2. The identity three of these carry has a
	// built-in default, so the frozen loader reporting "" for them is not a
	// disagreement about behaviour — it is a loader that predates the block.
	"MailFrom":                  "added by transactional-email; the frozen loader predates the field",
	"MailSender":                "added by transactional-email; the frozen loader predates the field",
	"MailReplyTo":               "added by transactional-email; the frozen loader predates the field",
	"MailFooter":                "added by transactional-email; the frozen loader predates the field",
	"MailSMTPAddr":              "added by transactional-email; the frozen loader predates the field",
	"MailSMTPUser":              "added by transactional-email; the frozen loader predates the field",
	"MailSMTPPass":              "added by transactional-email; the frozen loader predates the field",
	"ActivationSettledInterval": "added by domain-activation task 2.4; the frozen loader predates the field",
	"PostLogoutRedirectURL":     "added by the provider-logout change; the frozen loader predates the field",

	// domain-diagnostics task 4.5. The frozen loader predates certificate
	// observation entirely, so it has no opinion about any of these five.
	"ObservationEnabled":              "added by domain-diagnostics task 4.5; the frozen loader predates the field",
	"ObservationInterval":             "added by domain-diagnostics task 4.5; the frozen loader predates the field",
	"ObservationHandshakeTimeout":     "added by domain-diagnostics task 4.5; the frozen loader predates the field",
	"ObservationFullResolutionWindow": "added by domain-diagnostics task 4.5; the frozen loader predates the field",
	"ObservationRetention":            "added by domain-diagnostics task 4.5; the frozen loader predates the field",
}

func diffConfig(want, got Config) []string {
	var out []string
	t := reflect.TypeOf(Config{})
	w, g := reflect.ValueOf(want), reflect.ValueOf(got)
	for i := 0; i < t.NumField(); i++ {
		if _, skip := settingsTheLegacyLoaderNeverHad[t.Field(i).Name]; skip {
			continue
		}
		a, b := w.Field(i).Interface(), g.Field(i).Interface()
		if !reflect.DeepEqual(a, b) {
			out = append(out, fmt.Sprintf("%s: was %#v, now %#v", t.Field(i).Name, a, b))
		}
	}
	return out
}

// developmentEnv is the ACME_* half of development/.env as it stood before
// this change — the one environment we know an actual deployment used, and the
// reason this test exists rather than a table of invented strings. Note
// ACME_OIDC_GROUP_PREFIX: it shipped present-and-empty, which is the case that
// distinguishes "unset" from "set to empty" and the one a naive environment
// provider gets wrong.
func developmentEnv() map[string]string {
	return map[string]string{
		"ACME_ZONE":                     "dev.acmeservice.internal.",
		"ACME_SOA_PRIMARY_NS":           "ns1.dev.acmeservice.internal.",
		"ACME_SOA_HOSTMASTER":           "hostmaster@dev.acmeservice.internal",
		"ACME_NS_NAMES":                 "ns1.dev.acmeservice.internal.,ns2.dev.acmeservice.internal.",
		"ACME_DNS_LISTEN":               ":8053",
		"ACME_LISTEN":                   ":8080",
		"ACME_DB_ROLE":                  "acmeservice_app",
		"ACME_DB_PASSWORD":              "acmeservice_app",
		"ACME_DATABASE_URL":             "postgres://acmeservice_app:acmeservice_app@postgres:5432/acmeservice?sslmode=disable",
		"ACME_TRUSTED_PROXY_CIDRS":      "127.0.0.1/32,::1/128,172.28.0.0/24",
		"ACME_VERIFY_RESOLVER":          "172.28.0.53:53",
		"ACME_OIDC_ISSUER":              "http://auth.localhost/application/o/acmeservice-console/",
		"ACME_OIDC_CLIENT_ID":           "acmeservice-console",
		"ACME_OIDC_CLIENT_SECRET":       "dev-console-secret",
		"ACME_OIDC_REDIRECT_URL":        "http://console.localhost/api/v1/auth/callback",
		"ACME_OIDC_GROUPS_CLAIM":        "groups",
		"ACME_OIDC_GROUP_PREFIX":        "",
		"ACME_STAFF_HOST":               "admin.localhost",
		"ACME_STAFF_OIDC_CLIENT_ID":     "acmeservice-admin",
		"ACME_STAFF_OIDC_CLIENT_SECRET": "dev-admin-secret",
		"ACME_STAFF_OIDC_REDIRECT_URL":  "http://admin.localhost/api/admin/auth/callback",
		"ACME_LOG_LEVEL":                "debug",
	}
}

// everyVariableEnv sets every binding to a value distinguishable from
// both the default and every other setting's value, so a variable landing on
// the wrong path shows up as a mismatch rather than as two settings that
// happened to agree.
func everyVariableEnv() map[string]string {
	return map[string]string{
		"ACME_ZONE":                     "every.example.com.",
		"ACME_SOA_PRIMARY_NS":           "ns-primary.example.com.",
		"ACME_SOA_HOSTMASTER":           "hostmaster.example.com.",
		"ACME_SOA_SERIAL":               "2026082701",
		"ACME_SOA_REFRESH":              "91m",
		"ACME_SOA_RETRY":                "92m",
		"ACME_SOA_EXPIRE":               "93h",
		"ACME_SOA_MINIMUM":              "94s",
		"ACME_NS_NAMES":                 "ns1.example.com., ns2.example.com.",
		"ACME_CAA_ISSUER":               "letsencrypt.org,sectigo.com",
		"ACME_VIEW_REFRESH_INTERVAL":    "95s",
		"ACME_DNS_LISTEN":               ":5353",
		"ACME_LISTEN":                   ":9090",
		"ACME_SHUTDOWN_GRACE":           "96s",
		"ACME_DATABASE_URL":             "postgres://every:one@db:5432/acmeservice",
		"ACME_DB_ROLE":                  "every_app",
		"ACME_DB_PASSWORD":              "every_app_password",
		"ACME_MIGRATE_ON_START":         "false",
		"ACME_OIDC_ISSUER":              "https://idp.example.com/realms/every",
		"ACME_OIDC_CLIENT_ID":           "console-client",
		"ACME_OIDC_CLIENT_SECRET":       "console-secret",
		"ACME_OIDC_REDIRECT_URL":        "https://console.example.com/api/v1/auth/callback",
		"ACME_OIDC_GROUPS_CLAIM":        "roles",
		"ACME_OIDC_GROUP_PREFIX":        "/acme/",
		"ACME_SESSION_IDLE_TIMEOUT":     "97m",
		"ACME_SESSION_MAX_LIFETIME":     "98h",
		"ACME_STAFF_HOST":               "admin.example.com",
		"ACME_STAFF_OIDC_CLIENT_ID":     "staff-client",
		"ACME_STAFF_OIDC_CLIENT_SECRET": "staff-secret",
		"ACME_STAFF_OIDC_REDIRECT_URL":  "https://admin.example.com/api/admin/auth/callback",
		"ACME_CHALLENGE_TTL":            "99s",
		"ACME_CHALLENGE_LIFETIME":       "100m",
		"ACME_SWEEP_INTERVAL":           "101m",
		"ACME_INTERACTION_RETENTION":    "4320h",
		"ACME_TRUSTED_PROXY_CIDRS":      "10.0.0.0/8, 192.168.0.0/16",
		"ACME_VERIFY_RESOLVER":          "9.9.9.9:53",
		"ACME_ALIAS_USE_DOMAIN":         "true",
		"ACME_ALLOW_ACCOUNT_CREATION":   "TRUE",
		"ACME_LOG_LEVEL":                "warn",
	}
}

// TestNoFileMeansNothingMoved is task 5.1. Every case loads with no
// configuration file present and asserts the result is field-for-field what
// the pre-change loader produced from the same variables.
func TestNoFileMeansNothingMoved(t *testing.T) {
	requireNoBaseFile(t)

	cases := []struct {
		name string
		env  map[string]string
	}{
		// The environment-only deployment with nothing set at all: this is
		// the defaults, and it is the case where a default written twice
		// (Go and config.yaml) has the most room to disagree.
		{"nothing set", map[string]string{}},

		{"the development stack", developmentEnv()},
		{"every variable set", everyVariableEnv()},

		// Values that are legal for the old loader and awkward for the new
		// one: a list whose separators produce nothing, a boolean spelled the
		// way strconv accepts rather than the way YAML does, and settings
		// whose default is non-zero being driven to zero.
		{"awkward but legal", map[string]string{
			"ACME_NS_NAMES":              " , ,",
			"ACME_CAA_ISSUER":            "",
			"ACME_TRUSTED_PROXY_CIDRS":   "127.0.0.1/32,",
			"ACME_MIGRATE_ON_START":      "0",
			"ACME_ALIAS_USE_DOMAIN":      "1",
			"ACME_SOA_SERIAL":            "0",
			"ACME_CHALLENGE_TTL":         "0s",
			"ACME_SWEEP_INTERVAL":        "0",
			"ACME_INTERACTION_RETENTION": "4320h",
			"ACME_LOG_LEVEL":             "",
		}},

		// The environment carries more than this service's settings, and a
		// name that merely starts with ACME_ must reach nothing rather than
		// something approximate.
		{"unrelated variables", map[string]string{
			"ACME_ZONE":            "unrelated.example.com.",
			"ACME_CONFIG_FILE":     "",
			"ACME_PROFILES":        "",
			"ACME_NOT_A_SETTING":   "ignored",
			"ACME_OIDC_CLIENT":     "ignored",
			"ACME_STAFF":           "ignored",
			"HTTPREQ_ENDPOINT":     "http://acmeservice/present",
			"LEGO_DISABLE_CNAME":   "true",
			"POSTGRES_PASSWORD":    "not ours",
			"AUTHENTIK_SECRET_KEY": "not ours",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			environ, lookup := envFromMap(tc.env)

			want, wantErr := legacyLoad(lookup)
			got, gotErr := load(Options{Environ: environ, Lookup: lookup})

			if (wantErr == nil) != (gotErr == nil) {
				t.Fatalf("error disagreement:\n was: %v\n now: %v", wantErr, gotErr)
			}
			for _, d := range diffConfig(want, got) {
				t.Errorf("%s", d)
			}
		})
	}
}

// TestMalformedValueStillNamesTheVariable pins the other half of the old
// contract, which is not about the struct: a bad value is reported against the
// variable the operator set, every bad value is reported rather than the first
// one, and the setting still comes back with its default so the returned
// Config is usable. mapstructure would have said `cannot parse
// 'challenge.ttl'`, which names a file this deployment does not have.
func TestMalformedValueStillNamesTheVariable(t *testing.T) {
	requireNoBaseFile(t)

	env := map[string]string{
		"ACME_CHALLENGE_TTL":        "5 minutes",
		"ACME_MIGRATE_ON_START":     "yes please",
		"ACME_SOA_SERIAL":           "not-a-number",
		"ACME_SESSION_IDLE_TIMEOUT": "one hour",
	}
	environ, lookup := envFromMap(env)

	want, wantErr := legacyLoad(lookup)
	got, gotErr := load(Options{Environ: environ, Lookup: lookup})

	if wantErr == nil || gotErr == nil {
		t.Fatalf("expected both loaders to fail; was %v, now %v", wantErr, gotErr)
	}
	// The lines are compared as a set, not as a string: the old loader
	// accumulated in the order of a struct literal and the new one in the
	// order of the binding table, and which malformed setting is named first
	// is not something any caller can depend on.
	if w, g := errorLines(wantErr), errorLines(gotErr); !reflect.DeepEqual(w, g) {
		t.Errorf("reported errors differ\n was: %q\n now: %q", w, g)
	}
	for _, d := range diffConfig(want, got) {
		t.Errorf("after a malformed value, %s", d)
	}
}

func errorLines(err error) []string {
	var out []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" && line != "invalid configuration:" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// TestDevelopmentEnvVariablesAreStillRead is the failure this change must not
// have: a variable an operator sets that quietly reaches nothing. Equality
// with the old loader cannot catch it on its own, because a variable the old
// loader never read either — ACME_DB_ROLE, consumed by postgres-init — agrees
// with it by producing nothing on both sides.
//
// So this reads development/.env itself, and requires every ACME_* name in it
// to be a binding that resolves to a path the schema has — except the few that
// are deliberately not service settings at all.
func TestDevelopmentEnvVariablesAreStillRead(t *testing.T) {
	names := acmeNamesIn(t, "../../../development/.env")
	if len(names) == 0 {
		t.Fatal("no ACME_* variables found in development/.env; the path is probably wrong")
	}

	bound := map[string]string{}
	for _, b := range envBindings {
		bound[b.env] = b.path
	}
	sc, aliases := envSchema(), envAliases()

	for _, name := range names {
		if notASetting[name] {
			continue
		}
		path, ok := bound[name]
		if !ok {
			t.Errorf("%s is set in development/.env and is in no binding", name)
			continue
		}
		if got := sc.envMapper(EnvPrefix, aliases)(name); got != path {
			t.Errorf("%s reaches %q, want %q", name, got, path)
		}
	}
}

// notASetting is the short list of ACME_* names in development/.env that this
// process deliberately never reads, with the reason each one is there.
//
// It is a list rather than a prefix rule on purpose: the failure this test
// exists to catch is a setting that quietly reaches nothing, and a rule that
// exempted a whole shape of name would eventually exempt a real one.
var notASetting = map[string]bool{
	// Read by cmd/acmeservice's adminctl dispatch, not by config.Load: it names
	// the account the CLI acts in (design C10), which is a per-invocation target
	// rather than a property of the running service. Putting it in the schema
	// would also put it on the effective-configuration screen, where it would
	// describe nothing about the process serving that screen.
	"ACME_ADMINCTL_ACCOUNT": true,
}

// acmeNamesIn returns the ACME_* variable names assigned in a .env file. It is
// deliberately dumber than a .env parser: names are all this needs, and the
// values are quoted, interpolated and commented in ways that are not worth
// modelling.
func acmeNamesIn(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	defer f.Close()

	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, ok := strings.Cut(line, "=")
		if ok && strings.HasPrefix(name, EnvPrefix) {
			out = append(out, name)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return out
}

// requireNoBaseFile makes the premise of task 5.1 an assertion rather than an
// assumption. These tests load with Options.File empty, which means the
// conventional ./config.yaml — and a config.yaml appearing in this package's
// directory would make them pass while testing something else entirely.
func requireNoBaseFile(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(DefaultFile); err == nil {
		t.Fatalf("%s exists in the test's working directory; these tests must load no file", DefaultFile)
	}
}
