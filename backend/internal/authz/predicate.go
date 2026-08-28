package authz

import (
	"fmt"
	"strings"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Compiling a subject's grants into something a query can apply (design P13).
//
// Permission enforcement for existing objects is not a check performed after
// fetching. The grants become part of the WHERE clause, so a row the caller may
// not see is never returned by the database — which is what makes the
// non-disclosure property structural rather than a discipline. `nil` comes back
// for an object that does not exist and for one the caller may not see, because
// the query cannot tell them apart either.
//
// The predicate is a disjunction: each grant matching the required action and
// object type contributes one term, and the terms are OR-ed. That works only
// because permissions are allow-only. A deny rule would need correlated negation
// per grant and this would stop being a fold over a list.

// Predicate is a compiled authorization filter for one action on one object
// type, expressed as columns and values rather than as SQL — so the package
// that owns the schema applies it, and this one keeps knowing nothing about
// tables.
type Predicate struct {
	// All means every object of this type within the account qualifies: the
	// subject holds a wildcard identifier, or is unrestricted.
	All bool
	// Columns maps a column on the target table to the identifiers permitted
	// through it. "id" is the object itself; "project_id" is the materialized
	// ancestor of design P3, which exists precisely so that an inherited grant
	// reduces to a single-column predicate instead of a subquery.
	Columns map[string][]model.ID
}

// Empty reports whether the predicate matches nothing at all — the subject holds
// no grant for this action on this type.
//
// An empty predicate is not an error and must not be treated as "no filter". A
// listing compiled from one returns zero rows, which is the correct answer.
func (p Predicate) Empty() bool { return !p.All && len(p.Columns) == 0 }

// ancestorColumn names the column on a target table that holds the identifier of
// an ancestor of the given type.
//
// There is exactly one ancestor a grant can name from below, now that a project
// IS the load balancer, so there is exactly one column. It stays a function
// returning (column, ok) rather than a constant because the "ok" is what stops
// an inherited grant of an unmapped type from silently becoming no filter at
// all.
func ancestorColumn(t ObjectType) (string, bool) {
	if t == TypeProject {
		return "project_id", true
	}
	return "", false
}

// CompilePredicate folds a subject's grants into the filter for one action on
// one object type.
func CompilePredicate(s Subject, action string, target ObjectType) Predicate {
	p := Predicate{Columns: map[string][]model.ID{}}
	if s.AccountID == model.NilID {
		return Predicate{}
	}
	if s.Unrestricted {
		return Predicate{All: true}
	}

	for _, g := range s.Grants {
		if g.AccountID != s.AccountID {
			// Unreachable through the loaders, which anchor every grant to the
			// subject's account. Skipped rather than trusted, because this is
			// the one place a mistake would widen a query.
			continue
		}
		if g.Action != Wildcard && g.Action != action {
			continue
		}

		switch {
		case g.Type == TypeAny:
			if g.Form() == FormWildcard {
				return Predicate{All: true}
			}

		case g.Type == target:
			switch g.Form() {
			case FormWildcard:
				return Predicate{All: true}
			case FormID:
				p.Columns["id"] = append(p.Columns["id"], *g.ObjectID)
			}

		case Contains(g.Type, target) && g.Inheritable:
			col, ok := ancestorColumn(g.Type)
			if !ok {
				continue
			}
			switch g.Form() {
			case FormWildcard:
				return Predicate{All: true}
			case FormID:
				p.Columns[col] = append(p.Columns[col], *g.ObjectID)
			}
		}
	}
	if len(p.Columns) == 0 {
		return Predicate{}
	}
	return p
}

// There was a second compiler here, for the one identifier form that did not
// reduce to equality: a DNS record selector, matched by name pattern. It stored
// names reversed so that "everything beneath this name" became a prefix an index
// could serve rather than a leading-wildcard LIKE that no btree can, and excluded
// the pattern's own apex so a subtree grant could not reach the zone's own NS and
// MX.
//
// It went with the DNS product. The trick is worth remembering — reverse the
// name, match a prefix — if a predicate-shaped identifier is ever wanted again.

// String renders a predicate for a log line or a test failure, so a wrong
// listing can be diagnosed without reconstructing the fold by hand.
func (p Predicate) String() string {
	if p.All {
		return "every object in the account"
	}
	if p.Empty() {
		return "nothing"
	}
	parts := make([]string, 0, len(p.Columns))
	for col, ids := range p.Columns {
		parts = append(parts, fmt.Sprintf("%s in (%d)", col, len(ids)))
	}
	return strings.Join(parts, " or ")
}
