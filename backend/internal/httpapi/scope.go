package httpapi

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/labstack/echo/v4"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
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

// scope returns the account this request is confined to (design P19).
//
// Two things have to agree, and the redundancy is the point:
//
//   - the SESSION must have an account selected. A request from a session with
//     none is refused with a statement saying so, not with an empty result — an
//     empty result reads as "you have nothing", which is a different and wrong
//     answer.
//   - the PATH must name that same account. If the target came only from the
//     session, a forged or confused-deputy request would act in whatever account
//     happened to be selected, and the caller would never have had to know
//     which. Naming it makes the target something an attacker has to get right,
//     and a mismatch a refusal rather than a silent write into the wrong tenant.
//
// The scope itself is still built from what the DATABASE said — the session row
// — and never from the path. The path is a check, not a source.
func (a *admin) scope(c echo.Context) (store.Scope, error) {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return store.Scope{}, echo.ErrUnauthorized
	}
	if sess.SelectedAccountID == nil {
		return store.Scope{}, echo.NewHTTPError(http.StatusConflict, map[string]string{
			"message": "no account selected",
			"code":    "no_account_selected",
		})
	}
	named := c.Param("accountId")
	if named != "" {
		id, err := model.ParseID(named)
		if err != nil {
			return store.Scope{}, fmt.Errorf("%w: accountId must be an identifier", inventory.ErrInvalid)
		}
		if id != *sess.SelectedAccountID {
			// Not-found rather than forbidden: whether the named account exists
			// is not something this caller should learn from the mismatch.
			return store.Scope{}, echo.NewHTTPError(http.StatusNotFound, "no such account")
		}
	}
	return a.scopeFor(c, *sess.SelectedAccountID)
}

// scopeFor builds the confinement value for one account, having established that
// the caller may act in it.
//
// The membership check is what makes this safe, and it happens at SELECTION
// (selectAccount) rather than here — so a membership revoked mid-session takes
// effect at the next switch rather than at the next request. That is design
// P19's stated trade: re-checking per request would be stricter and would put a
// membership query on every read.
func (a *admin) scopeFor(c echo.Context, accountID model.ID) (store.Scope, error) {
	return store.ScopeForSelection(c.Request().Context(), a.opts.DB, accountID)
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
