package httpapi

import (
	"sync"

	"github.com/labstack/echo/v4"

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
