package oidcauth

import (
	"strings"
	"testing"
)

func TestGroups(t *testing.T) {
	cases := map[string]struct {
		claims map[string]any
		claim  string
		prefix string
		want   string // comma-joined
	}{
		"plain list": {
			claims: map[string]any{"groups": []any{"admins", "readers"}},
			want:   "admins,readers",
		},
		"configured prefix is stripped": {
			claims: map[string]any{"groups": []any{"/acme/admins", "/acme/readers"}},
			prefix: "/acme/",
			want:   "admins,readers",
		},
		"prefix only strips where it matches": {
			claims: map[string]any{"groups": []any{"/acme/admins", "/other/ops"}},
			prefix: "/acme/",
			want:   "admins,/other/ops",
		},
		"configured claim name": {
			claims: map[string]any{"roles": []any{"admins"}, "groups": []any{"wrong"}},
			claim:  "roles",
			want:   "admins",
		},
		"single string claim":     {claims: map[string]any{"groups": "admins"}, want: "admins"},
		"duplicates collapse":     {claims: map[string]any{"groups": []any{"a", "a"}}, want: "a"},
		"non-strings are skipped": {claims: map[string]any{"groups": []any{"a", 42}}, want: "a"},
		"claim missing":           {claims: map[string]any{}, want: ""},
		"claim of the wrong type": {claims: map[string]any{"groups": 42}, want: ""},
		"empty after stripping":   {claims: map[string]any{"groups": []any{"/acme/"}}, prefix: "/acme/", want: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(Groups(tc.claims, tc.claim, tc.prefix), ",")
			if got != tc.want {
				t.Fatalf("Groups = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]struct {
		claims map[string]any
		want   string
	}{
		"name wins":             {claims: map[string]any{"name": "Alice", "email": "a@x"}, want: "Alice"},
		"then username":         {claims: map[string]any{"preferred_username": "alice", "email": "a@x"}, want: "alice"},
		"then email":            {claims: map[string]any{"email": "a@x"}, want: "a@x"},
		"blank name skipped":    {claims: map[string]any{"name": "  ", "email": "a@x"}, want: "a@x"},
		"falls back to subject": {claims: map[string]any{}, want: "sub-1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := displayName(tc.claims, "sub-1"); got != tc.want {
				t.Fatalf("displayName = %q, want %q", got, tc.want)
			}
		})
	}
}
