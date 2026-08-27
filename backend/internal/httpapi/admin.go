package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// The management API: everything under /api/v1 on the admin listener (design
// D9). Three things are true of every route here and are wired once, in
// RegisterAdmin, rather than repeated per handler:
//
//   - the session is resolved once, by authz.Guard.Authenticated, which also
//     refuses an authenticated user who maps to no role at all;
//   - the permission is checked in internal/inventory, beside the operation and
//     with the object in hand, so the API and the CLI cannot diverge (design
//     P7). Session administration is the one exception and says why below;
//   - every error leaves through one JSON shape (errors.go), so the console
//     never has to guess whether a failure is HTML, a bare string, or an
//     object.
//
// There is deliberately no service layer between these handlers and
// internal/inventory: the validation rules live there precisely so the CLI and
// the API cannot diverge, and a second layer here would only be a place for
// them to.

// ConsoleAuth is the part of *oidcauth.Service the management API uses. It is
// an interface so httpapi does not depend on the OIDC provider machinery, and
// so the role tests can run the whole route table against a fake session.
type ConsoleAuth interface {
	Login(w http.ResponseWriter, r *http.Request) error
	Callback(w http.ResponseWriter, r *http.Request) error
	Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) error
	CSRF() echo.MiddlewareFunc
}

// AdminOptions configure the management API. DB, Guard and Auth are required.
type AdminOptions struct {
	// DB is the datastore every handler reads and writes through.
	DB *gorm.DB

	// Guard resolves the session and derives the roles a request holds.
	Guard *authz.Guard

	// Auth serves the sign-in routes and supplies the CSRF middleware.
	Auth ConsoleAuth

	// View is the in-memory zone view. When set, it is refreshed after an
	// alias appears or disappears, so a newly onboarded domain answers
	// without waiting for the periodic reload. nil skips that.
	View *zoneview.View

	// Config is the effective configuration: the DNS plan reads the zone and
	// the CAA issuers from it, verification reads the resolver, and
	// GET /api/v1/config renders it redacted.
	Config config.Config

	// Metrics receives the audit-entry counter. nil skips it; the service
	// always sets it (see ServerOptions.Metrics), so a nil here means a test.
	Metrics *obs.Metrics

	// Log receives audit-write failures and 5xx detail. nil uses slog.Default().
	Log *slog.Logger
}

