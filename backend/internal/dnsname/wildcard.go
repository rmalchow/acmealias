package dnsname

import (
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Warning is a note about an accepted name — something the operator should see
// but which does not stop the creation.
type Warning string

const wildcardPrefix = "*."

// ValidateDomain normalizes and validates a domain name an operator supplied,
// including wildcards. It implements design D4's table:
//
//	*.example.com          accepted
//	example.com            accepted (shares an alias with *.example.com)
//	*.*.example.com        rejected — one wildcard label only
//	foo.*.example.com      rejected — the wildcard must be leftmost
//	*                      rejected
//	*.com, *.co.uk         rejected — the wildcard sits at a public suffix
//
// The warning list is currently always empty: the one warning D4 defines
// ("already covered by an existing wildcard") needs the rest of the load balancer's
// domains and is raised by the inventory service, not here.
func ValidateDomain(name string) (Normalized, []Warning, error) {
	trimmed := strings.ToLower(strings.TrimSuffix(name, "."))
	if trimmed == "" {
		return "", nil, fmt.Errorf("%w: name is empty", ErrInvalid)
	}

	if trimmed == "*" {
		return "", nil, fmt.Errorf("%w: %q is not a domain name", ErrInvalid, trimmed)
	}

	base := trimmed
	wildcard := strings.HasPrefix(trimmed, wildcardPrefix)
	if wildcard {
		base = strings.TrimPrefix(trimmed, wildcardPrefix)
	}
	// Any remaining `*` label is either a second wildcard or a wildcard that is
	// not leftmost; both are rejected, and so is a bare `*` (whose base is empty).
	for _, label := range strings.Split(base, ".") {
		if label == "*" {
			return "", nil, fmt.Errorf("%w: %q — a wildcard label is allowed only as the leftmost label, and only once", ErrInvalid, trimmed)
		}
	}
	normalizedBase, err := Normalize(base)
	if err != nil {
		return "", nil, err
	}
	if wildcard {
		if suffix, _ := publicsuffix.PublicSuffix(normalizedBase); suffix == normalizedBase {
			return "", nil, fmt.Errorf("%w: %q would cover the whole public suffix %q", ErrInvalid, trimmed, suffix)
		}
	}

	full := normalizedBase
	if wildcard {
		full = wildcardPrefix + normalizedBase
		if len(full) > maxNameOctets {
			return "", nil, fmt.Errorf("%w: name %q is %d octets, limit is %d", ErrInvalid, full, len(full), maxNameOctets)
		}
	}
	return Normalized(full), nil, nil
}

// Covers reports whether host is a name the wildcard pattern matches: exactly
// one label beneath it, no more.
//
//	*.example.com, www.example.com    -> true
//	*.example.com, a.b.example.com    -> false — two labels down, and a TLS
//	                                     client would conclude the same
//	*.example.com, example.com        -> false — the apex is a separate name
//	                                     with its own row and its own switch
//	example.com,   www.example.com    -> false — not a wildcard, covers nothing
//
// It lives here rather than in the inventory service so that it sits beside the
// rules it has to agree with (design G8). A hostname accepted by a second
// matcher and rejected by ValidateDomain is a row nothing can ever observe.
//
// One label and not "any depth", which is what a TLS client does: RFC 6125 § 6.4.3
// matches a wildcard against exactly one label, so `*.example.com` presented for
// `a.b.example.com` fails verification in every browser. Accepting it here would
// let somebody store a test hostname whose observation can only ever come back
// as "does not cover the name asked for".
//
// Both arguments are lowercased and de-rooted the way the rest of this package
// normalizes, but neither is otherwise validated: a caller holding an
// unvalidated pattern should put it through ValidateDomain first.
func Covers(pattern, host string) bool {
	p := strings.ToLower(strings.TrimSuffix(pattern, "."))
	h := strings.ToLower(strings.TrimSuffix(host, "."))

	base, isWildcard := strings.CutPrefix(p, wildcardPrefix)
	if !isWildcard || base == "" {
		return false
	}
	// A wildcard does not cover another wildcard — including itself. Without
	// this, `*.example.com` would "cover" `*.example.com`, since `*` is a single
	// label like any other, and a wildcard would be storable as its own TLS test
	// hostname: the one name that cannot be one (G8).
	if strings.Contains(h, "*") {
		return false
	}
	label, found := strings.CutSuffix(h, "."+base)
	// A label of its own: non-empty, and with no dot left in it — the dot is
	// what separates "one label beneath" from "several".
	return found && label != "" && !strings.Contains(label, ".")
}

// Counterpart returns the other form of a name: the wildcard of a name that is
// not one, the base name of a name that is.
//
//	example.com       -> *.example.com
//	*.example.com     -> example.com
//	shop.example.com  -> *.shop.example.com
//
// This is derivation only. The result is a candidate, not a valid name: the
// wildcard rules are not inherited from the base, so `co.uk` yields `*.co.uk`
// and a 252-octet name yields one two octets over the limit. Every caller
// SHALL put the result through ValidateDomain before using it (design B2, B3).
func Counterpart(name string) string {
	n := strings.ToLower(strings.TrimSuffix(name, "."))
	if base, found := strings.CutPrefix(n, wildcardPrefix); found {
		return base
	}
	return wildcardPrefix + n
}
