package authz

import (
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Object is the thing a permission check is about: what it is, which one, and
// which ancestors it has.
//
// The ancestors are fields rather than a lookup because design P3 materialized
// them onto every row for exactly this reason. Checking one loaded object needs
// no query and no join: the chain is at most three long and it is already in
// memory. That is what keeps the challenge path's promise of adding no query.
type Object struct {
	Type           ObjectType
	ID             model.ID
	AccountID      model.ID
	ProjectID      model.ID
	LoadBalancerID model.ID
}

// ancestor returns this object's identifier of the given type, and whether it
// has one.
func (o Object) ancestor(t ObjectType) (model.ID, bool) {
	switch t {
	case TypeProject:
		return o.ProjectID, o.ProjectID != model.NilID
	case TypeLoadBalancer:
		return o.LoadBalancerID, o.LoadBalancerID != model.NilID
	}
	return model.NilID, false
}

// Subject is something that holds grants: a person through their roles, or an
// API key. Both reduce to the same two things — an anchor, and a set of grants —
// so the evaluator does not know which it is looking at.
type Subject struct {
	// AccountID is the anchor. Every grant this subject holds is confined to
	// it, and a wildcard means "within this account".
	AccountID model.ID
	// Unrestricted short-circuits the evaluation: the account's fixed role
	// confers every action on every object of that account. It is a flag rather
	// than a set of grants so it cannot be edited away one row at a time, and so
	// that "there is always a way back into an account" cannot be undone by a
	// grant edit.
	Unrestricted bool
	Grants       []Grant
}

// Can reports whether subject may perform action on object.
//
// Evaluation is a union over grants and nothing else: authority is granted, never
// denied, and no grant removes what another confers (the spec calls this out
// because a deny rule is where permission models stop being answerable). Order
// is therefore irrelevant, and so is the order grants were written in.
func Can(s Subject, action string, o Object) bool {
	// Tenancy first, and not as a permission. A subject cannot hold a grant
	// naming another account's object, so this is a belt-and-braces assertion of
	// something the grant grammar already makes unrepresentable — but it is the
	// assertion that keeps an evaluator bug inside one tenant.
	if s.AccountID == model.NilID || o.AccountID != s.AccountID {
		return false
	}
	if s.Unrestricted {
		return true
	}
	for _, g := range s.Grants {
		if matches(g, action, o) {
			return true
		}
	}
	return false
}

// matches reports whether one grant covers one action on one object.
func matches(g Grant, action string, o Object) bool {
	if g.AccountID != o.AccountID {
		return false
	}
	if g.Action != Wildcard && g.Action != action {
		return false
	}

	switch {
	case g.Type == TypeAny:
		// A wildcard type with a wildcard identifier is "everything in this
		// account". A wildcard type cannot name one object — Validate refuses
		// that — so there is nothing else to compare.
		return g.Form() == FormWildcard

	case g.Type == o.Type:
		switch g.Form() {
		case FormWildcard:
			return true
		case FormID:
			return *g.ObjectID == o.ID
		case FormSelector:
			// No object type defines a selector, and Validate refuses a grant
			// carrying one — so a stored grant that has one is a row nothing can
			// evaluate, and matching nothing is the safe reading.
			return false
		}

	case Contains(g.Type, o.Type):
		// The grant names an ancestor. Only an inheritable grant reaches down;
		// without the flag a grant on a project is a grant on the project row.
		if !g.Inheritable {
			return false
		}
		switch g.Form() {
		case FormWildcard:
			return true
		case FormID:
			id, ok := o.ancestor(g.Type)
			return ok && id == *g.ObjectID
		case FormSelector:
			// A selector describes objects of its own type; it cannot describe
			// an ancestor of a different type.
			return false
		}
	}
	// Inheritance never reaches upward: a grant on a load balancer says nothing
	// about the project containing it.
	return false
}

// Permitted lists the actions a subject may perform on one object, from a
// candidate vocabulary. The console renders buttons from it; the server still
// checks every request, because the client is not the enforcement point.
func Permitted(s Subject, actions []string, o Object) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		if Can(s, a, o) {
			out = append(out, a)
		}
	}
	return out
}

// actionsByType is the vocabulary that applies to each kind of object — what it
// even makes sense to ask about one.
//
// It exists so the per-object report (task 7.3) offers a stable, complete
// answer rather than whatever the caller happened to enumerate. A console that
// asked about a different set per screen would render different buttons for the
// same object depending on where you were looking at it from.
var actionsByType = map[ObjectType][]Action{
	TypeProject:      {ActionViewInventory, ActionInventoryWrite, ActionProjectAdmin},
	TypeLoadBalancer: {ActionViewInventory, ActionInventoryWrite, ActionDNSPlan, ActionKeyWrite},
	TypeNode:         {ActionViewInventory, ActionInventoryWrite},
	TypeDomain:       {ActionViewInventory, ActionInventoryWrite},
	TypeAlias:        {ActionViewInventory, ActionInventoryWrite},
	TypeAPIKey:       {ActionViewInventory, ActionKeyWrite, ActionKeySecretView},
}

// ActionsFor reports the actions that can be asked about one kind of object.
func ActionsFor(t ObjectType) []Action { return actionsByType[t] }

// PermittedOn is the per-object report the console renders controls from
// (task 7.3).
//
// Two things about it matter more than what it returns. It is NOT the
// enforcement point — every request is checked again when it arrives, because a
// client is not a place to keep a permission — and it accompanies the listing
// rather than costing a request per row, which is what makes a table of fifty
// projects one query instead of fifty-one.
//
// The subject's grants are already in memory and every object carries its own
// ancestors (design P3), so this is arithmetic: no query, whatever the size of
// the page.
func PermittedOn(s Subject, o Object) []string {
	candidates := ActionsFor(o.Type)
	out := make([]string, 0, len(candidates))
	for _, a := range candidates {
		permitted := Can(s, string(a), o)
		// Project administration is governed by MEMBERSHIP of the unrestricted
		// role, not by a grant (design P14), so the report has to answer for it
		// the way the handler does. Without this a role holding a wildcard grant
		// would be told it may create projects, and then refused when it tried —
		// which is the worst kind of wrong answer, because the user did nothing
		// wrong and the interface promised.
		if a == ActionProjectAdmin {
			permitted = s.Unrestricted
		}
		if permitted {
			out = append(out, string(a))
		}
	}
	return out
}
