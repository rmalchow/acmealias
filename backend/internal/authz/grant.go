package authz

import (
	"fmt"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// A grant is [object type, object identifier, action] plus a flag saying whether
// it reaches objects contained by the one it names, anchored to exactly one
// account.
//
// Each of the three parts may be a wildcard, and the identifier may instead name
// exactly one object.
//
// # Why anchored
//
// A wildcard means "within this account", never "everywhere". There is
// deliberately no way to express a grant naming an object of another account:
// tenant isolation is not a permission (design P1), it is a property of the
// query layer, and keeping cross-tenant access out of the grant grammar is what
// makes an evaluator bug produce over-permission inside one tenant rather than
// disclosure across tenants.

// Wildcard is the value that matches every object type or every action.
const Wildcard = "*"

// ObjectType names a kind of thing a grant can be about.
type ObjectType string

const (
	TypeProject ObjectType = "project"
	TypeNode    ObjectType = "lb_node"
	TypeDomain  ObjectType = "domain"
	TypeAlias   ObjectType = "alias"
	TypeAPIKey  ObjectType = "api_key"
	TypeAny     ObjectType = Wildcard
)

// There was a TypeLoadBalancer between TypeProject and the rest. It went when
// the level did (change collapse-project-and-loadbalancer, design L4), and it
// went rather than becoming an alias for TypeProject: a grammar with two words
// for one object type is what migration 000003 was written to remove, and the
// check constraint added by 000004 refuses a stored grant that still uses it.

// containedBy is the containment hierarchy, as a child-to-parent map. It is
// fixed rather than configurable: inheritance is only comprehensible if there is
// exactly one answer to "what contains this".
//
// An account contains projects; a project contains its nodes, domains and
// aliases. Nothing contains an account, which is why an account is not an object
// a grant can name at all (design P14) — operations on it are governed by
// membership.
//
// The chain is one link shorter than it was, and that is the shape of the whole
// collapse: a grant on a project used to reach a load balancer which reached a
// domain, so "may I grant this?" had a level to reason through that no longer
// exists.
//
// An API key is deliberately absent. It is a SUBJECT anchored to the account,
// not a thing sitting in a project — see ObjectOf — so key.write can only be
// anchored at the account, and it was already wrong to have it inherit from a
// level below one.
var containedBy = map[ObjectType]ObjectType{
	TypeNode:   TypeProject,
	TypeDomain: TypeProject,
	TypeAlias:  TypeProject,
}

// Contains reports whether parent contains child, directly or indirectly.
func Contains(parent, child ObjectType) bool {
	for t, ok := containedBy[child]; ok; t, ok = containedBy[t] {
		if t == parent {
			return true
		}
		child = t
	}
	return false
}

// The grammar once admitted a third identifier form: a typed SELECTOR, for an
// object type whose members are picked by a predicate rather than named one at a
// time. It was built for DNS records — zone, name pattern, record type — and was
// kept reserved-but-refused for a while after the DNS product went.
//
// It is gone now. Keeping a form nothing can produce and everything must branch
// on cost a nil check in the evaluator, a column in both grant tables and a
// clause in both check constraints, all to buy back a change that would be a
// grammar change anyway: a predicate identifier needs fields, a compiler and a
// containment rule, none of which the empty placeholder supplied.

// Grant is one row of a role or of an API key.
type Grant struct {
	ID        model.ID   `json:"id,omitempty"`
	AccountID model.ID   `json:"accountId"`
	Type      ObjectType `json:"type"`
	// ObjectID names one object. Nil means the identifier is a wildcard.
	ObjectID *model.ID `json:"objectId,omitempty"`
	Action   string    `json:"action"`
	// Inheritable makes the grant reach every object contained by the one it
	// names. Without it a grant on a project is a grant on the project row and
	// nothing beneath it.
	Inheritable bool `json:"inheritable"`
}

// IdentifierForm is which of the two shapes a grant's identifier takes.
type IdentifierForm int

const (
	// FormWildcard matches every object of the grant's type within its account.
	FormWildcard IdentifierForm = iota
	// FormID names exactly one object.
	FormID
)

// Form reports the identifier form, and is the single place that decides it —
// the role_grants_identifier_form check constraint enforces the same rule, so a
// row that disagrees cannot be stored.
func (g Grant) Form() IdentifierForm {
	if g.ObjectID != nil {
		return FormID
	}
	return FormWildcard
}

// Validate rejects a grant that cannot mean anything.
func (g Grant) Validate() error {
	if g.Action == "" {
		return fmt.Errorf("%w: a grant has no action", ErrInvalidGrant)
	}
	if g.Type == "" {
		return fmt.Errorf("%w: a grant has no object type", ErrInvalidGrant)
	}
	if g.AccountID == model.NilID {
		return fmt.Errorf("%w: a grant is not anchored to an account", ErrInvalidGrant)
	}
	if g.Type == TypeAny && g.ObjectID != nil {
		return fmt.Errorf("%w: a wildcard object type cannot name one object — "+
			"an identifier only means something within a type", ErrInvalidGrant)
	}
	if !KnownType(g.Type) {
		// The old word gets its own sentence. Somebody writing `load_balancer`
		// is not making a typo, they are using a grammar that was real until
		// recently, and "unknown object type" would send them looking for a
		// spelling mistake that is not there.
		if g.Type == "load_balancer" {
			return fmt.Errorf("%w: there is no load_balancer object type — a project IS the "+
				"load balancer now, so name the project", ErrInvalidGrant)
		}
		return fmt.Errorf("%w: %q is not an object type a grant can name", ErrInvalidGrant, g.Type)
	}
	return nil
}

// KnownTypes is every type a grant may name, in a stable order — the wildcard
// included, since it is a legal type slot rather than the absence of one.
var KnownTypes = []ObjectType{TypeAny, TypeProject, TypeNode, TypeDomain, TypeAlias, TypeAPIKey}

// KnownType reports whether t is one of them.
//
// Validate consults it so that an object type nothing can mean is refused where
// the grant is WRITTEN rather than ignored where it is evaluated. A grant naming
// a type no evaluator matches is not harmless: it sits in a role reading as
// authority somebody was given, and the first person to notice is the one who
// could not do the thing the role says they can.
func KnownType(t ObjectType) bool {
	for _, k := range KnownTypes {
		if k == t {
			return true
		}
	}
	return false
}

// String renders a grant the way the design writes them, which is also what the
// audit trail records: [type, id, action] with the flag.
func (g Grant) String() string {
	id := Wildcard
	if g.Form() == FormID {
		id = g.ObjectID.String()
	}
	suffix := ""
	if g.Inheritable {
		suffix = ", inheritable"
	}
	return fmt.Sprintf("[%s, %s, %s%s]", g.Type, id, g.Action, suffix)
}
