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
	// The internal edition derives membership from a group claim and stores no
	// account_members rows (design P18), so the roster above is empty there. The
	// account is still the one the deployment has, and the session is already
	// acting in it.
	if len(accounts) == 0 {
		if sc, err := store.ScopeForBootstrap(ctx, a.db()); err == nil {
			acc, err := store.GetAccount(ctx, a.db(), sc)
			if err != nil {
				return err
			}
			accounts = []model.Account{*acc}
		}
	}
	out := make([]accountView, 0, len(accounts))
	for _, acc := range accounts {
		out = append(out, accountView{
			ID: acc.ID, Slug: acc.Slug, Name: acc.Name, CreatedAt: acc.CreatedAt,
			Selected: sess.SelectedAccountID != nil && *sess.SelectedAccountID == acc.ID,
		})
	}
	return c.JSON(http.StatusOK, out)
}

// createAccount lets any authenticated person start one, making themselves its
// first unrestricted member in the same transaction (task 7.4a).
//
// The two halves are indivisible on purpose: an account whose creation succeeded
// and whose membership did not is an account nobody can administer, and there is
// no route that could fix it — the fixing routes are the ones that need a
// member.
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
	var body struct{ Slug, Name string }
	if err := bind(c, &body); err != nil {
		return err
	}
	if err := dnsname.ValidateLabel(body.Slug); err != nil {
		return fmt.Errorf("account slug: %w", err)
	}
	if body.Name == "" {
		return fmt.Errorf("%w: account name is empty", inventory.ErrInvalid)
	}

	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return err
	}
	acc, err := store.CreateAccountWithOwner(ctx, a.db(), body.Slug, body.Name, user.ID)
	if err != nil {
		return err
	}
	// Select it immediately: someone who just created an account meant to act in
	// it, and leaving them unselected would put a screen between them and the
	// thing they came to do.
	if err := store.SelectAccount(ctx, a.db(), sess.ID, &acc.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, accountView{
		ID: acc.ID, Slug: acc.Slug, Name: acc.Name, CreatedAt: acc.CreatedAt, Selected: true,
	})
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
	if err := store.SelectAccount(ctx, a.db(), sess.ID, &accountID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// mayAct reports whether a session's person may act in one account.
//
// Memberships have two sources and the caller must not care which (design P18).
// In the public edition they are rows in account_members. In the internal
// edition there are no rows at all — membership is derived per request from the
// group claim — so what stands in for it is: this deployment has one account,
// and the group mapping resolved to at least one role in it.
func (a *admin) mayAct(c echo.Context, sess *model.Session, accountID model.ID) (bool, error) {
	ctx := c.Request().Context()
	user, err := store.FindOrCreateUser(ctx, a.db(), sess.Subject, sess.DisplayName, "")
	if err != nil {
		return false, err
	}
	member, err := store.IsMember(ctx, a.db(), accountID, user.ID)
	if err != nil {
		return false, err
	}
	if member {
		return true, nil
	}
	// Derived membership: the single-account edition.
	bootstrap, err := store.ScopeForBootstrap(ctx, a.db())
	if err != nil || bootstrap.AccountID() != accountID {
		return false, nil
	}
	names := authz.MappingFromConfig(a.opts.Config).RoleNamesFor(sess.Groups)
	if len(names) == 0 {
		return false, nil
	}
	roles, err := store.RolesByName(ctx, a.db(), bootstrap, names...)
	if err != nil {
		return false, err
	}
	return len(roles) > 0, nil
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
