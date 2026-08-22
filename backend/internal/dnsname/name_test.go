package dnsname

import (
	"errors"
	"strings"
	"testing"
)

// name of exactly 253 octets: 63 + 63 + 63 + 61 + three dots.
var name253 = strings.Join([]string{
	strings.Repeat("a", 63),
	strings.Repeat("b", 63),
	strings.Repeat("c", 63),
	strings.Repeat("d", 61),
}, ".")

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string // empty means: expect a rejection
	}{
		{"example.com", "example.com"},
		{"Shop.Example.COM.", "shop.example.com"},
		{"EXAMPLE.COM", "example.com"},
		{"example.com.", "example.com"},
		{"_acme-challenge.example.com", "_acme-challenge.example.com"},
		{"_acme-challenge.shop.example.com.", "_acme-challenge.shop.example.com"},
		{"xn--bcher-kva.example.com", "xn--bcher-kva.example.com"}, // punycode is plain LDH
		{"a.b.c.d.e.f.g", "a.b.c.d.e.f.g"},
		{"1.2.3.4", "1.2.3.4"},
		{"a-b.example.com", "a-b.example.com"},
		{name253, name253},
		{strings.Repeat("a", 63) + ".example.com", strings.Repeat("a", 63) + ".example.com"},

		{"", ""},
		{".", ""},
		{"..", ""},
		{"example..com", ""},
		{".example.com", ""},
		{"example.com..", ""}, // only one trailing dot is stripped
		{name253 + "e", ""},   // 254 octets
		{strings.Repeat("a", 64) + ".example.com", ""}, // 64-octet label
		{"-example.com", ""},
		{"example-.com", ""},
		{"exam ple.com", ""},
		{"example.com/foo", ""},
		{"exam_ple.com", ""}, // underscore is allowed only as the leading character
		{"_.example.com", ""},
		{"münchen.de", ""},
		{"*.example.com", ""}, // Normalize does not take wildcards
		{"exa*mple.com", ""},
	}

	for _, tc := range tests {
		got, err := Normalize(tc.in)
		switch {
		case tc.want == "" && err == nil:
			t.Errorf("Normalize(%q) = %q, want a rejection", tc.in, got)
		case tc.want == "":
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Normalize(%q) error %v, want ErrInvalid", tc.in, err)
			}
		case err != nil:
			t.Errorf("Normalize(%q) unexpected error: %v", tc.in, err)
		case got != tc.want:
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeLengthBoundaries(t *testing.T) {
	if len(name253) != 253 {
		t.Fatalf("test fixture is %d octets, want 253", len(name253))
	}
	if _, err := Normalize(name253 + "."); err != nil {
		t.Errorf("253 octets plus the stripped root dot must be accepted: %v", err)
	}
	if _, err := Normalize(strings.Repeat("a", maxLabelOctets)); err != nil {
		t.Errorf("a %d-octet label must be accepted: %v", maxLabelOctets, err)
	}
	if _, err := Normalize(strings.Repeat("a", maxLabelOctets+1)); err == nil {
		t.Errorf("a %d-octet label must be rejected", maxLabelOctets+1)
	}
}
