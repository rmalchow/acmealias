package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// loaderTestConfig stands in for the real schema, which task group 2 writes.
// It has the shapes that matter to the loader: a nested block, a duration, an
// integer, a boolean and a list.
type loaderTestConfig struct {
	Zone struct {
		Name    string   `koanf:"name"`
		NSNames []string `koanf:"ns_names"`
	} `koanf:"zone"`
	Listeners struct {
		DNS  string `koanf:"dns"`
		HTTP string `koanf:"http"`
		Port int    `koanf:"port"`
	} `koanf:"listeners"`
	Challenge struct {
		TTL            time.Duration `koanf:"ttl"`
		AliasUseDomain bool          `koanf:"alias_use_domain"`
	} `koanf:"challenge"`
	Log struct {
		Level string `koanf:"level"`
	} `koanf:"log"`
}

// Defaults() is on the value receiver because collectDefaults finds it through
// reflect.New.
type defaultedBlock struct {
	Level string `koanf:"level"`
}

func (defaultedBlock) Defaults() map[string]any { return map[string]any{"level": "info"} }

type defaultedConfig struct {
	Log defaultedBlock `koanf:"log"`
}

// writeFiles drops files into a fresh temp dir and returns the base path.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return filepath.Join(dir, DefaultFile)
}

// noEnv is the empty environment: tests opt into variables explicitly, so the
// developer's own ACME_* settings cannot reach a test.
func noEnv(opts Options) Options {
	if opts.Environ == nil {
		opts.Environ = []string{}
	}
	if opts.Lookup == nil {
		opts.Lookup = func(string) (string, bool) { return "", false }
	}
	return opts
}

func mustLoad(t *testing.T, opts Options) *loaderTestConfig {
	t.Helper()
	cfg, _, err := loadWithSources[loaderTestConfig](noEnv(opts))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

const loaderBaseYAML = `
zone:
  name: base.example.com.
  ns_names: [ns1.acmealias.com, ns2.acmealias.com]
listeners:
  dns: ":53"
  http: ":8080"
log:
  level: info
`

// 5.2, first direction: a file value beats a Go default.
func TestFileBeatsDefault(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: "log:\n  level: debug\n"})

	cfg, _, err := loadWithSources[defaultedConfig](noEnv(Options{File: base}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("log.level = %q, want the file's debug over the block default", cfg.Log.Level)
	}

	// And with no file value the block default is what survives.
	bare := writeFiles(t, map[string]string{DefaultFile: "zone:\n  name: x\n"})
	cfg, _, err = loadWithSources[defaultedConfig](noEnv(Options{File: bare}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("log.level = %q, want the block default info", cfg.Log.Level)
	}
}

// 5.2, second direction and the whole point of G1: the environment beats the
// file, so a deployment configured entirely by ACME_* keeps behaving as it did.
func TestEnvironmentBeatsFile(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: loaderBaseYAML})

	cfg := mustLoad(t, Options{
		File:    base,
		Environ: []string{"ACME_LISTENERS_HTTP=:9999", "ACME_ZONE_NAME=env.example.com."},
	})

	if cfg.Listeners.HTTP != ":9999" {
		t.Errorf("listeners.http = %q, want the environment's :9999", cfg.Listeners.HTTP)
	}
	if cfg.Zone.Name != "env.example.com." {
		t.Errorf("zone.name = %q, want the environment's value", cfg.Zone.Name)
	}
	// A key the environment says nothing about still comes from the file.
	if cfg.Listeners.DNS != ":53" {
		t.Errorf("listeners.dns = %q, want the file's :53", cfg.Listeners.DNS)
	}
}

// The overlay sits between the two: above the base file, below the environment.
func TestOverlayBeatsBaseAndEnvironmentBeatsOverlay(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile:           loaderBaseYAML,
		"config-staging.yaml": "listeners:\n  http: \":7000\"\nlog:\n  level: warn\n",
	})

	cfg := mustLoad(t, Options{
		File:     base,
		Profiles: []string{"staging"},
		Environ:  []string{"ACME_LOG_LEVEL=error"},
	})

	if cfg.Listeners.HTTP != ":7000" {
		t.Errorf("listeners.http = %q, want the overlay's :7000", cfg.Listeners.HTTP)
	}
	if cfg.Log.Level != "error" {
		t.Errorf("log.level = %q, want the environment's error", cfg.Log.Level)
	}
}

