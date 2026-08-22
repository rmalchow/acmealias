package dnsname

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

// ValidateLabel checks an alias label: one DNS label of lowercase letters,
// digits and hyphens, not beginning or ending with a hyphen, at most 63
// octets. Unlike a name label it may not carry a leading underscore — the
// alias label is a name we publish, not one we receive.
func ValidateLabel(label string) error {
	if strings.Contains(label, ".") {
		return fmt.Errorf("%w: label %q contains a dot; an alias label is a single label", ErrInvalid, label)
	}
	if len(label) > maxLabelOctets {
		return fmt.Errorf("%w: label %q is %d octets, limit is %d", ErrInvalid, label, len(label), maxLabelOctets)
	}
	return validateLDH(label, label)
}

// GenerateLabel draws an alias label at random: 16 characters of lowercase
// base32, 80 bits from crypto/rand (design A1).
//
// Random and not a hash of the challenge name. A hash is unreadable but still
// deterministic, so anyone can compute a domain's label and ask our
// nameservers whether it answers — the enumeration oracle this change exists
// to close. Only unpredictability closes it.
//
// Ten bytes encode to exactly 16 base32 characters with no padding, and the
// base32 alphabet lowercased is letters and digits only, so the result is a
// valid DNS label by construction: never empty, never over 63 octets, never
// hyphenated at either end. No trimming or fallback is needed.
func GenerateLabel() (string, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating an alias label: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.EncodeToString(b[:])), nil
}

// SuggestLabel proposes an alias label for a challenge name:
// `_acme-challenge.example.com` becomes `example-com`. It is used when
// ACME_ALIAS_USE_DOMAIN is set; the inventory disambiguates a collision with a
// numeric suffix, so this only has to produce something valid and legible.
func SuggestLabel(challengeName string) string {
	base := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(challengeName, ".")), ChallengePrefix)

	var b strings.Builder
	for i := 0; i < len(base); i++ {
		c := base[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			// Dots, underscores, wildcards and anything else become a hyphen.
			b.WriteByte('-')
		}
	}

	label := b.String()
	if len(label) > maxLabelOctets {
		label = label[:maxLabelOctets]
	}
	label = strings.Trim(label, "-")
	if label == "" {
		// ponytail: no sensible label could be derived (a name of nothing but
		// punctuation); the caller's collision suffix makes this unique.
		return "alias"
	}
	return label
}
