package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// shippedFile is the committed base file, from this package's directory.
const shippedFile = "../../../config.yaml"

// TestShippedFileRestatesTheBuiltInDefaults is what keeps task 3.1 honest.
//
// The defaults exist twice on purpose: in Go, because an env-only deployment
// never reads a file and the production image ships none, and in config.yaml,
// because the whole point of the file is that an operator can see them without
// reading the source. Two copies drift, so this asserts they are one value:
// loading the shipped file with an empty environment must produce exactly the
// configuration that loading nothing at all produces.
//
// It fails on any default written in one place and not the other, and on any
// placeholder whose fallback was mistyped — `${ACME_LISTEN::8080}` losing a
// colon reads as an empty default and shows up here rather than in production.
func TestShippedFileRestatesTheBuiltInDefaults(t *testing.T) {
	empty := func(string) (string, bool) { return "", false }

	builtIn, err := load(Options{Environ: []string{}, Lookup: empty})
	if err != nil {
		t.Fatalf("loading with no file at all: %v", err)
	}

	fromFile, err := load(Options{File: shippedFile, Environ: []string{}, Lookup: empty})
	if err != nil {
		t.Fatalf("loading %s: %v", shippedFile, err)
	}

	if !reflect.DeepEqual(builtIn, fromFile) {
		t.Errorf("%s does not restate the built-in defaults\n built-in: %+v\n     file: %+v",
			shippedFile, builtIn, fromFile)
	}
}

// TestShippedFileCommitsNoSecret pins task 3.3. A fallback on a secret is a
// secret in the repository the moment someone fills it in, and the failure
// mode is silent: the service starts, with a password nobody meant to use.
// Written without one, an unset secret arrives empty and Validate refuses to
// start.
func TestShippedFileCommitsNoSecret(t *testing.T) {
	// Every placeholder resolves to its fallback, so a secret that has one
	// would show up here as a non-empty value.
	cfg, err := load(Options{
		File:    shippedFile,
		Environ: []string{},
		Lookup:  func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("loading %s: %v", shippedFile, err)
	}

	for _, s := range []struct{ name, value string }{
		{"ACME_OIDC_CLIENT_SECRET", cfg.OIDCClientSecret},
		{"ACME_STAFF_OIDC_CLIENT_SECRET", cfg.StaffOIDCClientSecret},
		{"ACME_DATABASE_URL", cfg.DatabaseURL},
	} {
		if s.value != "" {
			t.Errorf("%s has a fallback in %s: %q", s.name, shippedFile, s.value)
		}
	}
}

// TestEveryVariableReachesItsPath walks the binding table variable by variable,
// sets that one alone, and checks it arrives — which is the whole of task 2.4.
// A path that no longer exists in the schema, or an alias the derivation stops
// producing, silently maps to nothing, and nothing is exactly what a wrong
// mapping looks like from the outside.
func TestEveryVariableReachesItsPath(t *testing.T) {
	sc := envSchema()
	aliases := envAliases()

	for _, b := range envBindings {
		if _, ok := sc.flat[flatten(b.path)]; !ok {
			t.Errorf("%s: %q is not a leaf in the schema", b.env, b.path)
			continue
		}
		got := sc.envMapper(EnvPrefix, aliases)(b.env)
		if got != b.path {
			t.Errorf("%s maps to %q, want %q", b.env, got, b.path)
		}
	}
}

// TestShippedFilesQuotePlaceholdersSingly pins the quoting convention, which is
// a correctness rule and not a style one. Substitution happens before the parse
// (G2), so an injected value lands inside whatever quotes the file wrote. Double
// quotes are a YAML escape context: a password containing \t, \n, \\ or \u is
// altered and the service starts anyway, which is the one failure this design
// cannot report. Single quotes escape nothing, and the only character that needs
// care is the quote itself, which fails the parse loudly.
func TestShippedFilesQuotePlaceholdersSingly(t *testing.T) {
	for _, path := range []string{shippedFile, "../../../development/config-development.yaml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.Contains(line, `"${`) {
				t.Errorf("%s:%d double-quotes a placeholder, which processes backslash escapes in whatever is substituted: %s",
					path, i+1, strings.TrimSpace(line))
			}
		}
	}
}
