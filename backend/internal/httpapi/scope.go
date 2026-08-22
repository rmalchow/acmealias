package httpapi

import (
	"fmt"
	"sync"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// Scope resolution for the management API.
//
// Every read and write below this point is confined to one account by a
// store.Scope, and this is the only place in the HTTP layer that produces one.
// That is deliberate: a Scope cannot be built from a path parameter or a request
// body, so the confinement of a request is decided by who is asking rather than
// by what they asked for.
//
// In this edition the answer is always the same account, because there is exactly
// one. The public edition replaces this resolver with one that reads the session's
// selected account and the caller's membership of it; the shape here — resolve
// once per request, from the caller rather than from the request — is what stays.
type scopeResolver struct {
	once sync.Once
	sc   store.Scope
	err  error
}

// scope returns the scope this request is confined to.
//
// The single-account lookup is memoized because it cannot change while the
// process runs: the account is seeded by the migration and the internal edition
// never gains a second one. A deployment that did would fail the lookup rather
// than pick one, which is the safe direction.
func (a *admin) scope(c echo.Context) (store.Scope, error) {
	a.scopes.once.Do(func() {
		a.scopes.sc, a.scopes.err = store.ScopeForBootstrap(c.Request().Context(), a.opts.DB)
	})
	return a.scopes.sc, a.scopes.err
}

// subject returns the authority the caller is acting with, in the scope's
// account.
//
// In this edition it is derived per request from the session's group claim
// (design P18): the mapping names role rows, the rows carry grants, and the
// grants are the authority. Nothing is stored, so a group membership removed at
// the identity provider takes effect on the next request rather than on the next
// sign-in.
//
// The public edition reads memberships from account_members instead. Callers
// cannot tell which they were handed, and must not try to: that is what keeps
// one evaluator serving both.
func (a *admin) subject(c echo.Context) (authz.Subject, error) {
	sc, err := a.scope(c)
	if err != nil {
		return authz.Subject{}, err
	}
	sess := authz.SessionFromContext(c)
	if sess == nil {
		// Unreachable behind the authenticated guard, and present so that
		// re-wiring a route without it fails closed rather than granting the
		// empty subject's authority — which is none, but silently.
		return authz.Subject{}, echo.ErrUnauthorized
	}
	return authz.SubjectForGroups(c.Request().Context(), a.opts.DB, sc, authz.MappingFromConfig(a.opts.Config), sess.Groups)
}

// authorize checks one action against one object for the acting caller.
//
// Most permission checks live in internal/inventory, beside the operation and
// shared with the CLI (design P7). These are the handlers that do not go through
// that package — updating a name, and the key lifecycle — and they are exactly
// the ones that silently lost their check when the route middleware went away.
// The test in internal/inventory cannot see them, so they are listed in
// admin_test.go's route table instead, which is what caught this.
func (a *admin) authorize(c echo.Context, action authz.Action, o authz.Object) error {
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	if authz.Can(subj, string(action), o) {
		return nil
	}
	return fmt.Errorf("%w: %s", authz.ErrNotPermitted, action)
}

// authorizeUnrestricted gates what membership governs rather than a grant
// (design P14): operations on a project itself, which no grant may name because
// no grant may name the account above it.
func (a *admin) authorizeUnrestricted(c echo.Context, what string) error {
	subj, err := a.subject(c)
	if err != nil {
		return err
	}
	if subj.Unrestricted {
		return nil
	}
	return fmt.Errorf("%w: %s is governed by membership of the account's unrestricted role, not by a grant",
		authz.ErrNotPermitted, what)
}
