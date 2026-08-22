package dnsname

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateDomainD4Table walks every row of the design D4 wildcard table,
// plus the normalization and public-suffix cases that surround them.
func TestValidateDomainD4Table(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Normalized // empty means: expect a rejection
	}{
		// D4 table, row by row.
		{"wildcard accepted", "*.example.com", "*.example.com"},
		{"base accepted", "example.com", "example.com"},
		{"two wildcard labels", "*.*.example.com", ""},
		{"wildcard not leftmost", "foo.*.example.com", ""},
		{"bare wildcard", "*", ""},
		{"wildcard at a TLD", "*.com", ""},
		{"wildcard at a multi-label public suffix", "*.co.uk", ""},
		{"specific host alongside a wildcard is accepted here", "shop.example.com", "shop.example.com"},

		// Around the table.
		{"wildcard is normalized", "*.Example.COM.", "*.example.com"},
		{"wildcard one level down", "*.eu.example.com", "*.eu.example.com"},
		{"registrable name under a multi-label suffix", "*.example.co.uk", "*.example.co.uk"},
		{"wildcard at an unmanaged TLD", "*.test", ""},
		{"wildcard under an unmanaged TLD", "*.wild.test", "*.wild.test"},
		{"wildcard with nothing under it", "*.", ""},
		{"wildcard of a wildcard only", "*.*", ""},
		{"embedded asterisk", "fo*o.example.com", ""},
		{"trailing wildcard label", "example.com.*", ""},
		{"empty", "", ""},
		{"invalid label under a wildcard", "*.-example.com", ""},
		{"over-long label under a wildcard", "*." + name253, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings, err := ValidateDomain(tc.in)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("ValidateDomain(%q) = %q, want a rejection", tc.in, got)
				}
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("ValidateDomain(%q) error %v, want ErrInvalid", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateDomain(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ValidateDomain(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len(warnings) != 0 {
				t.Fatalf("ValidateDomain(%q) warnings = %v, want none at this layer", tc.in, warnings)
			}
		})
	}
}

func TestRegistrableDomain(t *testing.T) {
	tests := []struct {
		in   string
		want string // empty means: expect a rejection
	}{
		{"example.com", "example.com"},
		{"shop.example.com", "example.com"},
		{"*.example.com", "example.com"},
		{"a.b.c.example.com", "example.com"},
		{"_acme-challenge.shop.example.com", "example.com"},
		{"example.co.uk", "example.co.uk"},
		{"shop.example.co.uk", "example.co.uk"},
		{"WILD.Test.", "wild.test"},
		{"shop.wild.test", "wild.test"},
		{"com", ""},
		{"co.uk", ""},
		{"", ""},
	}

	for _, tc := range tests {
		got, err := RegistrableDomain(tc.in)
		switch {
		case tc.want == "" && err == nil:
			t.Errorf("RegistrableDomain(%q) = %q, want a rejection", tc.in, got)
		case tc.want == "":
		case err != nil:
			t.Errorf("RegistrableDomain(%q) unexpected error: %v", tc.in, err)
		case got != tc.want:
			t.Errorf("RegistrableDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCounterpart pins design B2's mirror and B3's premise: the derivation is
// purely textual, so a valid name can yield an invalid counterpart.
func TestCounterpart(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"example.com", "*.example.com"},
		{"*.example.com", "example.com"},
		{"shop.example.com", "*.shop.example.com"},
		{"*.shop.example.com", "shop.example.com"},
		{"Example.COM.", "*.example.com"},
		{"co.uk", "*.co.uk"},
	} {
		if got := Counterpart(tc.in); got != tc.want {
			t.Errorf("Counterpart(%q) = %q, want %q", tc.in, got, tc.want)
		}
		// Both directions round-trip for the names that are round-trippable at
		// all (the normalized ones).
		if tc.in == strings.ToLower(strings.TrimSuffix(tc.in, ".")) {
			if back := Counterpart(tc.want); back != tc.in {
				t.Errorf("Counterpart(Counterpart(%q)) = %q, want %q", tc.in, back, tc.in)
			}
		}
	}
}

// TestCounterpartMayBeInvalid is decision B3's whole reason for existing: the
// caller must validate what Counterpart hands back.
func TestCounterpartMayBeInvalid(t *testing.T) {
	// A name at a public suffix is fine; its wildcard is not.
	if _, _, err := ValidateDomain("co.uk"); err != nil {
		t.Fatalf("co.uk should validate as a plain name: %v", err)
	}
	if _, _, err := ValidateDomain(Counterpart("co.uk")); !errors.Is(err, ErrInvalid) {
		t.Errorf("*.co.uk should be refused, got %v", err)
	}

	// A name at the length limit is fine; two octets more is not.
	long := name253[1:] // 252 octets: its wildcard form is 254
	if _, _, err := ValidateDomain(long); err != nil {
		t.Fatalf("%d-octet name should validate: %v", len(long), err)
	}
	if _, _, err := ValidateDomain(Counterpart(long)); !errors.Is(err, ErrInvalid) {
		t.Errorf("the %d-octet counterpart should be refused as over-length, got %v", len(long)+2, err)
	}
}
