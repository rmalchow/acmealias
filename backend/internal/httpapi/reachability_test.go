package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// What is reachable from the two states this change made ordinary, and what a
// member holding nothing may read.
//
// Both are the same shape of mistake and neither was visible before design C10.
// "No account selected" used to be unreachable (sign-in selected the deployment's
// one account) and "suspended" did not exist, so every route that quietly
// required a usable selected account was correct by accident. And every
// authenticated caller used to hold one of four global roles, all of which
// carried inventory.view, so a read that checked no grant was gated by the door
// instead. The door is gone (authz.Guard no longer refuses a roleless user) and
// both states are now the front page.

// zeroGrantSession is a member of the account holding a role that confers
// nothing — the state a customer reaches by making their own role and leaving
// the grant composer empty, and the state no test could reach while roles were
// four constants.
func zeroGrantSession(t *testing.T, v *adminEnv, subject string) *client {
	t.Helper()
	ctx := t.Context()
	role := &model.Role{Name: "NOTHING-" + subject, Description: "holds no grants"}
	if err := store.CreateRole(ctx, v.db, v.sc, role, store.AllowAnyGrant); err != nil {
		t.Fatalf("create a zero-grant role: %v", err)
	}
	user, err := store.FindOrCreateUser(ctx, v.db, subject, subject, "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.AddMember(ctx, v.db, v.sc, user.ID, role.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return v.session(subject)
}

// clearSelection puts a session back in the state every sign-in now begins in
// (task 2.2): authenticated, with no account chosen.
func clearSelection(t *testing.T, v *adminEnv, cl *client) {
	t.Helper()
	if err := store.SelectAccount(t.Context(), v.db, cl.sessionID, nil); err != nil {
		t.Fatalf("clear the selection: %v", err)
	}
}

// TestIntegrationMeAnswersInASuspendedAccount is the screen design C5 asks for,
// asserted at the one route that has to work for it to render at all.
//
// C4 lets the owner of a suspended account select it, because otherwise nobody
// can ever pay for one. Everything they then reach is refused with 402, which is
// correct — except that the console shell starts by asking /me who it is talking
// to, and /me resolved the caller's authority through the same scope that
// refuses a suspended account. An owner who did exactly what the design tells
// them to do got a 402 for their own identity: no name, no navigation, no
// checkout button, because the console's session loader treats a failed /me as
// "not signed in".
func TestIntegrationMeAnswersInASuspendedAccount(t *testing.T) {
	v := newAdminEnv(t)
	owner := v.session("fixture-owner", roleAdmin)
	setBalance(t, v.db, v.sc.AccountID(), 0)

	rec := owner.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me with a suspended account selected: %d %s, want 200 — "+
			"the owner cannot be shown the balance and checkout by a console that cannot identify them",
			rec.Code, rec.Body.String())
	}
	var me meView
	decode(t, rec, &me)
	if me.SelectedAccountID == nil || *me.SelectedAccountID != v.sc.AccountID() {
		t.Fatalf("/me reports the selection as %v; the console needs it to address the account it is about to pay for",
			me.SelectedAccountID)
	}
	if me.Subject != "fixture-owner" {
		t.Fatalf("/me reports subject %q, want the caller's", me.Subject)
	}
	// The authority is empty, and that is the true answer rather than a
	// concession: every scoped route in a suspended account is refused, so
	// reporting a permission the caller cannot exercise would make the console
	// render controls that all fail.
	if len(me.Permissions) != 0 || len(me.Roles) != 0 {
		t.Fatalf("/me reports roles %v and permissions %v in a suspended account; there is no authority to hold in one",
			me.Roles, me.Permissions)
	}

	// And it is still an error rather than an empty answer when something is
	// genuinely wrong with the selection, so the tolerance above is narrow.
	if rec := owner.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", ""); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("a scoped route in a suspended account: %d, want 402 — /me is the exception, not the rule", rec.Code)
	}
}

// TestIntegrationSignMeOutEverywhereWorksFromThePicker is the route that has to
// work in the worst state a person can be in.
//
// Session revocation is registered outside the account-scoped group on purpose:
// a session belongs to a PERSON, not to an account. It nevertheless went through
// the mutation helper that resolves the caller's account, so it answered 409
// no_account_selected from the picker — which since task 2.2 is where every
// sign-in begins — and 402 from a suspended account. "My laptop was stolen" is
// exactly the moment somebody is sitting at the picker.
func TestIntegrationSignMeOutEverywhereWorksFromThePicker(t *testing.T) {
	v := newAdminEnv(t)

	withNoSelection := v.session("stolen-laptop", roleAdmin)
	clearSelection(t, v, withNoSelection)
	rec := withNoSelection.do(http.MethodDelete, "/api/v1/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sign out everywhere with no account selected: %d %s, want 200", rec.Code, rec.Body.String())
	}

	inSuspended := v.session("also-stolen", roleAdmin)
	setBalance(t, v.db, v.sc.AccountID(), 0)
	rec = inSuspended.do(http.MethodDelete, "/api/v1/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sign out everywhere from a suspended account: %d %s, want 200 — "+
			"an unpaid balance is not a reason to keep somebody signed in", rec.Code, rec.Body.String())
	}

	// The entry belongs to no account, rather than being filed into whichever
	// one happened to be selected: the revocation did not happen inside a tenant.
	var entries []model.AuditEntry
	err := store.CrossAccount(t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Where("action = ?", authz.ActionSessionRevoke).Find(&entries).Error
	})
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("a session revocation left no audit entry")
	}
	for _, e := range entries {
		if e.AccountID != nil {
			t.Fatalf("a session revocation was filed into account %s; a session belongs to a person", *e.AccountID)
		}
	}
}