// RegisterAdmin mounts the management API and the sign-in routes on e, and
// installs the JSON error handler.
//
// It also registers a catch-all under /api/, which is what keeps an unknown
// management path a JSON 404 rather than the console's index.html: the SPA
// fallback (group 16) is registered at /*, and Echo prefers the longer static
// prefix, so this route wins for anything under /api/ whatever the fallback
// does. A typo'd endpoint that returns HTML turns into a very confusing bug
// report (design D9).
func RegisterAdmin(e *echo.Echo, o AdminOptions) error {
	switch {
	case o.DB == nil:
		return errors.New("httpapi: the management API needs a database handle")
	case o.Guard == nil:
		return errors.New("httpapi: the management API needs an authz guard")
	case o.Auth == nil:
		return errors.New("httpapi: the management API needs a console authenticator")
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	a := &admin{opts: o, resolver: verificationResolver(o.Config, o.Log)}

	e.HTTPErrorHandler = a.handleError

	api := e.Group("/api/v1")

	// Sign-in is not behind the session guard, for the obvious reason.
	api.GET("/auth/login", func(c echo.Context) error { return o.Auth.Login(c.Response(), c.Request()) })
	api.GET("/auth/callback", func(c echo.Context) error { return o.Auth.Callback(c.Response(), c.Request()) })
	// Logout is reachable by GET as well as POST: the console offers it as a
	// link, and a forged sign-out is a nuisance rather than a vulnerability —
	// it destroys a credential, it cannot use one.
	logout := func(c echo.Context) error {
		if err := o.Auth.Logout(c.Request().Context(), c.Response(), c.Request()); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
	api.GET("/auth/logout", logout)
	api.POST("/auth/logout", logout)

	// CSRF first, so even a 401 carries a fresh XSRF-TOKEN cookie for the
	// console to send back after signing in. Echo validates the token on
	// everything except GET/HEAD/OPTIONS/TRACE, so mounting it on the whole
	// group still only guards the state-changing routes.
	sec := api.Group("", o.Auth.CSRF(), o.Guard.Authenticated())

	// No permission middleware on the routes (design P7). The check moved into
	// internal/inventory, where it can name the object being acted on and where
	// `adminctl` calls the same code — a rule enforced at the door was a rule
	// the CLI walked straight past.
	//
	// Two things still gate every route below: the session, and the account it
	// is confined to. Both are properties of the caller rather than of the path.
	//
	// The route table loses the property that made the middleware worth having —
	// a route that forgot its permission failed closed. That is now the job of
	// the test in internal/inventory that reads the package's own source and
	// asserts nothing performs an operation without authorizing it first.
	//
	// Sessions were the last route with a middleware check, gated on the global
	// ADMIN role. Design C10 removed the global roles, and there is no
	// per-account role that could stand in: a session belongs to a PERSON, not
	// to an account, so an account administrator holding power over one would be
	// holding power over somebody who may not be in their account at all. The
	// routes below are therefore confined to the caller's own sessions, and
	// administering anybody else's is a staff-plane power that will live under
	// /api/admin/** — see admin_sessions.go.

	// --- routes reachable with no account selected (design P19) -------------
	//
	// Exactly four things, and the list is short on purpose: a session with no
	// selection is a person who has not yet said which account they are acting
	// in, and everything below this block would otherwise act in whichever one
	// happened to be selected.
	//
	// The contract, served unauthenticated: a contract nobody can read without
	// credentials is not a published one, and it describes only what the API
	// already answers.
	registerOpenAPI(e)

	// Every signed-in user may read their own identity; there is nothing here
	// they did not already supply.
	sec.GET("/me", a.me)
	sec.GET("/accounts", a.listAccounts)
	sec.POST("/accounts", a.createAccount)
	sec.POST("/accounts/:accountId/select", a.selectAccount)

	// Checkout is here rather than under the account-scoped group below, and the
	// placement is the decision rather than an oversight (design C4, C5, task
	// 5.1). Everything under that group resolves a scope, and resolving a scope
	// refuses a suspended account — which is precisely the account whose owner
	// needs this route. Under `acct` it would be the one way to fix a suspended
	// account, gated on the account not being suspended.
	//
	// What gates it instead is ownership, checked in the handler. The offer is
	// readable with no account selected because the "Add account" screen renders
	// it before there is an account.
	sec.GET("/checkout/amounts", a.checkoutOffer)
	sec.POST("/accounts/:accountId/checkout", a.checkout)

	// --- account-scoped routes ---------------------------------------------
	//
	// The account is named in the path AND checked against the session's
	// selection (design P19). That looks redundant and is not: if the target
	// came only from the session, a forged or confused-deputy request would act
	// in whatever account happened to be selected, and the caller would never
	// have had to know which. Naming it makes the target something an attacker
	// has to get right, and a mismatch a refusal rather than a silent write into
	// the wrong tenant.
	//
	// It also makes the public API usable by programs, which should not have to
	// establish selection state before addressing a resource.
	acct := sec.Group("/accounts/:accountId")

	acct.GET("/projects", a.listProjects)
	acct.POST("/projects", a.createProject)
	acct.GET("/projects/:id", a.getProject)
	acct.PATCH("/projects/:id", a.updateProject)
	acct.DELETE("/projects/:id", a.deleteProject)

	// Reporting reads history and writes nothing.
	acct.GET("/projects/:id/usage", a.getProjectUsage)
	acct.GET("/projects/:id/inventory-history", a.getProjectInventoryHistory)

	acct.GET("/projects/:id/loadbalancers", a.listLoadBalancers)
	acct.POST("/projects/:id/loadbalancers", a.createLoadBalancer)
	acct.GET("/loadbalancers/:id", a.getLoadBalancer)
	acct.PATCH("/loadbalancers/:id", a.updateLoadBalancer)
	acct.DELETE("/loadbalancers/:id", a.deleteLoadBalancer)

	acct.POST("/loadbalancers/:id/nodes", a.createNode)
	acct.PATCH("/nodes/:id", a.updateNode)
	acct.DELETE("/nodes/:id", a.deleteNode)

	acct.GET("/loadbalancers/:id/domains", a.listDomains)
	acct.POST("/loadbalancers/:id/domains", a.createDomain)
	acct.DELETE("/domains/:id", a.deleteDomain)

	acct.GET("/loadbalancers/:id/interactions", a.listInteractions)
	acct.GET("/loadbalancers/:id/keys", a.listKeys)
	acct.POST("/loadbalancers/:id/keys", a.createKey)
	acct.PUT("/keys/:id/networks", a.restrictKey)
	acct.PUT("/keys/:id/expiry", a.setKeyExpiry)
	acct.POST("/keys/:id/rotate", a.rotateKey)
	acct.DELETE("/keys/:id", a.revokeKey)

	acct.GET("/loadbalancers/:id/plan", a.getPlan)
	acct.GET("/loadbalancers/:id/plan/zone", a.getPlanZone)
	// Verification is a read: it resolves names and stores nothing, so it is a
	// GET and needs no CSRF token.
	acct.GET("/loadbalancers/:id/verification", a.verifyPlan)

	acct.GET("/challenges", a.listChallenges)
	acct.GET("/audit", a.listAudit)
	acct.GET("/config", a.getConfig)

	// Roles, membership and grants: an account administering itself.
	acct.GET("/roles", a.listRoles)
	acct.POST("/roles", a.createRole)
	acct.PUT("/roles/:id", a.updateRole)
	acct.DELETE("/roles/:id", a.deleteRole)
	acct.GET("/members", a.listMembers)
	acct.POST("/members", a.addMember)
	acct.DELETE("/members/:id", a.removeMember)

	// Transfer IS scoped, unlike checkout: handing over an account is something
	// its owner does to a working account, and an owner who cannot pay for it
	// has nothing to hand over yet.
	acct.POST("/transfer", a.transferAccount)

	// Sessions belong to a person rather than to an account, so they are not
	// under /accounts — and for the same reason they are the caller's own.
	sec.GET("/sessions", a.listSessions)
	sec.DELETE("/sessions", a.revokeSubjectSessions)
	sec.DELETE("/sessions/:handle", a.revokeSession)

	// --- the internal surface (design P15) ----------------------------------
	//
	// /api/admin/** is the staff plane's tree: unversioned, undocumented, and
	// deliberately a SEPARATE tree rather than a prefix inside the public one.
	// That separation is the point — it keeps a staff-only operation from being
	// one forgotten annotation away from appearing in a public schema.
	//
	// It is empty until `staff-plane` fills it, and answers 404 rather than
	// nothing at all so that a caller learns the surface exists and their path
	// does not.
	e.Any("/api/admin/*", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound,
			"no such staff endpoint: "+c.Request().URL.Path)
	})

	e.Any("/api/*", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, "no such management endpoint: "+c.Request().URL.Path)
	})
	return nil
}

