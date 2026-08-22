package authz

// Action is one class of management operation. There is one Action per row of
// the permission matrix in design D10 and in the identity-and-roles spec, and
// no Action takes a project: no role is project-scoped, so there is nothing to
// scope a check to.
type Action string

const (
	// ActionProjectAdmin creates or deletes projects.
	ActionProjectAdmin Action = "project.admin"
	// ActionInventoryWrite creates, edits or deletes load balancers,
	// nodes and domains.
	ActionInventoryWrite Action = "inventory.write"
	// ActionKeyWrite creates or revokes API keys.
	ActionKeyWrite Action = "key.write"
	// ActionKeySecretView sees a key secret at creation, the one time it is
	// ever shown. Separate from ActionKeyWrite because it is a separate row of
	// the matrix, even though the two currently carry the same roles.
	ActionKeySecretView Action = "key.secret.view"
	// ActionSessionAdmin enumerates and revokes other people's sessions.
	ActionSessionAdmin Action = "session.admin"
	// ActionDNSPlan views the DNS plan and runs verification.
	ActionDNSPlan Action = "dns.plan"
	// ActionViewInventory views the inventory and the audit log.
	ActionViewInventory Action = "inventory.view"
)

// permissions is the matrix, spelled out. Reading down a column answers "what
// may this role do"; reading across a row answers "who may do this".
//
// An Action absent from this map is permitted to nobody, so a typo in a route's
// required permission fails closed rather than open.
var permissions = map[Action]RoleSet{
	ActionProjectAdmin:   NewRoleSet(RoleAdmin),
	ActionSessionAdmin:   NewRoleSet(RoleAdmin),
	ActionInventoryWrite: NewRoleSet(RoleAdmin, RoleConfigAdmin),
	ActionKeyWrite:       NewRoleSet(RoleAdmin, RoleConfigAdmin, RoleKeyRotation),
	ActionKeySecretView:  NewRoleSet(RoleAdmin, RoleConfigAdmin, RoleKeyRotation),
	ActionDNSPlan:        NewRoleSet(RoleAdmin, RoleConfigAdmin, RoleKeyRotation, RoleReadOnly),
	ActionViewInventory:  NewRoleSet(RoleAdmin, RoleConfigAdmin, RoleKeyRotation, RoleReadOnly),
}

// AllActions is every Action, for tests and for the console's "what may I do"
// answer.
var AllActions = []Action{
	ActionProjectAdmin,
	ActionSessionAdmin,
	ActionInventoryWrite,
	ActionKeyWrite,
	ActionKeySecretView,
	ActionDNSPlan,
	ActionViewInventory,
}

// CanRole reports whether an actor holding roles may perform action.
//
// This is the pre-grant model: a verb and nothing else, against four fixed role
// names. It survives only until roles become rows and the route middleware that
// calls it is removed — Can in evaluate.go is the model everything moves to. It
// is kept, and renamed rather than deleted, so the two can be compared during
// the migration and so a call site that still uses it is obvious at a glance.
func CanRole(roles RoleSet, action Action) bool {
	for r := range permissions[action] {
		if roles[r] {
			return true
		}
	}
	return false
}

// Allowed lists the actions roles permits, in AllActions order. The console
// uses it to decide which buttons to render; the server still checks every
// request, because the client is not the enforcement point.
func Allowed(roles RoleSet) []Action {
	out := make([]Action, 0, len(AllActions))
	for _, a := range AllActions {
		if CanRole(roles, a) {
			out = append(out, a)
		}
	}
	return out
}
