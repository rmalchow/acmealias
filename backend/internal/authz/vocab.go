package authz

// The audit vocabulary: what an entry can be about, and what can have happened
// to it.
//
// These are constants rather than strings written at the call site because the
// audit trail is only useful if `action` and `object_type` mean the same thing
// everywhere. A typo'd "delte" is not a compile error, it is a row that no
// filter will ever return — and nobody notices, because the screen that would
// have shown it is the one being filtered.

// Object types. The value is what lands in audit_entries.object_type and what
// GET /api/v1/audit?objectType= matches.
const (
	ObjectProject = "project"
	ObjectNode    = "lb_node"
	ObjectDomain  = "domain"
	ObjectAlias   = "alias"
	ObjectAPIKey  = "api_key"
	ObjectSession = "session"
	ObjectRole    = "role"
	ObjectMember  = "member"
	ObjectAccount = "account"

	// There was an ObjectLoadBalancer, and the project actions below now cover
	// what its three covered: a project IS the load balancer, so creating one
	// is project.create and there is nothing left for load_balancer.create to
	// describe. Entries already written keep the old word — audit_entries is
	// append-only and a record of what happened is not editable — so a filter
	// on object_type=load_balancer returns exactly the history that used it,
	// and nothing new ever joins it.
)

// Actions, named `<object>.<verb>`. The object prefix is part of the action so
// that an entry is legible on its own — "delete" tells you nothing, and a
// listing filtered to `action=delete` mixes a deleted node with a deleted
// project.
const (
	ActionProjectCreate = "project.create"
	ActionProjectUpdate = "project.update"
	ActionProjectDelete = "project.delete"

	ActionNodeCreate = "lb_node.create"
	ActionNodeUpdate = "lb_node.update"
	ActionNodeDelete = "lb_node.delete"

	ActionDomainCreate = "domain.create"
	ActionDomainDelete = "domain.delete"

	// An alias appears and disappears as a side effect of adding and removing
	// domains, never by itself. It is still recorded as its own entry: an
	// alias is the name a customer published a CNAME to, so its removal is the
	// moment their validation stops working, and that has to be findable
	// without knowing which domain deletion caused it.
	ActionAliasCreate = "alias.create"
	ActionAliasDelete = "alias.delete"

	ActionKeyCreate       = "apikey.create"
	ActionKeyRotate       = "apikey.rotate"
	ActionKeyRestrict     = "apikey.restrict"
	ActionKeyExpiryChange = "apikey.expiry_change"
	ActionKeyRevoke       = "apikey.revoke"
	ActionKeyExpire       = "apikey.expire"

	ActionSessionRevoke = "session.revoke"

	// Roles and membership are credential-class actions: they change who can do
	// what, which is the same class of question as who holds a key.
	ActionRoleCreate   = "role.create"
	ActionRoleUpdate   = "role.update"
	ActionRoleDelete   = "role.delete"
	ActionMemberAdd    = "member.add"
	ActionMemberRemove = "member.remove"

	ActionAccountCreate = "account.create"
	ActionAccountSelect = "account.select"

	// Buying credit and handing an account to somebody else. Both are
	// credential-class (below): one is money and one is who ultimately controls
	// the tenant, and both are questions asked by the person auditing who has
	// power here rather than by the person auditing what the setup looks like.
	//
	// account.checkout is recorded with the amount and the balance either side
	// of it, because the record that has to be right on the day money is
	// involved is the one written while none is (design C8).
	ActionAccountCheckout = "account.checkout"
	ActionAccountTransfer = "account.transfer"

	// The two actions a challenge key performs. They are separate so a key can
	// publish without being able to withdraw — which is what an operator wants
	// for a project that renews automatically but should never take a live
	// value away.
	//
	// They live here rather than beside the handler because two packages
	// authorize against them now — the challenge API and the CLI that mints
	// keys — and a vocabulary held as string literals in two places is the
	// typo nobody notices until a key silently cannot do its job.
	ActionChallengePublish  = "challenge.publish"
	ActionChallengeWithdraw = "challenge.withdraw"
)

// Action classes. The two questions asked of an audit trail are "who changed
// the setup" and "who touched the credentials", and they are asked by
// different people for different reasons. Every action belongs to exactly one.
const (
	ClassConfig     = "config"
	ClassCredential = "credential"
)

var actionClasses = map[string][]string{
	ClassConfig: {
		ActionProjectCreate, ActionProjectUpdate, ActionProjectDelete,
		ActionNodeCreate, ActionNodeUpdate, ActionNodeDelete,
		ActionDomainCreate, ActionDomainDelete,
		ActionAliasCreate, ActionAliasDelete,
	},
	ClassCredential: {
		ActionKeyCreate, ActionKeyRotate, ActionKeyRestrict,
		ActionKeyExpiryChange, ActionKeyRevoke, ActionKeyExpire,
		ActionSessionRevoke,
		// Roles and membership belong here rather than with configuration
		// (task 8.1): editing a role changes what a credential confers, and
		// adding a member hands someone one. An operator asking "who touched
		// the credentials" means these too.
		ActionRoleCreate, ActionRoleUpdate, ActionRoleDelete,
		ActionMemberAdd, ActionMemberRemove,
		ActionAccountCreate, ActionAccountSelect,
		ActionAccountCheckout, ActionAccountTransfer,
	},
}

// ActionsInClass returns the actions belonging to class, or nil if class is
// not one of ClassConfig or ClassCredential. Callers use the nil to tell an
// unknown class from an empty one, so a mistyped filter is a 400 rather than
// an empty listing that reads as "nothing happened".
func ActionsInClass(class string) []string { return actionClasses[class] }

// ActionClass reports which class an action belongs to, or "" for an action
// that is not in the vocabulary.
func ActionClass(action string) string {
	for class, actions := range actionClasses {
		for _, a := range actions {
			if a == action {
				return class
			}
		}
	}
	return ""
}