type admin struct {
	opts AdminOptions
	// resolver is the "host:port" live verification queries, resolved once at
	// registration. Empty means none could be determined and the verification
	// route says so rather than silently querying nothing.
	resolver string
}

func (a *admin) db() *gorm.DB      { return a.opts.DB }
func (a *admin) log() *slog.Logger { return a.opts.Log }

// limits is the configured room an account has, handed to internal/inventory on
// every call that can be refused for lack of it (design C13). It is read off the
// effective config per request rather than captured once, so a reload takes
// effect without a restart — the same property the funding check has.
func (a *admin) limits() inventory.Limits {
	return inventory.Limits{ProjectsPerPersonalAccount: a.opts.Config.ProjectsPerPersonalAccount}
}

func (a *admin) planOptions() inventory.PlanOptions {
	return inventory.PlanOptions{Zone: a.opts.Config.Zone, CAAIssuers: a.opts.Config.CAAIssuers}
}

// idParam reads an identifier path parameter. A malformed id is a client
// mistake, so it is a 400 rather than a 404 that would read as "that object was
// deleted".
//
// Identifiers are UUIDs since design P17, so this rejects anything that is not
// one — including the integers earlier versions of this API used, which is the
// intended behaviour rather than a compatibility gap: there is no integer whose
// meaning survives the conversion.
func idParam(c echo.Context, name string) (model.ID, error) {
	id, err := model.ParseID(c.Param(name))
	if err != nil || id == model.NilID {
		return model.NilID, fmt.Errorf("%w: %s must be an identifier, got %q", inventory.ErrInvalid, name, c.Param(name))
	}
	return id, nil
}

// bind decodes a JSON body, mapping a malformed one onto the same 400 shape
// as a validation rejection.
func bind(c echo.Context, dst any) error {
	if err := c.Bind(dst); err != nil {
		return fmt.Errorf("%w: %s", inventory.ErrInvalid, err)
	}
	return nil
}

