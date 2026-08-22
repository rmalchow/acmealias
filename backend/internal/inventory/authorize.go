package inventory

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Permission checks live here, in the service layer, and not on the routes
// (design P7).
//
// They used to be route middleware: `authz.Require(action)` declared beside each
// path. That had one real virtue — a route that forgot one did not silently
// inherit "any signed-in user may" — and two problems that ended it. A verb
// checked at the door cannot name the object being acted on, which is the whole
// point of the new model; and the CLI does not go through routes at all, so
// every rule enforced there was a rule `adminctl` walked past.
//
// Putting the check next to the operation means the API and the CLI cannot
// diverge, because there is one implementation and both call it. The route table
// keeps its virtue a different way: nothing in this package performs an
// operation without calling authorize first, and the test in
// authorize_test.go asserts that by reading this package's own source.

// authorize refuses an action the subject holds no grant for.
//
// The refusal names the action and not the object. A subject that may not read
// an object must not learn from the error whether it exists — the store already
// makes absence and denial indistinguishable, and a chatty message here would
// undo that one layer up.
func authorize(s authz.Subject, action authz.Action, o authz.Object) error {
	if authz.Can(s, string(action), o) {
		return nil
	}
	return fmt.Errorf("%w: %s", authz.ErrNotPermitted, action)
}

// authorizeInAccount is the check for an operation that has no object yet —
// creating the first thing of its kind. The object is the account, which is not
// an ACL object (design P14), so what is checked is a wildcard within the
// anchor.
func authorizeInAccount(s authz.Subject, action authz.Action, sc store.Scope) error {
	return authorize(s, action, authz.Object{Type: authz.TypeAny, AccountID: sc.AccountID()})
}

// requireUnrestricted gates the operations that are governed by MEMBERSHIP
// rather than by a grant (design P14).
//
// Creating a project is the case that matters. A project is the top of the
// containment tree inside an account, so a grant permitting its creation would
// have to name the account as its object — and an account is deliberately not an
// object a grant can name, because a grant over the account is indistinguishable
// from being an administrator of it, said in a place that can be edited.
//
// So: you may create a project if you are a member of the account's unrestricted
// role, and not otherwise.
func requireUnrestricted(s authz.Subject, what string) error {
	if s.Unrestricted {
		return nil
	}
	return fmt.Errorf("%w: %s is governed by membership of the account's unrestricted role, not by a grant",
		authz.ErrNotPermitted, what)
}

// objectFor loads an object and describes it for the evaluator, in one step,
// within the scope.
//
// Load-then-check rather than check-then-load, deliberately. The check needs the
// object's ancestors, which only the row has; and the load is already confined by
// Scope, so a caller who may not see the object gets not-found from the store
// before any permission is evaluated. Absence and denial stay indistinguishable.
func objectFor[T any](ctx context.Context, db *gorm.DB, sc store.Scope, id model.ID, load func(context.Context, *gorm.DB, store.Scope, model.ID) (*T, error)) (*T, authz.Object, error) {
	row, err := load(ctx, db, sc, id)
	if err != nil {
		return nil, authz.Object{}, err
	}
	return row, authz.ObjectOf(row), nil
}
