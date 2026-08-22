// Package authz derives the four fixed, global roles from a session's group
// claim and enforces them on management routes.
//
// The whole model is: a role type, a set, and a middleware. There is no table
// of role bindings, no per-project scoping, and no rule language — the
// group-to-role mapping is four environment variables read at startup, and the
// permission matrix is the table in permissions.go.
package authz

import (
	"log/slog"
	"slices"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
)

// Role is one of exactly four global roles. No role is scoped to a subset of
// projects; there is deliberately no fifth.
type Role string

const (
	RoleAdmin       Role = "ADMIN"
	RoleConfigAdmin Role = "CONFIG_ADMIN"
	RoleKeyRotation Role = "KEY_ROTATION"
	RoleReadOnly    Role = "READ_ONLY"
)

// AllRoles is the canonical order used for logging, for the effective-config
// view and for the strings written into audit rows, so those stay stable
// regardless of map iteration order.
var AllRoles = []Role{RoleAdmin, RoleConfigAdmin, RoleKeyRotation, RoleReadOnly}

// RoleSet is the set of roles a request's actor holds. Holding several at once
// is normal and additive.
type RoleSet map[Role]bool

// NewRoleSet builds a set. Duplicates collapse.
func NewRoleSet(roles ...Role) RoleSet {
	s := make(RoleSet, len(roles))
	for _, r := range roles {
		s[r] = true
	}
	return s
}

// Has reports whether the actor holds r.
func (s RoleSet) Has(r Role) bool { return s[r] }

// Empty reports whether the actor holds no role at all — an authenticated user
// in none of the configured groups, which is refused and explained rather than
// silently treated as a reader.
func (s RoleSet) Empty() bool { return len(s) == 0 }

// List returns the held roles as strings in AllRoles order.
func (s RoleSet) List() []string {
	out := make([]string, 0, len(s))
	for _, r := range AllRoles {
		if s[r] {
			out = append(out, string(r))
		}
	}
	return out
}

// Mapping names, per role, the identity-provider group that grants it. A role
// missing from the map — an unset environment variable — is held by nobody,
// never by everybody.
//
// It is configuration, not data: nothing in the management API can change it,
// and a change takes effect when the process restarts.
type Mapping map[Role]string

// MappingFromConfig reads the four ACME_*_GROUP variables. Empty values are
// left out, so an unset variable and a variable set to "" behave identically.
func MappingFromConfig(c config.Config) Mapping {
	m := Mapping{}
	for role, group := range map[Role]string{
		RoleAdmin:       c.AdminGroup,
		RoleConfigAdmin: c.ConfigAdminGroup,
		RoleKeyRotation: c.KeyRotationGroup,
		RoleReadOnly:    c.ReadOnlyGroup,
	} {
		if group != "" {
			m[role] = group
		}
	}
	return m
}

// RolesFor is the union of every role whose configured group appears in the
// session's groups claim.
//
// Groups are compared verbatim: oidcauth.Groups has already stripped the
// configured prefix, so what arrives here is what the operator is expected to
// have put in the environment variable. A mismatch is therefore a mapping
// mistake, and Middleware reports the observed groups so it can be seen.
func (m Mapping) RolesFor(groups []string) RoleSet {
	s := RoleSet{}
	for role, group := range m {
		if slices.Contains(groups, group) {
			s[role] = true
		}
	}
	return s
}

// RoleGroup is one row of the mapping, for logging and for the effective
// configuration view. Group is empty when nobody holds the role.
type RoleGroup struct {
	Role  Role   `json:"role"`
	Group string `json:"group"`
}

// Entries returns all four roles in canonical order, including the unmapped
// ones — an operator looking at the effective configuration needs to see that
// a role has no group as clearly as that it has one.
func (m Mapping) Entries() []RoleGroup {
	out := make([]RoleGroup, 0, len(AllRoles))
	for _, r := range AllRoles {
		out = append(out, RoleGroup{Role: r, Group: m[r]})
	}
	return out
}

// LogValue logs the mapping. The group names are not secrets — they are the
// first thing to check when someone cannot do what they should be able to.
func (m Mapping) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, len(AllRoles))
	for _, e := range m.Entries() {
		group := e.Group
		if group == "" {
			group = "(unset: nobody holds this role)"
		}
		attrs = append(attrs, slog.String(string(e.Role), group))
	}
	return slog.GroupValue(attrs...)
}

// Log records the mapping at startup. Call it once, from main.
func (m Mapping) Log(log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	log.Info("role mapping", slog.Any("groups", m))
}
