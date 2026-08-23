package authz

import (
	"encoding/json"
	"fmt"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// A grant is [object type, object identifier, action] plus a flag saying whether
// it reaches objects contained by the one it names, anchored to exactly one
// account.
//
// Each of the three parts may be a wildcard. The identifier may instead be a
// typed selector, where the object type defines one — today only dns_record
// does.
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
	TypeProject      ObjectType = "project"
	TypeLoadBalancer ObjectType = "load_balancer"
	TypeNode         ObjectType = "lb_node"
	TypeDomain       ObjectType = "domain"
	TypeAlias        ObjectType = "alias"
	TypeAPIKey       ObjectType = "api_key"
	TypeAny          ObjectType = Wildcard
)

// containedBy is the containment hierarchy, as a child-to-parent map. It is
// fixed rather than configurable: inheritance is only comprehensible if there is
// exactly one answer to "what contains this".
//
// An account contains projects; a project contains load balancers; a load
// balancer contains its nodes, domains, aliases and keys. Nothing contains an
// account, which is why an account is not an object a grant can name at all
// (design P14) — operations on it are governed by membership.
var containedBy = map[ObjectType]ObjectType{
	TypeLoadBalancer: TypeProject,
	TypeNode:         TypeLoadBalancer,
	TypeDomain:       TypeLoadBalancer,
	TypeAlias:        TypeLoadBalancer,
	TypeAPIKey:       TypeLoadBalancer,
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

// The identifier admits a third form the grammar reserves and nothing uses: a
// typed SELECTOR, for an object type whose members are picked by a predicate
// rather than named one at a time.
//
// It was built for DNS records — zone, name pattern, record type — and went with
// them when the service stopped hosting DNS. The slot is kept because keeping it
// costs one nil check and re-adding it would otherwise mean changing the grammar
// every subject and every stored row is written against.

// Grant is one row of a role or of an API key.
type Grant struct {
	ID        model.ID   `json:"id,omitempty"`
	AccountID model.ID   `json:"accountId"`
	Type      ObjectType `json:"type"`
	// ObjectID names one object. Nil means the identifier is a wildcard or a
	// selector — Selector says which.
	ObjectID *model.ID `json:"objectId,omitempty"`
	// Selector is the reserved typed identifier form. Always nil today: no object
	// type defines one. See the note above.
	Selector *Selector `json:"selector,omitempty"`
	Action   string    `json:"action"`
	// Inheritable makes the grant reach every object contained by the one it
	// names. Without it a grant on a project is a grant on the project row and
	// nothing beneath it.
	Inheritable bool `json:"inheritable"`
}

// IdentifierForm is which of the three shapes a grant's identifier takes.
type IdentifierForm int

const (
	// FormWildcard matches every object of the grant's type within its account.
	FormWildcard IdentifierForm = iota
	// FormID names exactly one object.
	FormID
	// FormSelector names a set by a typed predicate.
	FormSelector
)

// Form reports the identifier form, and is the single place that decides it —
// the database constraint of migration 000008 enforces the same rule, so a row
// that disagrees cannot be stored.
func (g Grant) Form() IdentifierForm {
	switch {
	case g.Selector != nil:
		return FormSelector
	case g.ObjectID != nil:
		return FormID
	default:
		return FormWildcard
	}
}

// Validate rejects a grant that cannot mean anything.
func (g Grant) Validate() error {
	if g.ObjectID != nil && g.Selector != nil {
		return fmt.Errorf("%w: a grant names either an object or a selector, not both", ErrInvalidGrant)
	}
	if g.Action == "" {
		return fmt.Errorf("%w: a grant has no action", ErrInvalidGrant)
	}
	if g.Type == "" {
		return fmt.Errorf("%w: a grant has no object type", ErrInvalidGrant)
	}
	if g.AccountID == model.NilID {
		return fmt.Errorf("%w: a grant is not anchored to an account", ErrInvalidGrant)
	}
	if g.Selector != nil {
		// No object type defines a selector today, so any grant carrying one is a
		// grant nothing could evaluate. Refused rather than ignored: a stored
		// selector that matched nothing would look like a permission somebody had.
		return fmt.Errorf("%w: object type %q defines no selector", ErrInvalidGrant, g.Type)
	}
	if g.Type == TypeAny && g.ObjectID != nil {
		return fmt.Errorf("%w: a wildcard object type cannot name one object — "+
			"an identifier only means something within a type", ErrInvalidGrant)
	}
	return nil
}

// Selector is the reserved predicate form of a grant identifier. It carries no
// fields, because no object type defines one; it exists so the three-form shape
// of an identifier stays visible in the type rather than only in a comment.
type Selector struct{}

// EncodeSelector renders the selector for storage. Nil selectors store as SQL
// NULL rather than as the JSON literal "null", so the identifier-form check
// constraint can tell the two apart.
func EncodeSelector(s *Selector) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// DecodeSelector reads a stored selector back.
func DecodeSelector(raw []byte) (*Selector, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s Selector
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%w: selector is not readable: %v", ErrInvalidGrant, err)
	}
	return &s, nil
}

// String renders a grant the way the design writes them, which is also what the
// audit trail records: [type, id, action] with the flag.
func (g Grant) String() string {
	id := Wildcard
	switch g.Form() {
	case FormID:
		id = g.ObjectID.String()
	case FormSelector:
		id = "{selector}"
	}
	suffix := ""
	if g.Inheritable {
		suffix = ", inheritable"
	}
	return fmt.Sprintf("[%s, %s, %s%s]", g.Type, id, g.Action, suffix)
}
