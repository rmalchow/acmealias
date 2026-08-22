package authz

import (
	"encoding/json"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Turning rows into a Subject.
//
// Two row shapes carry grants — role_grants and api_key_grants — and they are
// identical but for their owner. Both reduce here to the one Grant type the
// evaluator reads, so nothing downstream of this file knows which kind of
// subject it is authorizing.

// SubjectFromRoles builds the subject a person acts as in one account.
//
// The unrestricted flag short-circuits everything: a member of the account's
// fixed role may do anything in that account, and that is a property of the role
// row rather than of a set of grants, so it cannot be edited away one grant at a
// time.
func SubjectFromRoles(accountID model.ID, roles []model.Role) Subject {
	s := Subject{AccountID: accountID}
	for _, r := range roles {
		if r.AccountID != accountID {
			// A role of another account confers nothing here. This cannot
			// normally happen — the membership's composite foreign key forbids
			// it — so reaching this line means something upstream is wrong, and
			// silently ignoring the role is the safe reading.
			continue
		}
		if r.Unrestricted {
			s.Unrestricted = true
		}
		for _, g := range r.Grants {
			s.Grants = append(s.Grants, grantFrom(g.AccountID, g.ObjectType, g.ObjectID, g.Selector, g.Action, g.Inheritable, g.ID))
		}
	}
	return s
}

// SubjectFromKey builds the subject an API key acts as.
//
// The key's own AccountID is the anchor, taken from the row the database
// returned rather than from anything the request said — which is what makes it
// unforgeable by a caller.
func SubjectFromKey(k *model.APIKey) Subject {
	if k == nil {
		return Subject{}
	}
	s := Subject{AccountID: k.AccountID}
	for _, g := range k.Grants {
		s.Grants = append(s.Grants, grantFrom(g.AccountID, g.ObjectType, g.ObjectID, g.Selector, g.Action, g.Inheritable, g.ID))
	}
	return s
}

func grantFrom(account model.ID, objectType string, objectID *model.ID, selector json.RawMessage, action string, inheritable bool, id model.ID) Grant {
	g := Grant{
		ID:          id,
		AccountID:   account,
		Type:        ObjectType(objectType),
		ObjectID:    objectID,
		Action:      action,
		Inheritable: inheritable,
	}
	// A selector that will not decode is dropped rather than treated as absent:
	// absent means wildcard, and reading a corrupt selector as "everything"
	// would turn a storage fault into an escalation. Dropping it makes the grant
	// match nothing, which is the safe direction.
	if sel, err := DecodeSelector(selector); err == nil && sel != nil {
		g.Selector = sel
	} else if len(selector) > 0 {
		g.Type = ObjectType("unreadable")
	}
	return g
}

// ObjectOf describes an inventory row for the evaluator, filling in the
// ancestors design P3 materialized. It is the one place that knows which field
// of which struct is which ancestor, so a new object type is added here rather
// than at every call site.
func ObjectOf(v any) Object {
	switch o := v.(type) {
	case *model.Project:
		return Object{Type: TypeProject, ID: o.ID, AccountID: o.AccountID, ProjectID: o.ID}
	case *model.LoadBalancer:
		return Object{Type: TypeLoadBalancer, ID: o.ID, AccountID: o.AccountID, ProjectID: o.ProjectID, LoadBalancerID: o.ID}
	case *model.LBNode:
		return Object{Type: TypeNode, ID: o.ID, AccountID: o.AccountID, ProjectID: o.ProjectID, LoadBalancerID: o.LoadBalancerID}
	case *model.Domain:
		return Object{Type: TypeDomain, ID: o.ID, AccountID: o.AccountID, ProjectID: o.ProjectID, LoadBalancerID: o.LoadBalancerID}
	case *model.Alias:
		return Object{Type: TypeAlias, ID: o.ID, AccountID: o.AccountID, ProjectID: o.ProjectID, LoadBalancerID: o.LoadBalancerID}
	case *model.APIKey:
		return Object{Type: TypeAPIKey, ID: o.ID, AccountID: o.AccountID, ProjectID: o.ProjectID, LoadBalancerID: o.LoadBalancerID}
	}
	// An unknown type matches no grant, because Object.Type is empty and no
	// grant names the empty type. Failing closed rather than panicking: an
	// authorization check is not the place to take a process down, and a refusal
	// is visible in a way a nil dereference is not.
	return Object{}
}

// Ancestors reports an object's ancestor identifiers by type, which is what the
// subset rule needs when a held grant names an ancestor of a proposed grant's
// object.
func Ancestors(o Object) map[ObjectType]model.ID {
	out := map[ObjectType]model.ID{}
	if o.ProjectID != model.NilID {
		out[TypeProject] = o.ProjectID
	}
	if o.LoadBalancerID != model.NilID {
		out[TypeLoadBalancer] = o.LoadBalancerID
	}
	return out
}
