package authz

import (
	"fmt"
	"slices"
	"strings"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The subset rule: a subject may grant only what it holds.
//
// When a grant is written to a role or to an API key, it is refused unless some
// single grant the writer holds covers it. "Single" is the load-bearing word,
// and it is a deliberate under-approximation.
//
// # Why coverage is not evaluated across several held grants
//
// A writer holding [load_balancer, A, *] and [load_balancer, B, *] cannot write
// [project, P, *] even when A and B are the only load balancers in P today. The
// union does cover it, and the check refuses anyway.
//
// Two reasons. The union is true only of the inventory as it stands: add load
// balancer C to P tomorrow and the grant the writer issued now reaches something
// they never had authority over — an escalation that happens with nobody
// touching a permission. And a union test is a set-cover problem over patterns
// and wildcards, which is where an evaluator acquires the bugs that make it
// unreviewable.
//
// The cost is real and is accepted: a legitimate grant is sometimes refused, and
// the writer has to be given the wider grant explicitly rather than assembling
// it. That is the safe direction to be wrong in.

// Covers reports whether held ⊒ proposed: everything the proposed grant permits,
// the held grant already permits.
func Covers(held, proposed Grant) bool {
	if held.AccountID != proposed.AccountID {
		return false
	}
	if held.Action != Wildcard && held.Action != proposed.Action {
		return false
	}
	if !typeCovers(held, proposed) {
		return false
	}
	return identifierCovers(held, proposed)
}

// typeCovers compares the object-type slots, including the case where the held
// grant names an ancestor type and reaches down into the proposed one.
func typeCovers(held, proposed Grant) bool {
	switch {
	case held.Type == TypeAny:
		return true
	case held.Type == proposed.Type:
		return true
	case Contains(held.Type, proposed.Type):
		// Reaching down is only possible for an inheritable grant, and the
		// proposed grant must not out-reach it: a non-inheritable held grant
		// covers nothing below itself.
		return held.Inheritable
	default:
		return false
	}
}

// identifierCovers compares the identifier slots.
func identifierCovers(held, proposed Grant) bool {
	switch held.Form() {
	case FormWildcard:
		// A wildcard identifier within the account covers every identifier of a
		// type the held grant reaches — but a held grant that is not inheritable
		// covers only grants that are likewise confined to its own type.
		if !held.Inheritable && proposed.Inheritable && proposed.Type == held.Type {
			return false
		}
		return true

	case FormID:
		switch proposed.Form() {
		case FormID:
			if held.Type == proposed.Type {
				if *held.ObjectID != *proposed.ObjectID {
					return false
				}
				// An inheritable proposal reaches further than a
				// non-inheritable holding of the same object.
				return held.Inheritable || !proposed.Inheritable
			}
			// The held grant names an ancestor type. It cannot be checked
			// against the proposed object's id without loading that object, and
			// the writer's own authority must not depend on a lookup that can
			// race with a move. Coverage is therefore refused unless the caller
			// resolved the ancestry first — see CoversResolved.
			return false
		case FormWildcard, FormSelector:
			// One object never covers a wildcard or a set.
			return false
		}

	case FormSelector:
		if proposed.Form() != FormSelector {
			return false
		}
		return selectorCovers(*held.Selector, *proposed.Selector)
	}
	return false
}

// selectorCovers is containment for the DNS record selector: same zone, a
// pattern that contains the proposed one, and a type set that is a superset.
func selectorCovers(held, proposed DNSSelector) bool {
	if !strings.EqualFold(normalizeName(held.Zone), normalizeName(proposed.Zone)) {
		return false
	}
	if !PatternContains(held.Names, proposed.Names) {
		return false
	}
	if len(proposed.Types) == 0 {
		return false
	}
	for _, t := range proposed.Types {
		if !slices.ContainsFunc(held.Types, func(h string) bool { return strings.EqualFold(h, t) }) {
			return false
		}
	}
	return true
}

// CoversResolved is Covers for the case where the held grant names an ancestor
// of the proposed grant's object, and the caller has already resolved that
// object's ancestry.
//
// ancestors maps an object type to the identifier this object has of that type,
// which is exactly what design P3 materialized onto every row. The caller passes
// it rather than the evaluator looking it up, because the authority of a write
// must not depend on a query that could race with the object being moved.
func CoversResolved(held, proposed Grant, ancestors map[ObjectType]model.ID) bool {
	if Covers(held, proposed) {
		return true
	}
	if held.Form() != FormID || proposed.Form() != FormID {
		return false
	}
	if !Contains(held.Type, proposed.Type) || !held.Inheritable {
		return false
	}
	a, ok := ancestors[held.Type]
	if !ok || a == model.NilID {
		return false
	}
	return a == *held.ObjectID
}

// CheckSubset is the write-time gate. It returns nil when some single grant the
// writer holds covers the proposed one, and an error naming the refusal
// otherwise.
func CheckSubset(writer Subject, proposed Grant) error {
	if err := proposed.Validate(); err != nil {
		return err
	}
	if proposed.AccountID != writer.AccountID {
		return fmt.Errorf("%w: a grant cannot be written into another account", ErrInvalidGrant)
	}
	// The unrestricted role holds everything in its account, so it covers every
	// grant anchored there — and only there.
	if writer.Unrestricted {
		return nil
	}
	for _, held := range writer.Grants {
		if Covers(held, proposed) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not covered by any single grant the writer holds",
		ErrNotPermitted, proposed)
}
