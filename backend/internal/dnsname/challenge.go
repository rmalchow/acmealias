package dnsname

import (
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// ChallengePrefix is the label ACME validates under.
const ChallengePrefix = "_acme-challenge."

// ChallengeName derives the name a CA looks up for a domain: the leading
// wildcard label is stripped, then `_acme-challenge.` is prefixed. A wildcard
// and its base therefore derive the *same* challenge name — the reason aliases
// are their own entity (design D4).
//
// The argument is expected to have come from ValidateDomain; case and a
// trailing dot are tolerated anyway so no caller can produce a second spelling.
func ChallengeName(domain string) string {
	d := strings.ToLower(strings.TrimSuffix(domain, "."))
	return ChallengePrefix + strings.TrimPrefix(d, wildcardPrefix)
}

// RegistrableDomain returns the name one label below the public suffix —
// `example.com` for `shop.example.com`, `*.example.com` and `example.com`
// alike. CAA records are placed here, and only here, because CAA is inherited
// and the nearest ancestor wins (design D5).
func RegistrableDomain(name string) (string, error) {
	base, err := Normalize(strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(name, ".")), wildcardPrefix))
	if err != nil {
		return "", err
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(base)
	if err != nil {
		return "", fmt.Errorf("%w: %q has no registrable domain: %v", ErrInvalid, base, err)
	}
	return registrable, nil
}