// 1.6 / G3: a list replaces the list beneath it. Merging would publish three
// nameservers, one of them production's, and nobody wrote that list.
func TestOverlayListReplacesRatherThanMerges(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile:           loaderBaseYAML,
		"config-staging.yaml": "zone:\n  ns_names: [ns1.staging.acmealias.com]\n",
	})

	cfg := mustLoad(t, Options{File: base, Profiles: []string{"staging"}})

	want := []string{"ns1.staging.acmealias.com"}
	if !reflect.DeepEqual(cfg.Zone.NSNames, want) {
		t.Fatalf("zone.ns_names = %v, want exactly %v", cfg.Zone.NSNames, want)
	}
	// The rest of the base block is still there: maps merge, lists do not.
	if cfg.Zone.Name != "base.example.com." {
		t.Errorf("zone.name = %q, want the base file's value", cfg.Zone.Name)
	}
}

// 5.3, first half: an overlay that does not exist changes nothing and is
// recorded as absent rather than failing.
func TestAbsentOverlayIsNotAnError(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: loaderBaseYAML})

	cfg, sources, err := loadWithSources[loaderTestConfig](noEnv(Options{
		File:     base,
		Profiles: []string{"nosuchprofile"},
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listeners.HTTP != ":8080" {
		t.Errorf("listeners.http = %q, want the base file's :8080", cfg.Listeners.HTTP)
	}

	overlay := filepath.Join(filepath.Dir(base), "config-nosuchprofile.yaml")
	var found bool
	for _, s := range sources {
		if s.Ref == overlay {
			found = true
			if s.Found {
				t.Errorf("source %v reported as present", s)
			}
			if !strings.Contains(s.String(), "(absent)") {
				t.Errorf("Source.String() = %q, want it to say the file was absent", s.String())
			}
		}
	}
	if !found {
		t.Errorf("sources %v do not mention the overlay at all", sources)
	}
}

// 5.3, second half: a base file somebody configured and that is not there is a
// broken deployment, and starting on defaults nobody chose is worse than not
// starting (G4).
func TestAbsentConfiguredBaseIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.yaml")

	// Configured programmatically.
	if _, _, err := loadWithSources[loaderTestConfig](noEnv(Options{File: missing})); err == nil {
		t.Fatal("load succeeded with an explicitly configured base file that does not exist")
	}

	// Configured through the environment — the same intention, stated by an
	// operator rather than by a caller.
	_, _, err := loadWithSources[loaderTestConfig](noEnv(Options{
		Lookup: func(name string) (string, bool) {
			if name == "ACME_CONFIG_FILE" {
				return missing, true
			}
			return "", false
		},
	}))
	if err == nil {
		t.Fatal("load succeeded with ACME_CONFIG_FILE naming a file that does not exist")
	}
}

// The environment-only deployment of G1: no file, nothing configured, no error.
func TestAbsentConventionalBaseIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // so the conventional ./config.yaml is the one that is missing

	cfg, _, err := loadWithSources[loaderTestConfig](noEnv(Options{
		Environ: []string{"ACME_ZONE_NAME=envonly.example.com."},
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Zone.Name != "envonly.example.com." {
		t.Errorf("zone.name = %q, want the environment's value", cfg.Zone.Name)
	}
}

// 5.4 / G2: the placeholder is expanded before the YAML is parsed, so the value
// keeps its natural type. Expanded after parsing, every one of these would be a
// string and every consumer would have to coerce or fail.
func TestPlaceholdersKeepTheirYAMLType(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: `
listeners:
  port: ${ACME_PORT:8080}
challenge:
  ttl: ${ACME_CHALLENGE_TTL:45s}
  alias_use_domain: ${ACME_ALIAS_USE_DOMAIN:true}
zone:
  ns_names: ${ACME_NS:[ns1.example.com, ns2.example.com]}
`})

	cfg := mustLoad(t, Options{File: base})

	if cfg.Listeners.Port != 8080 {
		t.Errorf("listeners.port = %d, want 8080", cfg.Listeners.Port)
	}
	if cfg.Challenge.TTL != 45*time.Second {
		t.Errorf("challenge.ttl = %v, want 45s", cfg.Challenge.TTL)
	}
	if !cfg.Challenge.AliasUseDomain {
		t.Error("challenge.alias_use_domain = false, want true")
	}
	want := []string{"ns1.example.com", "ns2.example.com"}
	if !reflect.DeepEqual(cfg.Zone.NSNames, want) {
		t.Errorf("zone.ns_names = %v, want %v", cfg.Zone.NSNames, want)
	}
}

// The same property at the byte level, where it is unambiguous: the integer
// never passes through a string.
func TestPlaceholderExpansionIsPreParse(t *testing.T) {
	got := string(resolvePlaceholders([]byte("port: ${PORT:8080}\n"), func(string) (string, bool) { return "", false }))
	if got != "port: 8080\n" {
		t.Fatalf("resolvePlaceholders = %q, want %q", got, "port: 8080\n")
	}
}

func TestPlaceholderForms(t *testing.T) {
	lookup := func(name string) (string, bool) {
		switch name {
		case "SET":
			return "from-env", true
		case "EMPTY":
			return "", true
		}
		return "", false
	}

	cases := []struct{ in, want string }{
		{"${SET}", "from-env"},
		{"${SET:fallback}", "from-env"},  // a set variable always beats the default
		{"${SET:-fallback}", "from-env"}, // both default forms behave alike
		{"${UNSET:fallback}", "fallback"},
		{"${UNSET:-fallback}", "fallback"},
		{"${UNSET}", ""},                      // no default: empty, which YAML reads as null
		{"${EMPTY:fallback}", ""},             // set-but-empty is still set
		{"${UNSET:redis:6379}", "redis:6379"}, // a default may itself contain a colon
		{"$${UNSET}", "${UNSET}"},             // escaped to a literal dollar-brace
		{"pass$${word}", "pass${word}"},
	}
	for _, c := range cases {
		if got := string(resolvePlaceholders([]byte(c.in), lookup)); got != c.want {
			t.Errorf("resolvePlaceholders(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// G5: the mapping is by schema, so a name that could split four ways lands on
// the one path that exists — and a name that names no path at all is skipped
// rather than invented.
func TestEnvironmentMappingIsBySchema(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: loaderBaseYAML})

	cfg := mustLoad(t, Options{
		File: base,
		Environ: []string{
			"ACME_CHALLENGE_ALIAS_USE_DOMAIN=true",
			"ACME_NOT_A_CONFIG_KEY=noise",
			"UNRELATED_LISTENERS_HTTP=:1",
			"PATH=/usr/bin",
		},
	})

	if !cfg.Challenge.AliasUseDomain {
		t.Error("challenge.alias_use_domain = false, want the environment's true")
	}
	if cfg.Listeners.HTTP != ":8080" {
		t.Errorf("listeners.http = %q, want the file's value: an unprefixed variable is not ours", cfg.Listeners.HTTP)
	}
}

// A variable naming a whole block reaches nothing. The schema makes a block
// addressable, because a file can set one, but an environment value is always
// a scalar: mapping ACME_LOG onto `log` produces `'log' expected a map, got
// 'string'` and the service does not start — over a variable it does not even
// have. Silently ignoring it is the least wrong of the three options, and the
// alias table is where a variable that really does mean a block, as ACME_ZONE
// means zone.name, says so.
func TestVariableNamingAWholeBlockIsIgnored(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: loaderBaseYAML})

	cfg := mustLoad(t, Options{
		File:    base,
		Environ: []string{"ACME_LOG=verbose", "ACME_ZONE=stray.example.com."},
	})

	if cfg.Log.Level != "info" {
		t.Errorf("log.level = %q, want the file's info", cfg.Log.Level)
	}
	if cfg.Zone.Name != "base.example.com." {
		t.Errorf("zone.name = %q, want the file's value: an unaliased ACME_ZONE names the block, not a leaf", cfg.Zone.Name)
	}
}

// A variable whose name does not decompose into its own path reaches it through
// an alias, which is how G5's "rename nothing" is paid for.
func TestEnvAliasReachesAPathTheNameDoesNotDescribe(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: loaderBaseYAML})

	cfg := mustLoad(t, Options{
		File:       base,
		EnvAliases: map[string]string{"ACME_DNS_LISTEN": "listeners.dns"},
		Environ:    []string{"ACME_DNS_LISTEN=:5353"},
	})

	if cfg.Listeners.DNS != ":5353" {
		t.Fatalf("listeners.dns = %q, want the aliased variable's :5353", cfg.Listeners.DNS)
	}
}

