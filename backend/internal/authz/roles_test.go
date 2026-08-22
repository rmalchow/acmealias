package authz

import (
	"slices"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
)

// testMapping is the mapping every test in this package uses. The group names
// deliberately do not look like the role names, so a test that passes because
// something compared a role to itself would fail here.
var testMapping = Mapping{
	RoleAdmin:       "acme-admins",
	RoleConfigAdmin: "acme-config",
	RoleKeyRotation: "acme-keys",
	RoleReadOnly:    "acme-viewers",
}

func TestMappingFromConfig(t *testing.T) {
	m := MappingFromConfig(config.Config{
		AdminGroup:       "acme-admins",
		ConfigAdminGroup: "acme-config",
		KeyRotationGroup: "acme-keys",
		ReadOnlyGroup:    "acme-viewers",
	})
	if len(m) != 4 {
		t.Fatalf("got %d mapped roles, want 4: %v", len(m), m)
	}
	for role, want := range testMapping {
		if m[role] != want {
			t.Errorf("%s: got %q, want %q", role, m[role], want)
		}
	}
}

func TestMappingFromConfigUnsetVariableMapsNobody(t *testing.T) {
	// Only ADMIN is configured. The other three must be held by nobody — not
	// by everybody, which is the failure mode this test exists to catch.
	m := MappingFromConfig(config.Config{AdminGroup: "acme-admins"})

	for _, role := range []Role{RoleConfigAdmin, RoleKeyRotation, RoleReadOnly} {
		if _, ok := m[role]; ok {
			t.Errorf("%s is mapped from an unset variable", role)
		}
	}
	// Someone in every conceivable group still gets nothing but ADMIN.
	roles := m.RolesFor([]string{"acme-admins", "acme-config", "acme-keys", "acme-viewers", ""})
	if got := roles.List(); !slices.Equal(got, []string{"ADMIN"}) {
		t.Errorf("roles = %v, want [ADMIN]", got)
	}
}

func TestRolesFor(t *testing.T) {
	tests := []struct {
		name   string
		groups []string
		want   []string
	}{
		{"no groups at all", nil, nil},
		{"unrelated groups only", []string{"everyone", "vpn-users"}, nil},
		{"admin", []string{"acme-admins"}, []string{"ADMIN"}},
		{"config admin", []string{"acme-config"}, []string{"CONFIG_ADMIN"}},
		{"key rotation", []string{"acme-keys"}, []string{"KEY_ROTATION"}},
		{"read only", []string{"acme-viewers"}, []string{"READ_ONLY"}},
		{
			"roles are additive",
			[]string{"vpn-users", "acme-keys", "acme-viewers"},
			[]string{"KEY_ROTATION", "READ_ONLY"},
		},
		{
			"every group",
			[]string{"acme-admins", "acme-config", "acme-keys", "acme-viewers"},
			[]string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"},
		},
		{
			// Keycloak's "Full group path" left switched on: the claim carries
			// "/acme/acme-admins" and nothing matches. This is the mistake the
			// no-role 403 exists to make visible.
			"full group path does not match a leaf name",
			[]string{"/acme/acme-admins"},
			nil,
		},
		{"membership is case sensitive", []string{"ACME-ADMINS"}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := testMapping.RolesFor(tc.groups).List()
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("RolesFor(%v) = %v, want %v", tc.groups, got, tc.want)
			}
		})
	}
}

func TestRoleSetListIsCanonicallyOrdered(t *testing.T) {
	// Built in a deliberately wrong order; List must still come out in
	// AllRoles order, because audit rows and log lines are compared by humans.
	s := NewRoleSet(RoleReadOnly, RoleAdmin, RoleKeyRotation)
	want := []string{"ADMIN", "KEY_ROTATION", "READ_ONLY"}
	if got := s.List(); !slices.Equal(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestMappingEntriesShowUnmappedRoles(t *testing.T) {
	entries := Mapping{RoleAdmin: "acme-admins"}.Entries()
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want one per role", len(entries))
	}
	if entries[0].Role != RoleAdmin || entries[0].Group != "acme-admins" {
		t.Errorf("first entry = %+v", entries[0])
	}
	for _, e := range entries[1:] {
		if e.Group != "" {
			t.Errorf("%s: got group %q, want empty", e.Role, e.Group)
		}
	}
}
