package config

import (
	"regexp"
	"strings"
)

// placeholderRE matches ${VAR}, ${VAR:default} and ${VAR:-default}. The default
// part may contain anything except a closing brace — a colon included, which is
// what lets ${ACME_VERIFY_RESOLVER:127.0.0.1:53} and ${ACME_OIDC_ISSUER:https://idp}
// mean what they look like.
var placeholderRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-?([^}]*))?\}`)

// resolvePlaceholders expands ${VAR} / ${VAR:default} references in raw config
// bytes, before the YAML is parsed. Resolving pre-parse rather than on the
// decoded values is a typing decision, not a style one (G2): with
// `port: ${ACME_PORT:8080}` YAML sees `port: 8080` and the value is an integer,
// where substituting into the decoded string would leave it the string "8080"
// and every consumer would have to coerce or fail. Booleans, durations and
// lists have the same problem.
//
// A doubled dollar sign escapes the sequence: $${NOT_A_VAR} yields the literal
// ${NOT_A_VAR}. A configuration file that cannot express a dollar-brace cannot
// configure a service whose password contains one.
//
// A set variable always beats an inline default — the default is a fallback,
// not an override. An unset variable with no default expands to nothing, which
// leaves either a null (unquoted) or an empty string (quoted, which is what the
// shipped files write, G2). Neither reaches the merge: loadFile prunes an
// absent value so the key falls back to whatever an earlier layer provided,
// which is the only reading of "${VAR}" in an overlay that is not a trap. A
// secret written as ${ACME_OIDC_CLIENT_SECRET} with no default therefore
// reaches Validate empty, and Validate is what refuses to start.
//
// Quote the substituted value in the file, always, and prefer SINGLE quotes.
// Double quotes are a YAML escape context: a password containing \t, \n, \\ or
// \u comes out ALTERED and the service starts anyway, which is the one failure
// mode this design does not otherwise have — everything else about injecting a
// hostile value into a document fails at parse time. Inside single quotes there
// are no escapes; the only special character is the quote itself, and a value
// carrying one breaks the parse loudly instead.
func resolvePlaceholders(raw []byte, lookup LookupFunc) []byte {
	// The sentinel contains NUL so that it cannot be forged: a printable
	// marker could be typed into a config file and round-tripped back into a
	// literal ${ that the regex never saw.
	const escape = "\x00cfgesc\x00"
	s := strings.ReplaceAll(string(raw), "$${", escape)

	s = placeholderRE.ReplaceAllStringFunc(s, func(m string) string {
		g := placeholderRE.FindStringSubmatch(m)
		if v, ok := lookup(g[1]); ok {
			return v
		}
		return g[2] // "" when no default was given
	})

	return []byte(strings.ReplaceAll(s, escape, "${"))
}
