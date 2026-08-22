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
	// through it. "id" is the object itself; "project_id" and
	// "load_balancer_id" are the materialized ancestors of design P3, which
	// exist precisely so that an inherited grant reduces to a single-column
	// predicate instead of a subquery.
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
func ancestorColumn(t ObjectType) (string, bool) {
	switch t {
	case TypeProject:
		return "project_id", true
	case TypeLoadBalancer:
		return "load_balancer_id", true
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
			case FormSelector:
				// Selectors do not reduce to equality and are compiled
				// separately; see CompileRecordPredicate.
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

// RecordPredicate is the compiled filter for DNS records, which is the one type
// whose identifier does not reduce to equality (design P13).
//
// A selector matches by name pattern, so its term is a suffix match with the
// pattern's own apex excluded — and a leading-wildcard LIKE cannot use an
// ordinary btree index. The remedy is to store record names REVERSED with a
// text_pattern_ops index and match a prefix instead, which is why this carries
// reversed values rather than the patterns as written.
//
// The special case is isolated here and does not shape the general builder.
type RecordPredicate struct {
	All   bool
	Terms []RecordTerm
}

// RecordTerm is one selector: a zone, a reversed-name prefix, the exact reversed
// name that the prefix must NOT match (the pattern's apex), and a type set.
type RecordTerm struct {
	Zone string
	// ReversedPrefix is empty for a pattern with no wildcard label, in which
	// case ReversedExact is the whole of the match.
	ReversedPrefix string
	ReversedExact  string
	// ExcludeReversedApex is the apex the wildcard does not reach. Empty when
	// the pattern names an exact host.
	ExcludeReversedApex string
	Types               []string
}

// ReverseName reverses a DNS name label-wise, which is what turns "everything
// beneath this name" from a leading-wildcard LIKE into a prefix match an index
// can serve.
//
//	www.example.com  ->  com.example.www
func ReverseName(name string) string {
	labels := strings.Split(normalizeName(name), ".")
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	return strings.Join(labels, ".")
}

// CompileRecordPredicate folds the record selectors a subject holds for one
// action.
func CompileRecordPredicate(s Subject, action string) RecordPredicate {
	if s.AccountID == model.NilID {
		return RecordPredicate{}
	}
	if s.Unrestricted {
		return RecordPredicate{All: true}
	}
	var out RecordPredicate
	for _, g := range s.Grants {
		if g.AccountID != s.AccountID {
			continue
		}
		if g.Action != Wildcard && g.Action != action {
			continue
		}
		if g.Type == TypeAny && g.Form() == FormWildcard {
			return RecordPredicate{All: true}
		}
		if g.Type != TypeDNSRecord || g.Form() != FormSelector {
			continue
		}
		sel := *g.Selector
		term := RecordTerm{Zone: normalizeName(sel.Zone), Types: sel.Types}
		if rest, ok := strings.CutPrefix(normalizeName(sel.Names), "*."); ok {
			// Beneath the pattern, and NOT the pattern's own apex — which is the
			// rule that keeps a subtree grant away from the zone's own NS and MX.
			term.ReversedPrefix = ReverseName(rest) + "."
			term.ExcludeReversedApex = ReverseName(rest)
		} else {
			term.ReversedExact = ReverseName(sel.Names)
		}
		out.Terms = append(out.Terms, term)
	}
	return out
}

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