// TestIntegrationTheAccountReadsRequireAGrant covers the reads that lost their
// only authorization when the route middleware went away (design C10).
//
// None of them goes through internal/inventory, so the source-reading test in
// that package cannot see them; all of them were gated by
// authz.Guard.Authenticated refusing anybody who landed in none of the four
// configured groups. That door is gone, and until now nothing per-route replaced
// it — so a member holding a role with no grants at all was refused GET
// /projects and served the account's entire audit trail.
func TestIntegrationTheAccountReadsRequireAGrant(t *testing.T) {
	v := newAdminEnv(t)
	nobody := zeroGrantSession(t, v, "holds-nothing")
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	for _, path := range []string{"/audit", "/challenges", "/config"} {
		rec := nobody.do(http.MethodGet, acct+path, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as a member holding no grant: %d %s, want 403 — "+
				"the same caller is refused GET /projects, and this is the same data one screen away",
				path, rec.Code, rec.Body.String())
		}
	}

	// A caller who does hold the grant still reads them, so the check above is
	// not simply refusing everybody.
	reader := v.session("reads-things", roleRead)
	for _, path := range []string{"/audit", "/challenges", "/config"} {
		if rec := reader.do(http.MethodGet, acct+path, ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s as READ_ONLY: %d %s, want 200", path, rec.Code, rec.Body.String())
		}
	}
}

// TestIntegrationConfigIsAccountScoped is the second half of the same hole, and
// the worse half: GET /accounts/:accountId/config never resolved a scope at all,
// so any authenticated session could read the deployment's effective
// configuration through any account identifier — one it had not selected, one it
// was not a member of, one that does not exist.
func TestIntegrationConfigIsAccountScoped(t *testing.T) {
	v := newAdminEnv(t)
	reader := v.session("reads-things", roleRead)

	stranger := model.NewID().String()
	rec := reader.do(http.MethodGet, "/api/v1/accounts/"+stranger+"/config", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET config through an account this session did not select: %d %s, want 404",
			rec.Code, rec.Body.String())
	}

	clearSelection(t, v, reader)
	rec = reader.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/config", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("GET config with no account selected: %d %s, want 409 no_account_selected",
			rec.Code, rec.Body.String())
	}
}

// TestIntegrationCheckoutIsNotAnExistenceOracle is the one construction the
// tenancy invariant forbids by name, and its visible consequence.
//
// Checkout takes its account from a path parameter — it has to, since it sits
// outside the group where a selection would have established one (design C4,
// C5). Reading the account and comparing the owner afterwards answered 403 for
// an account that exists and 404 for one that does not, which is enough for
// anybody with a session to confirm an identifier by guessing it.
func TestIntegrationCheckoutIsNotAnExistenceOracle(t *testing.T) {
	v := newAdminEnv(t)
	notTheOwner := v.session("admin", roleAdmin)

	real := notTheOwner.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/checkout", `{"amount":25}`)
	imaginary := notTheOwner.do(http.MethodPost, "/api/v1/accounts/"+model.NewID().String()+"/checkout", `{"amount":25}`)

	if real.Code != http.StatusNotFound || imaginary.Code != http.StatusNotFound {
		t.Fatalf("checkout answers %d for an account that exists and %d for one that does not; "+
			"the two must be indistinguishable", real.Code, imaginary.Code)
	}

	// And nothing was credited by the attempt.
	acc, err := store.GetAccount(t.Context(), v.db, v.sc)
	if err != nil {
		t.Fatalf("read account: %v", err)
	}
	if acc.Credits != 25 {
		t.Fatalf("a refused checkout moved the balance to %d", acc.Credits)
	}
}

// TestIntegrationNodeRoutesReadAScopedRow is the scopeless read of a policied
// table that this suite structurally cannot catch.
//
// `lb_nodes` carries a row-level security policy, and node() filtered in Go
// while running as the first statement of a mutation's transaction — where
// app.account_id has never been set, so the policy matched nothing and PATCH
// /nodes/:id answered 404 for a perfectly good node under any non-superuser
// role. The test connection is a superuser and bypasses policies entirely, so
// this asserts the SHAPE that made the bug possible is gone rather than the
// symptom: the route works, which is all this connection can see, and the fix is
// Scope.Run in the helper.
func TestIntegrationNodeRoutesReadAScopedRow(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	rec := cl.do(http.MethodGet, acct+"/loadbalancers/"+v.lb.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("read the load balancer: %d %s", rec.Code, rec.Body.String())
	}
	var nodes []model.LBNode
	err := store.CrossAccount(t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Where("load_balancer_id = ?", v.lb.ID).Find(&nodes).Error
	})
	if err != nil || len(nodes) == 0 {
		t.Fatalf("read the fixture's nodes: %v (%d found)", err, len(nodes))
	}

	rec = cl.do(http.MethodPatch, acct+"/nodes/"+nodes[0].ID.String(), `{"ipv4":"203.0.113.99"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH a node: %d %s, want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "203.0.113.99") {
		t.Fatalf("the node was not updated: %s", rec.Body.String())
	}
}
