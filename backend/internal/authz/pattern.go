package authz

import (
	"errors"
	"fmt"
	"strings"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
)

// ErrInvalidGrant classifies every rejection of a grant that cannot mean
// anything. It is deliberately the same shape as inventory.ErrInvalid so one
// errors.Is in the HTTP error handler covers both.
var ErrInvalidGrant = errors.New("authz: invalid grant")

// ErrNotPermitted is the refusal of an operation the subject holds no grant for.
var ErrNotPermitted = errors.New("authz: not permitted")

// Name patterns, for the DNS record selector.
//
// A pattern is a DNS name whose FIRST label may be `*`, and only the first.
// `*.staging.example.com` matches `web.staging.example.com` and
// `db.web.staging.example.com` — one or more labels — and does NOT match
// `staging.example.com` itself.
//
// The apex exclusion is the rule most likely to be got wrong, and it is the one
// that matters most. `*.example.com` reads as "everything under example.com",
// and if it included the apex then a grant meant to delegate a subtree would
// also confer the zone's own `NS` and `MX` records — which is to say, where the
// domain is served from and where its mail goes. So the wildcard matches
// strictly beneath, and reaching the apex takes a grant that names it.
//
// Interior wildcards (`web-*.example.com`, `*.web.*.example.com`) are refused at
// write time rather than being given a meaning. Every meaning they could have is
// a guess about intent, and a grant nobody can read at a glance is a grant
// nobody can review.

// ValidatePattern rejects a pattern that is not a name with an optional leading
// wildcard label.
func ValidatePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("%w: a DNS record selector names no pattern", ErrInvalidGrant)
	}
	labels := strings.Split(strings.TrimSuffix(pattern, "."), ".")
	for i, l := range labels {
		if !strings.Contains(l, "*") {
			continue
		}
		if i != 0 {
			return fmt.Errorf("%w: %q has a wildcard in label %d; only the first label may be `*`",
				ErrInvalidGrant, pattern, i+1)
		}
		if l != "*" {
			return fmt.Errorf("%w: %q has a partial wildcard label; a wildcard label is exactly `*`",
				ErrInvalidGrant, pattern)
		}
	}
	if len(labels) < 2 {
		return fmt.Errorf("%w: %q is not a name a grant can select", ErrInvalidGrant, pattern)
	}
	return nil
}

// normalizeName lowercases and strips the trailing dot, so a pattern and a name
// are compared in one form. The existing dnsname package owns the harder
// normalization rules; this is the comparison-only reduction both sides need.
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// MatchPattern reports whether name falls within pattern.
func MatchPattern(pattern, name string) bool {
	p, n := normalizeName(pattern), normalizeName(name)
	if p == "" || n == "" {
		return false
	}
	if !strings.HasPrefix(p, "*.") {
		return p == n
	}
	suffix := p[len("*."):]
	if n == suffix {
		// The apex of the pattern is NOT within it: see the note above.
		return false
	}
	return strings.HasSuffix(n, "."+suffix)
}

// PatternContains reports whether every name matched by inner is also matched by
// outer — the pattern half of the subset rule.
//
//	*.a.b ⊑ *.b     a subtree is inside a wider subtree
//	a.b   ⊑ *.b     an exact name is inside the subtree above it
//	*.b   ⋢ *.a.b   a wider subtree is NOT inside a narrower one
//	a.b   ⋢ a.b.c   unrelated names contain nothing of each other
func PatternContains(outer, inner string) bool {
	o, i := normalizeName(outer), normalizeName(inner)
	if o == i {
		return true
	}
	if !strings.HasPrefix(o, "*.") {
		// A non-wildcard outer contains only itself, which the equality above
		// already covered.
		return false
	}
	suffix := o[len("*."):]
	if strings.HasPrefix(i, "*.") {
		// `*.x ⊑ *.y` exactly when x is y or lies beneath y. The inner's own
		// apex is excluded from the inner, so it need not be inside the outer.
		x := i[len("*."):]
		return x == suffix || strings.HasSuffix(x, "."+suffix)
	}
	return MatchPattern(o, i)
}

// NormalizeRecordName reduces a record name to the form patterns are compared
// against, delegating the real rules to dnsname so a grant and a record agree
// about what a name is.
//
// A wildcard RECORD — one whose first label is literally `*` — is an ordinary
// record, not a pattern, and normalizes with that label intact.
func NormalizeRecordName(name string) (string, error) {
	if rest, ok := strings.CutPrefix(strings.TrimSpace(name), "*."); ok {
		n, err := dnsname.Normalize(rest)
		if err != nil {
			return "", err
		}
		return "*." + n, nil
	}
	return dnsname.Normalize(name)
}
