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
