// Package dnsname holds the normalization and validation rules for every DNS
// name the service handles. Every entry point — the challenge API, the admin
// API and the CLI — goes through these functions so the rules cannot diverge.
package dnsname

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid classifies every rejection in this package, so callers (the HTTP
// error handler in particular) can map them to a validation response with
// errors.Is without inspecting messages.
var ErrInvalid = errors.New("invalid dns name")

// Normalized is a name that has passed ValidateDomain: lowercase, no trailing
// dot, every label valid. The distinct type is documentation, not enforcement.
type Normalized string

func (n Normalized) String() string { return string(n) }

// maxNameOctets is the presentation-form limit: 255 octets on the wire minus
// the length byte of the first label and the root label.
const maxNameOctets = 253

// maxLabelOctets is the DNS label limit, counted in octets, not runes.
const maxLabelOctets = 63

// Normalize lowercases a name, strips one trailing dot and validates every
// label. It does not accept wildcards — use ValidateDomain for names an
// operator may have typed a `*` into.
func Normalize(name string) (string, error) {
	n := strings.ToLower(strings.TrimSuffix(name, "."))
	if n == "" {
		return "", fmt.Errorf("%w: name is empty", ErrInvalid)
	}
	if len(n) > maxNameOctets {
		return "", fmt.Errorf("%w: name %q is %d octets, limit is %d", ErrInvalid, n, len(n), maxNameOctets)
	}
	for _, label := range strings.Split(n, ".") {
		if err := validateNameLabel(label); err != nil {
			return "", fmt.Errorf("%w in %q", err, n)
		}
	}
	return n, nil
}

// validateNameLabel accepts a label of a name, which — unlike an alias label —
// may carry a leading underscore so that `_acme-challenge` and other
// underscore-prefixed service labels normalize.
func validateNameLabel(label string) error {
	if len(label) > maxLabelOctets {
		return fmt.Errorf("%w: label %q is %d octets, limit is %d", ErrInvalid, label, len(label), maxLabelOctets)
	}
	return validateLDH(strings.TrimPrefix(label, "_"), label)
}

// validateLDH checks the letter-digit-hyphen body of a label. reported is the
// label as the user wrote it, so the message names what they typed.
func validateLDH(body, reported string) error {
	if body == "" {
		return fmt.Errorf("%w: label %q is empty", ErrInvalid, reported)
	}
	if body[0] == '-' || body[len(body)-1] == '-' {
		return fmt.Errorf("%w: label %q begins or ends with a hyphen", ErrInvalid, reported)
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return fmt.Errorf("%w: label %q contains %q", ErrInvalid, reported, string(body[i]))
		}
	}
	return nil
}