// A comma-separated environment value is a list, as it has always been. Left to
// the unmarshaller it would become one nameserver called "ns1...,ns2...".
func TestEnvironmentListsSplitOnCommas(t *testing.T) {
	base := writeFiles(t, map[string]string{DefaultFile: loaderBaseYAML})

	cfg := mustLoad(t, Options{
		File:    base,
		Environ: []string{"ACME_ZONE_NS_NAMES=ns1.example.com, ns2.example.com"},
	})

	want := []string{"ns1.example.com", "ns2.example.com"}
	if !reflect.DeepEqual(cfg.Zone.NSNames, want) {
		t.Fatalf("zone.ns_names = %v, want %v", cfg.Zone.NSNames, want)
	}
}

// G6: the layers are reported, in the order they applied, so the operator does
// not have to guess which overlay was read.
func TestSourcesReportEveryLayerInOrder(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile:       loaderBaseYAML,
		"config-dev.yaml": "log:\n  level: debug\n",
	})

	_, sources, err := loadWithSources[loaderTestConfig](noEnv(Options{File: base, Profiles: []string{"dev"}}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var kinds []string
	for _, s := range sources {
		kinds = append(kinds, s.Kind)
	}
	want := []string{"defaults", "file", "file", "env"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("source kinds = %v, want %v", kinds, want)
	}
	if last := sources[len(sources)-1]; last.Ref != "ACME_*" {
		t.Errorf("last source = %v, want the environment on top", last)
	}
}

func TestProfilesComeFromTheEnvironmentToo(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile:       loaderBaseYAML,
		"config-dev.yaml": "log:\n  level: debug\n",
	})

	cfg := mustLoad(t, Options{
		File: base,
		Lookup: func(name string) (string, bool) {
			if name == "ACME_PROFILES" {
				return "dev", true
			}
			return "", false
		},
	})

	if cfg.Log.Level != "debug" {
		t.Fatalf("log.level = %q, want the dev overlay's debug", cfg.Log.Level)
	}
}