// mutate runs one mutation and the audit entries describing it inside a single
// transaction.
//
// That is the whole point of this helper: the change and its record commit
// together or not at all. fn does its work through the tx it is handed —
// never through a.db() — and returns the entries to write; anything fn needs
// to render afterwards it assigns to a variable in the enclosing handler.
//
// A failed audit write rolls the mutation back, so the 500 below means
// "nothing was applied". Work that must NOT be undone by that rollback — and
// work that must not run at all if the change is rolled back, refreshView
// above all — belongs after mutate returns, not inside fn.
func (a *admin) mutate(c echo.Context, fn func(tx *gorm.DB) ([]authz.Event, error)) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	// Every entry carries the account the change happened inside, so a tenant
	// reading their own trail sees their own entries and nothing else. It comes
	// off the scope rather than off the request, for the same reason the scope
	// itself does.
	// The roles are the ones this request was authorized against, resolved from
	// the caller's membership of that account rather than from anything on the
	// session (design C10). An entry recording no authority when the actor held
	// some is worse than no entry: it reads as a system action.
	_, roles, err := a.subjectWithRoles(c)
	if err != nil {
		return err
	}
	return a.mutateAs(c, sc, roles, fn)
}

// mutateAs is mutate for the two callers that cannot let it resolve their scope
// and their roles: checkout in an account that has none yet, and checkout in an
// account so suspended that a.scope() refuses it (design C4, C8).
//
// Splitting it out rather than making mutate's resolution conditional keeps the
// dangerous half explicit — a caller here is SAYING which account it is writing
// into, and the two that do say it are the two that had no other way to.
// Everything else must keep going through mutate, where the account is a
// property of the caller rather than an argument.
func (a *admin) mutateAs(c echo.Context, sc store.Scope, roles []string, fn func(tx *gorm.DB) ([]authz.Event, error)) error {
	return a.record(c, authz.ConsoleActor(c, roles).InAccount(sc.AccountID()), fn)
}

// mutateForPerson is mutate for the changes that belong to a PERSON rather than
// to an account: ending one's own sessions.
//
// It resolves no scope, and that is the fix rather than a shortcut. The session
// routes were going through a.mutate, which resolves the caller's account — so
// "sign me out everywhere" answered 409 no_account_selected from the picker,
// which since task 2.2 is the state every sign-in begins in, and 402 from a
// suspended account. "My laptop was stolen" is exactly the moment somebody is
// sitting at the picker, and it was the one moment the button did not work.
//
// The audit entry is written with NO account (authz.Actor.AccountID stays nil,
// which audit_entries.account_id allows) instead of being filed into whichever
// account happened to be selected. A session revocation did not happen inside a
// tenant, and putting it in one both misattributes it and hides it from the
// person whose session it was the moment they switch accounts.
//
// Nothing else may use this. Every other mutation acts on tenant data and must
// keep going through mutate, where the account is a property of the caller.
func (a *admin) mutateForPerson(c echo.Context, fn func(tx *gorm.DB) ([]authz.Event, error)) error {
	return a.record(c, authz.ConsoleActor(c, nil), fn)
}

// record is the transaction the three mutate helpers share: the change and its
// audit entries commit together or not at all.
func (a *admin) record(c echo.Context, actor authz.Actor, fn func(tx *gorm.DB) ([]authz.Event, error)) error {
	ctx := c.Request().Context()
	return a.db().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		events, err := fn(tx)
		if err != nil {
			return err
		}
		return a.recordOn(ctx, tx, actor, events)
	})
}

// recordOn writes the entries describing a change onto the transaction that made
// it. It is separate from record for the one caller that opens its own
// transaction because it has to — createAccount, which is writing the account
// the entries are about — and which therefore cannot hand a closure to a helper
// that would open a second one.
func (a *admin) recordOn(ctx context.Context, tx *gorm.DB, actor authz.Actor, events []authz.Event) error {
	for _, e := range events {
		if err := authz.Record(ctx, tx, a.opts.Metrics, actor, e); err != nil {
			a.log().ErrorContext(ctx, "audit entry not recorded",
				slog.String("action", e.Action), slog.String("object_type", e.ObjectType), slog.Any("error", err))
			return echo.NewHTTPError(http.StatusInternalServerError,
				"nothing was applied: the change could not be recorded in the audit trail")
		}
	}
	return nil
}

// refreshView reloads the zone view after an alias appeared or disappeared, so
// a new label answers — and a removed one stops answering — without waiting
// for the periodic reload. A failure is logged, not returned: the inventory
// change is correct and the periodic refresh will catch up.
//
// It must be called AFTER the transaction commits, never from inside mutate's
// fn. The refresh reads through its own connection, so from inside it would
// either not see the change at all or — worse, if the transaction then rolls
// back — leave the in-memory view serving a label that no longer exists.
func (a *admin) refreshView(c echo.Context) {
	if a.opts.View == nil {
		return
	}
	ctx := c.Request().Context()
	if err := a.opts.View.Refresh(ctx); err != nil {
		a.log().ErrorContext(ctx, "zone view not refreshed after an inventory change", slog.Any("error", err))
	}
}
