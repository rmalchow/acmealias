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

// GenerateLabel draws an alias label at random: 10 characters of lowercase
// base32, 48 bits from crypto/rand (design A1).
//
// Random and not a hash of the challenge name. A hash is unreadable but still
// deterministic, so anyone can compute a domain's label and ask our
// nameservers whether it answers — the enumeration oracle this change exists
// to close. Only unpredictability closes it.
//
// IT WAS 80 BITS AND 16 CHARACTERS. Six of those characters were doing nothing
// a customer benefits from: this string is typed into a DNS provider's form by
// hand, read back off a screen to check it, and quoted in support threads, and
// every character is one more chance to transpose two of them. 48 bits is
// enough for both jobs it has. Guessing: 2^48 is 281 trillion, and a guess buys
// an attacker the ability to publish a TXT record under a name nobody has
// delegated to them, which is worth nothing without the matching delegation.
// Collision: at a hundred thousand aliases the birthday probability that ANY
// two collide is about 1 in 56 million, and a collision is not a fault anyway —
// it is a retry, handled twice over, before the insert by the caller and by the
// database's unique index if two transactions race.
//
// Six bytes encode to exactly 10 base32 characters WITH PADDING SUPPRESSED, and
// suppressing it is load-bearing: StdEncoding would append six '=' characters,
// which are not legal in a DNS label. The lowercased base32 alphabet is letters
// and digits only, so what comes back is a valid label by construction — never
// empty, never over 63 octets, never hyphenated at either end.
func GenerateLabel() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating an alias label: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])), nil
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
