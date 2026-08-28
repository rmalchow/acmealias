package authz

// Action is one class of management operation — the verb half of a grant.
//
// It used to be one row of a permission matrix keyed by four global role names
// (design D10). The matrix is gone with the internal edition (design C10): an
// action is now checked against an OBJECT for a SUBJECT by the evaluator, and
// this list is what there is to check.
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
	// ever shown. Separate from ActionKeyWrite because seeing the secret and
	// being able to mint one are different powers, even though the default role
	// set happens to hand them out together.
	ActionKeySecretView Action = "key.secret.view"
	// ActionDNSPlan runs verification. It is no longer the DNS plan's
	// permission, because there is no DNS plan (change domain-diagnostics,
	// G2) — the string keeps its spelling deliberately: renaming it would mean
	// migrating role_grants rows and editing the default role set for a word.
	ActionDNSPlan Action = "dns.plan"
	// ActionViewInventory views the inventory and the audit log.
	ActionViewInventory Action = "inventory.view"
)

// AllActions is every Action, in a stable order, for tests and for the "what may
// I do here" answer the console renders controls from.
var AllActions = []Action{
	ActionProjectAdmin,
	ActionInventoryWrite,
	ActionKeyWrite,
	ActionKeySecretView,
	ActionDNSPlan,
	ActionViewInventory,
}

// PermittedInAccount lists the actions a subject holds ACCOUNT-WIDE, which is
// what /api/v1/me answers.
//
// "Account-wide" is a real question with a real answer, and that is why it is
// asked this way rather than by consulting a table of roles. A wildcard object
// matches only a grant whose type and identifier are both wildcards, so this
// reports exactly the actions the subject may perform on anything in the
// account — which is what the old role matrix meant and all it could mean.
//
// A subject holding an action on ONE project is correctly absent from this list
// and correctly present in PermittedOn for that project. The console asks this
// one for the shell and that one for a screen; conflating them would render a
// button that is right on average and wrong on every row.
func PermittedInAccount(s Subject) []string {
	everything := Object{Type: TypeAny, AccountID: s.AccountID}
	out := make([]string, 0, len(AllActions))
	for _, a := range AllActions {
		permitted := Can(s, string(a), everything)
		// Project administration is governed by MEMBERSHIP of the account's
		// unrestricted role rather than by a grant (design P14), so it is
		// answered the way the handler answers it. PermittedOn carries the same
		// exception, for the same reason.
		if a == ActionProjectAdmin {
			permitted = s.Unrestricted
		}
		if permitted {
			out = append(out, string(a))
		}
	}
	return out
}
