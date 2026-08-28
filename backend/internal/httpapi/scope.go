package httpapi

import (
	"fmt"
	"net/http"

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
// The answer is the account the session has selected, checked against the
// account the path names. Before design C10 it was always the same account,
// because a deployment had exactly one; the shape — resolve once per request,
// from the caller rather than from the request — is what did not change.

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
//
// The FUNDING check is not that trade and is made here, on every request (task
// 4.3, design C7). Two reasons it does not follow membership into selectAccount.
// It has to take effect without a new session — an account that runs out mid
// session must stop, and one that is paid for must resume, neither waiting for
// somebody to re-select (spec: "the account becomes usable without a new
// session"). And this is the one place the account is established for every
// account-scoped route, so a route added tomorrow is gated by existing, which is
// the same property that made the check worth putting in scope() and not in a
// middleware table somebody has to remember to extend.
func (a *admin) scopeFor(c echo.Context, accountID model.ID) (store.Scope, error) {
	ctx := c.Request().Context()
	// One read, not two. This used to be ScopeForSelection followed by
	// GetAccount — the same row, through two separate cross-account
	// transactions, on every account-scoped request.
	sc, acc, err := store.AccountForSelection(ctx, a.opts.DB, accountID)
	if err != nil {
		return store.Scope{}, err
	}
	if !store.Usable(acc) {
		// The Scope is NOT returned. A caller that got one and a refusal could
		// use the first and ignore the second; returning the zero value means a
		// handler that mishandles the error reads nothing rather than reading a
		// suspended account's inventory.
		//
		// Checkout is deliberately not among the routes that reach this: it
		// hangs off /accounts/:accountId directly rather than inside the scoped
		// group, because the owner of a suspended account has to be able to pay
		// for it and this is exactly what would refuse them (design C4, C5).
		return store.Scope{}, fmt.Errorf("%w: %s", store.ErrAccountSuspended, store.SuspensionReason(acc))
	}
	return sc, nil
}

// requireTeamAccount refuses roles, members and invitations in a personal
// account (task 3.3, design C9).
//
// A refusal and not an empty list. An empty collection reads as "there are none
// yet" and invites a client to render the Add button for something that can
// never exist; "there cannot be any" is a different sentence, and it is the true
// one. The account has exactly one person in it and that person is the account.
//
// What this must NOT be extended to is API keys and their grants. The grant
// model stays live in a personal account — a key narrower than its holder is
// good practice in an account of one, and the subset rule of P10 applies to it
// regardless of kind (design C9, task 3.4). Anyone adding a route here should
// ask whether it is about PEOPLE or about CREDENTIALS; only the first belongs.
func (a *admin) requireTeamAccount(c echo.Context, sc store.Scope, what string) error {
	acc, err := store.GetAccount(c.Request().Context(), a.opts.DB, sc)
	if err != nil {
		return err
	}
	if acc.Kind == model.AccountKindTeam {
		return nil
	}
	return fmt.Errorf("%w: %s", store.ErrPersonalAccountHasNoRoster, what)
}

// requireOwner gates what belongs to the person the account answers to rather
// than to a role in it: paying for it, and handing it over.
//
// Ownership is not a grant and not the unrestricted flag, and conflating them
// would be wrong in both directions. An unrestricted member can administer
// everything inside the account and is still not the person whose account it is;
// and the owner of a SUSPENDED account holds no usable authority at all — no
// route that resolves a scope will serve them — yet must still be able to pay.
// So this reads the owner column, and nothing else.
func (a *admin) requireOwner(c echo.Context, acc *model.Account, what string) (*model.User, error) {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return nil, echo.ErrUnauthorized
	}
	user, err := store.FindOrCreateUser(c.Request().Context(), a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return nil, err
	}
	if acc.OwnerUserID != user.ID {
		return nil, fmt.Errorf("%w: %s is the account owner's, and this account is not yours",
			authz.ErrNotPermitted, what)
	}
	return user, nil
}

// subject returns the authority the caller is acting with, in the scope's
// account, together with the names of the roles it came from.
//
// It is read from account_members and the roles those rows name (design P18).
// There used to be a second derivation here — roles computed per request from
// the session's group claim, mapped by four environment variables — and design
// C10 removed it, so membership is now only ever rows we wrote.
//
// The role names come back with the subject because the audit trail records the
// authority the actor held, and asking a second time could answer differently
// from the query the request was authorized against.
func (a *admin) subjectWithRoles(c echo.Context) (authz.Subject, []string, error) {
	sc, err := a.scope(c)
	if err != nil {
		return authz.Subject{}, nil, err
	}
	sess := authz.SessionFromContext(c)
	if sess == nil {
		// Unreachable behind the authenticated guard, and present so that
		// re-wiring a route without it fails closed rather than granting the
		// empty subject's authority — which is none, but silently.
		return authz.Subject{}, nil, echo.ErrUnauthorized
	}
	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return authz.Subject{}, nil, err
	}
	subj, roles, err := authz.SubjectForUser(ctx, a.db(), sc, user.ID)
	if err != nil {
		return authz.Subject{}, nil, err
	}
	return subj, authz.RoleNames(roles), nil
}

// subject is subjectWithRoles for the callers that only need the authority.
func (a *admin) subject(c echo.Context) (authz.Subject, error) {
	subj, _, err := a.subjectWithRoles(c)
	return subj, err
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

// currentUser resolves the session's subject to the user row, which is where the
// VERIFIED email address lives.
//
// It exists because invitations are bound to an address and the session is not:
// model.Session carries a subject, a display name and the provider's groups, and
// nothing about which address the provider proved. Adding a column to sessions
// would have put a second copy of that fact in a row that outlives a sign-in, so
// a session issued before somebody changed their address would carry the old
// one — and the address is what decides who may join an account.
//
// FindOrCreateUser rather than a read, matching every other caller here: the row
// is materialized at sign-in, and re-creating it costs one query on a path that
// is already doing several.
func (a *admin) currentUser(c echo.Context) (*model.User, error) {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return nil, echo.ErrUnauthorized
	}
	return store.FindOrCreateUser(c.Request().Context(), a.db(), sess.Subject, sess.DisplayName, "")
}
