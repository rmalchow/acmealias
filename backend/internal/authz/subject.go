package authz

import (
	"context"
	"encoding/json"
	"slices"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
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

// GrantFromRow converts one stored api_key_grants row into the evaluator's Grant.
// Exported because the audit trail records a key's authority in full, and doing
// that from the row shape would mean a second renderer that could drift.
func GrantFromRow(g model.APIKeyGrant) Grant {
	return grantFrom(g.AccountID, g.ObjectType, g.ObjectID, g.Selector, g.Action, g.Inheritable, g.ID)
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
		// A key has no project or load balancer ancestor since 000009: it is a
		// subject anchored to the account, and what it may reach is its grants
		// rather than its position in the tree. So a grant OVER a key — the
		// key.write permission — can only be anchored at the account.
		return Object{Type: TypeAPIKey, ID: o.ID, AccountID: o.AccountID}
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

// SubjectForUser resolves what a person may do in one account, from the rows we
// wrote for them.
//
// This is P18 without its exception. There used to be a second producer here,
// SubjectForGroups, which computed a membership per request from the identity
// provider's group claim and stored nothing — the internal edition's derivation.
// Design C10 removed it, so the only answer to "what may this person do here" is
// the account_members rows and the roles they name.
//
// The roles are returned alongside the subject rather than being looked up again
// by whoever wants them: the audit trail records the role NAMES the actor held at
// the time, and a second query for them could disagree with the one the request
// was authorized against.
//
// No membership is not an error. It is the ordinary answer for a person who is
// not in this account, and it yields a subject holding nothing — which every
// check then refuses. The caller decides whether that is a 403 or a 404; the
// evaluator has no opinion.
func SubjectForUser(ctx context.Context, db *gorm.DB, sc store.Scope, userID model.ID) (Subject, []model.Role, error) {
	ms, err := store.MembershipsFor(ctx, db, sc.AccountID(), userID)
	if err != nil {
		return Subject{}, nil, err
	}
	var roleIDs []model.ID
	for _, m := range ms {
		roleIDs = append(roleIDs, m.RoleIDs()...)
	}
	if len(roleIDs) == 0 {
		return Subject{AccountID: sc.AccountID()}, nil, nil
	}
	roles, err := store.RolesFor(ctx, db, sc, roleIDs...)
	if err != nil {
		return Subject{}, nil, err
	}
	return SubjectFromRoles(sc.AccountID(), roles), roles, nil
}

// RoleNames renders a subject's roles for the audit trail, in a stable order.
//
// Names as written, not references: a role is a row, rows are edited and
// deleted, and an entry pointing at one would become unreadable exactly when it
// mattered — after somebody removed the role whose authority is being questioned
// (task 8.3).
func RoleNames(roles []model.Role) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return out
}

// CLISubject is the authority `internal/adminctl` acts with.
//
// It is unrestricted, and that is a statement of fact rather than a grant of
// convenience: the CLI runs with the database connection string and no session,
// so whoever can run it can already write any row directly. Pretending otherwise
// — giving it a narrower subject it could trivially bypass — would put a check
// in the way of the honest path while leaving the dishonest one open, and would
// make the audit trail claim a constraint that did not exist.
//
// What the CLI does NOT get is a way past tenancy: it is anchored to one
// account, like every other subject, and the Scope it was built from decides
// which. That Scope used to be the deployment's only account; since design C10
// there is no such thing, and the account is named on the command line
// (store.ScopeForSlug).
func CLISubject(accountID model.ID) Subject {
	return Subject{AccountID: accountID, Unrestricted: true}
}
