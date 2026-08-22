package dnsname

import (
	"errors"
	"strings"
	"testing"
)

func TestChallengeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"example.com", "_acme-challenge.example.com"},
		{"*.example.com", "_acme-challenge.example.com"},
		{"shop.example.com", "_acme-challenge.shop.example.com"},
		{"*.shop.example.com", "_acme-challenge.shop.example.com"},
		{"*.Example.COM.", "_acme-challenge.example.com"},
		{"wild.test", "_acme-challenge.wild.test"},
	}
	for _, tc := range tests {
		if got := ChallengeName(tc.in); got != tc.want {
			t.Errorf("ChallengeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWildcardAndBaseShareOneChallengeName is the fact the whole alias model
// rests on: two domain rows, one challenge name, therefore one alias and one
// customer-side CNAME.
func TestWildcardAndBaseShareOneChallengeName(t *testing.T) {
	base, _, err := ValidateDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	wild, _, err := ValidateDomain("*.EXAMPLE.com.")
	if err != nil {
		t.Fatal(err)
	}
	if base == wild {
		t.Fatalf("the two domains must stay distinct: %q", base)
	}
	if ChallengeName(base.String()) != ChallengeName(wild.String()) {
		t.Fatalf("ChallengeName(%q) = %q, ChallengeName(%q) = %q, want identical",
			base, ChallengeName(base.String()), wild, ChallengeName(wild.String()))
	}
	if got := ChallengeName(wild.String()); got != "_acme-challenge.example.com" {
		t.Fatalf("challenge name = %q, want _acme-challenge.example.com", got)
	}
	// ...and a specific host does not join them.
	if ChallengeName("shop.example.com") == ChallengeName(base.String()) {
		t.Fatal("shop.example.com must derive its own challenge name")
	}
}

func TestValidateLabel(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"example-com", true},
		{"a", true},
		{"a1", true},
		{"1a", true},
		{"shop-example-com", true},
		{strings.Repeat("a", 63), true},

		{"", false},
		{strings.Repeat("a", 64), false},
		{"example.com", false},
		{"_acme", false},
		{"-example", false},
		{"example-", false},
		{"Example", false},
		{"exa mple", false},
		{"exämple", false},
		{"*", false},
	}
	for _, tc := range tests {
		err := ValidateLabel(tc.in)
		if tc.ok && err != nil {
			t.Errorf("ValidateLabel(%q) = %v, want accepted", tc.in, err)
		}
		if !tc.ok {
			if err == nil {
				t.Errorf("ValidateLabel(%q) = nil, want a rejection", tc.in)
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("ValidateLabel(%q) error %v, want ErrInvalid", tc.in, err)
			}
		}
	}
}

func TestSuggestLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"_acme-challenge.example.com", "example-com"},
		{"_acme-challenge.shop.example.com", "shop-example-com"},
		{"_acme-challenge.example.co.uk", "example-co-uk"},
		{"_acme-challenge.EXAMPLE.com.", "example-com"},
		{"_acme-challenge.wild.test", "wild-test"},
		{"example.com", "example-com"}, // tolerates a name that is not a challenge name
	}
	for _, tc := range tests {
		got := SuggestLabel(tc.in)
		if got != tc.want {
			t.Errorf("SuggestLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if err := ValidateLabel(got); err != nil {
			t.Errorf("SuggestLabel(%q) = %q, which is not a valid label: %v", tc.in, got, err)
		}
	}
}

// TestSuggestLabelAlwaysValid: whatever a challenge name looks like, the
// suggestion must satisfy ValidateLabel — the operator may accept it unedited.
func TestSuggestLabelAlwaysValid(t *testing.T) {
	long := ChallengeName(strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + ".example.com")
	for _, in := range []string{
		long,
		ChallengeName(strings.Repeat("a", 63) + ".com"),
		"_acme-challenge." + strings.Repeat("-", 70),
		"_acme-challenge.",
		"",
		"...",
	} {
		got := SuggestLabel(in)
		if err := ValidateLabel(got); err != nil {
			t.Errorf("SuggestLabel(%q) = %q, which is not a valid label: %v", in, got, err)
		}
	}
	if len(SuggestLabel(long)) != maxLabelOctets {
		t.Errorf("SuggestLabel of a long name = %q, want it truncated to %d octets", SuggestLabel(long), maxLabelOctets)
	}
}

// TestGenerateLabelIsValidAndUnpredictable pins both halves of design A1: the
// label is always a usable DNS label, and it is drawn fresh every time.
//
// The distinctness assertions are the ones a hash-based implementation fails,
// which is exactly why they are here — a hash of the challenge name would
// produce a perfectly valid, perfectly stable label and leave the enumeration
// oracle wide open.
func TestGenerateLabelIsValidAndUnpredictable(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		got, err := GenerateLabel()
		if err != nil {
			t.Fatalf("GenerateLabel: %v", err)
		}
		if err := ValidateLabel(got); err != nil {
			t.Fatalf("GenerateLabel() = %q, which is not a valid label: %v", got, err)
		}
		if len(got) != 16 {
			t.Fatalf("GenerateLabel() = %q, want 16 octets, got %d", got, len(got))
		}
		if seen[got] {
			t.Fatalf("GenerateLabel() repeated %q within %d draws", got, n)
		}
		seen[got] = true
	}
}

// Two labels for the same challenge name differ: there is no function from a
// domain to its label at all (design A1).
func TestGenerateLabelIgnoresTheChallengeName(t *testing.T) {
	first, err := GenerateLabel()
	if err != nil {
		t.Fatalf("GenerateLabel: %v", err)
	}
	second, err := GenerateLabel()
	if err != nil {
		t.Fatalf("GenerateLabel: %v", err)
	}
	if first == second {
		t.Errorf("two labels for %q are both %q; a generated label must not be a function of the name",
			ChallengeName("demo.example.com"), first)
	}
	if strings.Contains(first, "example") || strings.Contains(first, "demo") {
		t.Errorf("generated label %q contains part of the domain", first)
	}
}
