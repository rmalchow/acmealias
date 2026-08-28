package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The routes reachable with no account selected (design P19), and the ones that
// administer an account from inside it.

type accountView struct {
	ID        model.ID  `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// Selected says which of these the session is currently acting in, so the
	// console can render the switcher without a second request.
	Selected bool `json:"selected"`
	// Kind is what the console navigates by (design C11): a personal account
	// gets Projects and Upgrade, a team account gets Projects, Audit and
	// Settings. It is sent rather than inferred from the absence of something,
	// because "this account has no members" and "this account cannot have
	// members" are the distinction the whole of C9 turns on.
	Kind model.AccountKind `json:"kind"`
	// Owner says whether the reader is the person this account answers to. It
	// is what the console needs to know whether to offer checkout, and it is
	// answered here rather than by comparing user identifiers in the browser —
	// the console has no user id, only a subject.
	Owner bool `json:"owner"`
	// Credits and Suspended are the funded state (design C3, C4). Both travel
	// with the LISTING and not only with the selected account, because a
	// suspended account is shown in the picker as suspended rather than omitted
	// — its owner is the only person who can restore it, and they have to be
	// able to reach it (task 6.1b).
	//
	// Credits is always zero for a personal account and the database keeps it
	// that way; the console renders no balance for one.
	Credits   int64 `json:"credits"`
	Suspended bool  `json:"suspended"`
	// SuspendedReason distinguishes the two suspensions in a sentence, since
	// only one of them is fixed by paying (design C6). Empty when usable.
	SuspendedReason string `json:"suspendedReason,omitempty"`
}

// toAccount renders one account for the picker, from the reader's point of view.
func toAccount(acc model.Account, selected bool, viewerID model.ID) accountView {
	return accountView{
		ID: acc.ID, Slug: acc.Slug, Name: acc.Name, CreatedAt: acc.CreatedAt,
		Selected: selected,
		Kind:     acc.Kind,
		Owner:    acc.OwnerUserID == viewerID,
		Credits:  acc.Credits,
		// Suspended is the negation of the one predicate (design C3, C6) rather
		// than a second reading of the same columns. A view that recomputed
		// "credits == 0" here would be a second definition of usable, and the
		// two would disagree the day a third clause is added.
		Suspended:       !store.Usable(&acc),
		SuspendedReason: store.SuspensionReason(&acc),
	}
}

// checkoutAmountsView is what the console offers on the buy screen. It is served
// from the same list the checkout validates against, so the screen cannot offer
// a button the API refuses (design C8).
type checkoutAmountsView struct {
	Amounts  []int64 `json:"amounts"`
	Currency string  `json:"currency"`
}

// listAccounts answers with the accounts this person belongs to.
//
// It is one of the four things reachable with no selection, and it is the one
// that makes the others usable: a session with no account needs somewhere to
// find out which accounts it could select.
func (a *admin) listAccounts(c echo.Context) error {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return err
	}
	accounts, err := store.AccountsForUser(ctx, a.db(), user.ID)
	if err != nil {
		return err
	}
	// No fallback, and no longer any need for one. There used to be one here for
	// the internal edition, which derived membership from a group claim and
	// wrote no account_members rows (design P18) — so the roster was empty and
	// the deployment's single account had to be substituted in.
	//
	// This list cannot be empty now: a personal account is materialized at
	// sign-in (task 3.1), so every signed-in caller belongs to at least one
	// account and "no account selected" means "you have not yet said which"
	// rather than "you have nothing" (task 7.6). If it ever IS empty, that is a
	// materialization that did not happen, and an empty picker is the honest
	// rendering of it — substituting something in would hide the bug behind an
	// account the user does not actually belong to, which is what the removed
	// fallback did.
	//
	// A SUSPENDED account is listed, and shown as suspended (task 6.1b). It is
	// the one thing the picker must not filter: its owner is the only person who
	// can restore it, and they reach it from here.
	out := make([]accountView, 0, len(accounts))
	for _, acc := range accounts {
		selected := sess.SelectedAccountID != nil && *sess.SelectedAccountID == acc.ID
		out = append(out, toAccount(acc, selected, user.ID))
	}
	return c.JSON(http.StatusOK, out)
}

// checkoutOffer answers what a checkout may ask for.
//
// Unscoped and reachable with no account selected, because it is what the "Add
// account" screen renders before any account exists (design C12). It discloses
// nothing: the offered amounts are the price list.
func (a *admin) checkoutOffer(c echo.Context) error {
	return c.JSON(http.StatusOK, checkoutAmountsView{Amounts: store.CheckoutAmounts, Currency: "EUR"})
}

// createAccount is a CHECKOUT that also creates the account (task 5.4, design
// C8).
//
// Creating and buying are one act, and the transaction is what makes that true:
// the account, its default roles, its owner's membership, its first credit and
// the audit entry commit together or not at all. An account that exists but was
// never funded is a state with no purpose — its owner would land on the pay
// screen of an account they had just made — so it is not a state this route can
// produce, however it fails.
//
// The amount comes from a fixed set and not from the caller (C8). There is no
// payment processor in the way to notice a body saying 1000000, so the only
// thing between the balance and an arbitrary number is that no arbitrary number
// is accepted.
//
// Nothing here says the flow is a stand-in, and that is deliberate (task 5.5). A
// banner reading "no payment will be taken" would mean the screen being
// exercised is not the screen that ships; what makes this safe is that there is
// no processor behind it.
func (a *admin) createAccount(c echo.Context) error {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	// Closed by default. Creating an account means creating a tenant, and a
	// deployment that authenticates an entire organization does not want every
	// person in it able to do that. The route exists in every deployment and
	// answers a refusal, rather than being absent — a route that is only hidden
	// is still reachable by anyone who reads the bundle.
	if !a.opts.Config.AllowAccountCreation {
		return echo.NewHTTPError(http.StatusForbidden, map[string]string{
			"message": "creating accounts is not enabled on this deployment",
			"code":    "account_creation_closed",
		})
	}
	var body struct {
		Slug   string `json:"slug"`
		Name   string `json:"name"`
		Amount int64  `json:"amount"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if err := dnsname.ValidateLabel(body.Slug); err != nil {
		return fmt.Errorf("account slug: %w", err)
	}
	if body.Name == "" {
		return fmt.Errorf("%w: account name is empty", inventory.ErrInvalid)
	}
	if !store.IsOfferedAmount(body.Amount) {
		return fmt.Errorf("%w: %d EUR; this service offers %v", store.ErrAmountNotOffered, body.Amount, store.CheckoutAmounts)
	}

	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return err
	}

	// One transaction, opened here rather than through a.mutate: mutate resolves
	// the scope from the session's SELECTION, and the account being created has
	// not been selected and does not yet exist.
	//
	// The audit entries are written onto THIS transaction, with a.recordOn. An
	// earlier version handed them to a.mutateAs from inside here, which ignores
	// the transaction it is nested in and opens a second one from the pool — so
	// the entries committed independently, and before the account they describe.
	// It only appeared to work because audit_entries.account_id carries no
	// foreign key, and it took a second connection out of the pool while holding
	// the first, which deadlocks on a pool of one. What the comment claimed —
	// "together or not at all" — is true now that it is one transaction.
	var acc *model.Account
	err = a.db().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := store.CreateAccountWithOwner(ctx, tx, body.Slug, body.Name, user.ID, body.Amount)
		if err != nil {
			return err
		}
		acc = created
		id := created.ID
		// No roles on the actor: the authority exercised here is being a signed
		// in person on a deployment that allows account creation, and there is
		// no account to have held a role in a moment ago. Recording an invented
		// one would be worse than recording none.
		actor := authz.ConsoleActor(c, nil).InAccount(id)
		return a.recordOn(ctx, tx, actor, []authz.Event{{
			Action: authz.ActionAccountCreate, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: created.Slug,
			After: map[string]any{
				"slug": created.Slug, "name": created.Name,
				"kind": string(created.Kind), "credits": created.Credits,
			},
		}, {
			// The credit is its own entry rather than a field on the creation,
			// because it is the entry a real payment will also write and the two
			// paths have to produce the same row (C8).
			Action: authz.ActionAccountCheckout, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: created.Slug,
			After: map[string]any{
				"amount": body.Amount, "currency": "EUR",
				"balanceBefore": int64(0), "balanceAfter": created.Credits,
			},
		}})
	})
	if err != nil {
		return err
	}
	// Select it immediately: someone who just created an account meant to act in
	// it, and leaving them unselected would put a screen between them and the
	// thing they came to do. Outside the transaction, because a selection that
	// survived a rolled-back creation would point at nothing.
	if err := store.SelectAccount(ctx, a.db(), sess.ID, &acc.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toAccount(*acc, true, user.ID))
}