// An overlay written the house way — every placeholder quoted (G2, task 3.2) —
// must not DESTROY the layer beneath it when the variable is unset. Quoted,
// `${UNSET}` expands to an empty string rather than to a null, and merging that
// leaves the operator with a value nobody wrote and no file to find it in.
func TestUnresolvedPlaceholderFallsThroughToTheLayerBeneath(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile:           loaderBaseYAML,
		"config-staging.yaml": "zone:\n  name: '${UNSET_ZONE_NAME}'\nlog:\n  level: '${UNSET_LOG_LEVEL}'\n",
	})

	cfg := mustLoad(t, Options{File: base, Profiles: []string{"staging"}})

	if cfg.Zone.Name != "base.example.com." {
		t.Errorf("zone.name = %q, want the base file's value: an unset placeholder is not a value", cfg.Zone.Name)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log.level = %q, want the base file's info", cfg.Log.Level)
	}
}

// The same rule inside one file: a block whose every key resolved to nothing
// contributes nothing, rather than a block of empty strings.
func TestEmptyValuesInAFileDoNotOverrideDefaults(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile: "log:\n  level: \"\"\nzone:\n  name: kept.example.com.\n",
	})

	cfg, _, err := loadWithSources[defaultedConfig](noEnv(Options{File: base}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("log.level = %q, want the block default info", cfg.Log.Level)
	}
}

// Environ and Lookup are one environment (see normalize): a caller that injects
// only the overlay must not have its base file, its profiles and its
// placeholders resolved from the developer's own process.
func TestInjectingEnvironAloneAlsoInjectsTheLookup(t *testing.T) {
	base := writeFiles(t, map[string]string{
		DefaultFile:       "zone:\n  name: '${WHO:nobody}'\n",
		"config-dev.yaml": "log:\n  level: debug\n",
	})

	cfg, _, err := loadWithSources[loaderTestConfig](Options{Environ: []string{
		"ACME_CONFIG_FILE=" + base,
		"ACME_PROFILES=dev",
		"WHO=someone",
	}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Zone.Name != "someone" {
		t.Errorf("zone.name = %q, want the injected WHO: placeholders read the injected environment", cfg.Zone.Name)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %q, want the overlay named by the injected ACME_PROFILES", cfg.Log.Level)
	}
}