// checkout adds credit to an account that already exists (task 5.1, design C8).
//
// It is NOT under the account-scoped route group, and that is the whole reason
// it is a separate handler rather than one more line under /accounts/:accountId.
// Every route in that group goes through a.scope(), which refuses a suspended
// account — and the person this route exists for is the owner of exactly such an
// account. Gating checkout on the account being usable would mean nobody could
// ever pay for one (design C4, C5).
//
// What replaces that gate is ownership. Not membership, and not the unrestricted
// role: an unrestricted member of a suspended account cannot even select it, and
// the owner holds no usable authority in it either — the account is the thing
// that is broken, and its owner is the person who can mend it.
func (a *admin) checkout(c echo.Context) error {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	accountID, err := model.ParseID(c.Param("accountId"))
	if err != nil {
		return fmt.Errorf("%w: accountId must be an identifier", inventory.ErrInvalid)
	}
	var body struct {
		Amount int64 `json:"amount"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if !store.IsOfferedAmount(body.Amount) {
		return fmt.Errorf("%w: %d EUR; this service offers %v", store.ErrAmountNotOffered, body.Amount, store.CheckoutAmounts)
	}
	ctx := c.Request().Context()
	viewer, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return err
	}
	// The account and the ownership check are ONE query, and that is the point
	// rather than a saved round trip. This route's account comes from a path
	// parameter — it has to, since it is deliberately outside the group where a
	// selection would have established it — so reading the row and then
	// comparing the owner made it an existence oracle: 403 for an account that
	// exists, 404 for one that does not, which is enough for anybody with a
	// session to confirm an identifier by guessing it. Asking for "the account
	// with this id that I own" answers 404 to both, and hands back a Scope built
	// from the row the database returned rather than from the parameter.
	sc, acc, err := store.AccountOwnedBy(ctx, a.db(), accountID, viewer.ID)
	if err != nil {
		return err
	}
	before := acc.Credits
	var after *model.Account
	if err := a.mutateAs(c, sc, nil, func(tx *gorm.DB) ([]authz.Event, error) {
		credited, err := store.Credit(ctx, tx, accountID, body.Amount)
		if err != nil {
			return nil, err
		}
		after = credited
		id := credited.ID
		return []authz.Event{{
			Action: authz.ActionAccountCheckout, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: credited.Slug,
			// Who, which account and how much, with the balance either side of
			// it. This is the record that has to be right on the day money is
			// involved, so it is right now, while none is.
			Before: map[string]any{"credits": before},
			After: map[string]any{
				"amount": body.Amount, "currency": "EUR",
				"balanceBefore": before, "balanceAfter": credited.Credits,
			},
		}}, nil
	}); err != nil {
		return err
	}
	selected := sess.SelectedAccountID != nil && *sess.SelectedAccountID == after.ID
	return c.JSON(http.StatusOK, toAccount(*after, selected, viewer.ID))
}

// transferAccount hands a team account to somebody else (task 3.6).
//
// Three things happen together and cannot be separated: the owner column moves,
// the recipient gets an unrestricted membership if they did not have one, and
// the change is recorded. An owner who is not a member is an owner who cannot
// open their own account; a transfer nobody can see afterwards is the one change
// where "who has this now" is the entire question.
//
// The refusals — a personal account is never transferred, and nobody ends up
// with two — live in store.TransferOwnership and, under it, in the schema's
// accounts_kind_is_immutable trigger and accounts_one_personal_per_owner index
// (design C1, C2).
func (a *admin) transferAccount(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	acc, err := store.GetAccount(ctx, a.db(), sc)
	if err != nil {
		return err
	}
	if _, err := a.requireOwner(c, acc, "transferring the account"); err != nil {
		return err
	}
	var body struct {
		Subject string `json:"subject"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.Subject == "" {
		return fmt.Errorf("%w: subject is empty; name the identity-provider subject of the new owner", inventory.ErrInvalid)
	}
	// The recipient is named by IdP subject, as addMember names one, because
	// that is the only identifier a future sign-in will match. Someone who has
	// never signed in is created here: the account has to be able to change
	// hands before the new owner's first visit.
	recipient, err := store.FindOrCreateUser(ctx, a.db(), body.Subject, body.Subject, "")
	if err != nil {
		return err
	}
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.TransferOwnership(ctx, tx, sc, recipient.ID); err != nil {
			return nil, err
		}
		// The membership, in the same transaction — store.TransferOwnership
		// says in its own doc comment that establishing it is the caller's job,
		// precisely so that it lands here where the transaction is.
		unrestricted, err := store.UnrestrictedRole(ctx, tx, sc)
		if err != nil {
			return nil, err
		}
		member, err := store.IsMember(ctx, tx, sc.AccountID(), recipient.ID)
		if err != nil {
			return nil, err
		}
		if !member {
			if err := store.AddMember(ctx, tx, sc, recipient.ID, unrestricted.ID); err != nil {
				return nil, err
			}
		}
		id := acc.ID
		return []authz.Event{{
			Action: authz.ActionAccountTransfer, ObjectType: authz.ObjectAccount,
			ObjectID: &id, ObjectLabel: acc.Slug,
			Before: map[string]any{"ownerUserId": acc.OwnerUserID},
			After:  map[string]any{"ownerUserId": recipient.ID, "ownerSubject": body.Subject},
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// selectAccount switches the session into one account.
//
// Membership is re-evaluated here rather than trusted from whenever the session
// was established (design P19): a membership revoked mid-session takes effect at
// the switch, which is a lever that did not exist when roles were global.
func (a *admin) selectAccount(c echo.Context) error {
	sess := authz.SessionFromContext(c)
	if sess == nil {
		return echo.ErrUnauthorized
	}
	// "none" clears the selection: leaving an account without signing out of the
	// service. Selection is nullable precisely so this is a state rather than an
	// error (design P19), and a person who has finished with one account should
	// not have to end their session to reach the picker.
	if c.Param("accountId") == "none" {
		if err := store.SelectAccount(c.Request().Context(), a.db(), sess.ID, nil); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
	accountID, err := model.ParseID(c.Param("accountId"))
	if err != nil {
		return fmt.Errorf("%w: accountId must be an identifier", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	ok, err := a.mayAct(c, sess, accountID)
	if err != nil {
		return err
	}
	if !ok {
		// Not-found rather than forbidden: whether an account exists is not
		// something a non-member should learn by asking.
		return echo.NewHTTPError(http.StatusNotFound, "no such account")
	}
	// The funding gate, WITH the owner exception (task 4.2, design C4).
	//
	// Without the exception this is a trap door: a suspended account cannot be
	// selected, every route that could pay for it is account-scoped, and so
	// nobody — the owner included — can ever restore it. The exception is what
	// makes the state recoverable, and it is narrow on purpose: the owner may
	// SELECT it, and what they then reach is the balance and checkout and
	// nothing else, because every scoped route is still refused by scopeFor
	// (task 4.3, design C5).
	//
	// A member who is not the owner is refused here, and refused as SUSPENDED
	// rather than as forbidden. The two must not share wording: "you do not have
	// permission" sends somebody to ask an administrator for a grant that would
	// not help, when what is true is that the account is stopped and its owner
	// can restart it.
	_, acc, err := store.AccountForSelection(ctx, a.db(), accountID)
	if err != nil {
		return err
	}
	if !store.Usable(acc) {
		user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
		if err != nil {
			return err
		}
		if acc.OwnerUserID != user.ID {
			return fmt.Errorf("%w: %s, and its owner can restore it", store.ErrAccountSuspended, store.SuspensionReason(acc))
		}
	}
	if err := store.SelectAccount(ctx, a.db(), sess.ID, &accountID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// mayAct reports whether a session's person may act in one account.
//
// One source, and that is the whole of it (design P18, task 2.4): a row in
// account_members. There used to be a second — a membership derived per request
// from the identity provider's group claim, for a deployment that had one
// account and no rows — and removing it (design C10) turns a rule with an
// exception into a rule.
func (a *admin) mayAct(c echo.Context, sess *model.Session, accountID model.ID) (bool, error) {
	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return false, err
	}
	return store.IsMember(ctx, a.db(), accountID, user.ID)
}

// --- roles, membership and grants (task 7.2) -------------------------------

type roleView struct {
	ID           model.ID    `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	Unrestricted bool        `json:"unrestricted"`
	Grants       []grantView `json:"grants"`
}

func toRole(r model.Role) roleView {
	v := roleView{ID: r.ID, Name: r.Name, Description: r.Description, Unrestricted: r.Unrestricted, Grants: []grantView{}}
	for _, g := range r.Grants {
		parsed := authz.GrantFromRow(model.APIKeyGrant{
			AccountID: g.AccountID, ObjectType: g.ObjectType, ObjectID: g.ObjectID,
			Selector: g.Selector, Action: g.Action, Inheritable: g.Inheritable, ID: g.ID,
		})
		v.Grants = append(v.Grants, grantView{
			Type: g.ObjectType, ObjectID: g.ObjectID, Selector: parsed.Selector,
			Action: g.Action, Inheritable: g.Inheritable, Summary: parsed.String(),
		})
	}
	return v
}

func (a *admin) listRoles(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "listing roles"); err != nil {
		return err
	}
	roles, err := store.RolesFor(c.Request().Context(), a.db(), sc)
	if err != nil {
		return err
	}
	out := make([]roleView, 0, len(roles))
	for _, r := range roles {
		out = append(out, toRole(r))
	}
	return c.JSON(http.StatusOK, out)
}

// subsetCheck is the write-time rule as the store takes it: one grant at a time,
// against a single grant the writer holds (task 4.3).
//
// NOT gated on the account kind, and this is the one thing in this file most
// likely to be deleted by accident (design C9, task 3.4). It is shared with the
// API-key path, and the grant model stays live in a PERSONAL account: a key
// narrower than its holder is good practice in an account of one, and the subset
// rule applies to it exactly as it does in a team account. Roles are how PEOPLE
// get authority and exist only where there are people to give them to; grants
// are also how a KEY is scoped, and every account has keys. Removing the roles
// screen from a personal account must not touch either of the two functions
// below.
func (a *admin) subsetCheck(c echo.Context, sc store.Scope) (store.GrantCheck, error) {
	writer, err := a.subject(c)
	if err != nil {
		return nil, err
	}
	return func(g store.Grant) error {
		parsed := authz.GrantFromRow(model.APIKeyGrant{
			AccountID: sc.AccountID(), ObjectType: g.ObjectType, ObjectID: g.ObjectID,
			Selector: g.Selector, Action: g.Action, Inheritable: g.Inheritable,
		})
		return authz.CheckSubset(writer, parsed)
	}, nil
}

// grantsFromBody turns the request's grants into rows, refusing any the writer
// does not itself hold (the subset rule).
func (a *admin) grantsFromBody(c echo.Context, sc store.Scope, in []grantView) ([]model.RoleGrant, error) {
	writer, err := a.subject(c)
	if err != nil {
		return nil, err
	}
	out := make([]model.RoleGrant, 0, len(in))
	for _, g := range in {
		row := model.RoleGrant{
			AccountID: sc.AccountID(), ObjectType: g.Type, ObjectID: g.ObjectID,
			Action: g.Action, Inheritable: g.Inheritable,
		}
		if g.Selector != nil {
			raw, err := authz.EncodeSelector(g.Selector)
			if err != nil {
				return nil, err
			}
			row.Selector = raw
		}
		parsed := authz.GrantFromRow(model.APIKeyGrant{
			AccountID: row.AccountID, ObjectType: row.ObjectType, ObjectID: row.ObjectID,
			Selector: row.Selector, Action: row.Action, Inheritable: row.Inheritable,
		})
		if err := authz.CheckSubset(writer, parsed); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func (a *admin) createRole(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "creating a role"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "administering roles"); err != nil {
		return err
	}
	var body struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Grants      []grantView `json:"grants"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.Name == "" {
		return fmt.Errorf("%w: role name is empty", inventory.ErrInvalid)
	}
	grants, err := a.grantsFromBody(c, sc, body.Grants)
	if err != nil {
		return err
	}
	check, err := a.subsetCheck(c, sc)
	if err != nil {
		return err
	}
	r := &model.Role{Name: body.Name, Description: body.Description, Grants: grants}
	var out roleView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.CreateRole(c.Request().Context(), tx, sc, r, check); err != nil {
			return nil, err
		}
		out = toRole(*r)
		return []authz.Event{{
			Action: authz.ActionRoleCreate, ObjectType: authz.ObjectRole,
			ObjectID: &r.ID, ObjectLabel: r.Name, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, out)
}

func (a *admin) updateRole(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "editing a role"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "administering roles"); err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Grants      []grantView `json:"grants"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	grants, err := a.grantsFromBody(c, sc, body.Grants)
	if err != nil {
		return err
	}
	check, err := a.subsetCheck(c, sc)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var out roleView
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		existing, err := store.RolesFor(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if len(existing) == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		before := toRole(existing[0])
		r := &model.Role{ID: id, Name: body.Name, Description: body.Description, Grants: grants}
		if err := store.UpdateRole(ctx, tx, sc, r, check); err != nil {
			return nil, err
		}
		after, err := store.RolesFor(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		out = toRole(after[0])
		return []authz.Event{{
			Action: authz.ActionRoleUpdate, ObjectType: authz.ObjectRole,
			ObjectID: &id, ObjectLabel: out.Name, Before: before, After: out,
		}}, nil
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) deleteRole(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "deleting a role"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "administering roles"); err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		existing, err := store.RolesFor(ctx, tx, sc, id)
		if err != nil {
			return nil, err
		}
		if len(existing) == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		before := toRole(existing[0])
		if err := store.DeleteRole(ctx, tx, sc, id); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionRoleDelete, ObjectType: authz.ObjectRole,
			ObjectID: &id, ObjectLabel: before.Name, Before: before,
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type memberView struct {
	ID          model.ID `json:"id"`
	UserID      model.ID `json:"userId"`
	Subject     string   `json:"subject"`
	DisplayName string   `json:"displayName"`
	RoleID      model.ID `json:"roleId"`
	RoleName    string   `json:"roleName"`
}

// listMembers is readable by any member of the account without a grant (task
// 7.4b): knowing who else is in the room is not a privilege, and a roster
// nobody can see is how an account ends up with a member nobody remembers
// adding.
func (a *admin) listMembers(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "listing members"); err != nil {
		return err
	}
	ms, err := store.ListMembers(c.Request().Context(), a.db(), sc)
	if err != nil {
		return err
	}
	out := make([]memberView, 0, len(ms))
	for _, m := range ms {
		v := memberView{ID: m.ID, UserID: m.UserID, RoleID: m.RoleID}
		if m.User != nil {
			v.Subject, v.DisplayName = m.User.Subject, m.User.DisplayName
		}
		if m.Role != nil {
			v.RoleName = m.Role.Name
		}
		out = append(out, v)
	}
	return c.JSON(http.StatusOK, out)
}

func (a *admin) addMember(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	// The nearest thing this service has to an invitation, and the place task
	// 3.3's "no invitations in a personal account" therefore lands: there is no
	// invitation table, no pending state and no email — a member is added by
	// naming their identity-provider subject. When invitations do arrive they
	// arrive behind this same refusal.
	if err := a.requireTeamAccount(c, sc, "adding a member"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "changing membership"); err != nil {
		return err
	}
	var body struct {
		Subject string   `json:"subject"`
		RoleID  model.ID `json:"roleId"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.Subject == "" {
		return fmt.Errorf("%w: subject is empty", inventory.ErrInvalid)
	}
	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), body.Subject, body.Subject, "")
	if err != nil {
		return err
	}
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		if err := store.AddMember(ctx, tx, sc, user.ID, body.RoleID); err != nil {
			return nil, err
		}
		return []authz.Event{{
			Action: authz.ActionMemberAdd, ObjectType: authz.ObjectMember,
			ObjectID: &user.ID, ObjectLabel: body.Subject,
			After: map[string]any{"subject": body.Subject, "roleId": body.RoleID},
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *admin) removeMember(c echo.Context) error {
	sc, err := a.scope(c)
	if err != nil {
		return err
	}
	if err := a.requireTeamAccount(c, sc, "removing a member"); err != nil {
		return err
	}
	if err := a.authorizeUnrestricted(c, "changing membership"); err != nil {
		return err
	}
	id, err := idParam(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.mutate(c, func(tx *gorm.DB) ([]authz.Event, error) {
		ms, err := store.ListMembers(ctx, tx, sc)
		if err != nil {
			return nil, err
		}
		var found *model.AccountMember
		for i := range ms {
			if ms[i].ID == id {
				found = &ms[i]
			}
		}
		if found == nil {
			return nil, gorm.ErrRecordNotFound
		}
		if err := store.RemoveMember(ctx, tx, sc, found.UserID, found.RoleID); err != nil {
			return nil, err
		}
		label := found.UserID.String()
		if found.User != nil {
			label = found.User.Subject
		}
		return []authz.Event{{
			Action: authz.ActionMemberRemove, ObjectType: authz.ObjectMember,
			ObjectID: &found.UserID, ObjectLabel: label,
			Before: map[string]any{"subject": label, "roleId": found.RoleID},
		}}, nil
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
