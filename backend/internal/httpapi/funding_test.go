package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// setBalance writes an account's balance directly.
//
// Straight to the column rather than through checkout, because these tests are
// about what happens AT a balance rather than about how it got there — and
// because "spend it down to zero" is not something the service can do yet: the
// ledger that draws a balance down belongs to `metering-and-credits`.
func setBalance(t *testing.T, db *gorm.DB, accountID model.ID, credits int64) {
	t.Helper()
	err := store.CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).Where("id = ?", accountID).
			UpdateColumn("credits", credits).Error
	})
	if err != nil {
		t.Fatalf("set balance to %d: %v", credits, err)
	}
}

// TestIntegrationTheBoundaryIsExactlyZero is task 4.6 in both directions, and
// through the API rather than through the predicate — TestUsableIsTheWholeRule
// in internal/store already pins the predicate, and this asserts that the gates
// actually consult it.
//
// The third act is the one that matters most: the transition takes effect
// WITHOUT a restart and without a new session. The same client, holding the same
// session cookie, having already selected the account, is served, then refused,
// then served again. If the check were cached — on the session, on the scope, in
// a map somewhere — the last request would fail, and an operator who had just
// been paid would be telling a customer to sign out and back in.
func TestIntegrationTheBoundaryIsExactlyZero(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	projects := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects"

	setBalance(t, v.db, v.sc.AccountID(), 1)
	if rec := cl.do(http.MethodGet, projects, ""); rec.Code != http.StatusOK {
		t.Fatalf("at one credit: %d %s, want 200 — one credit is funded", rec.Code, rec.Body.String())
	}

	setBalance(t, v.db, v.sc.AccountID(), 0)
	rec := cl.do(http.MethodGet, projects, "")
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("at zero credits: %d %s, want 402 — the account is suspended, and it is not the caller's authority that is wrong",
			rec.Code, rec.Body.String())
	}
	var body ErrorBody
	decode(t, rec, &body)
	if !strings.Contains(body.Message, "credit") {
		t.Fatalf("the refusal %q does not say why; C5's point is that a refusal names its remedy", body.Message)
	}

	setBalance(t, v.db, v.sc.AccountID(), 25)
	if rec := cl.do(http.MethodGet, projects, ""); rec.Code != http.StatusOK {
		t.Fatalf("after being credited: %d %s, want 200 — paying must work without a new session",
			rec.Code, rec.Body.String())
	}
}

// TestIntegrationPayingDoesNotLiftAStaffSuspension is design C6: two
// suspensions, and neither lifts the other.
func TestIntegrationPayingDoesNotLiftAStaffSuspension(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	projects := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects"

	err := store.CrossAccount(t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Model(&model.Account{}).Where("id = ?", v.sc.AccountID()).
			UpdateColumn("staff_suspended", true).Error
	})
	if err != nil {
		t.Fatalf("staff-suspend: %v", err)
	}
	setBalance(t, v.db, v.sc.AccountID(), 100)

	rec := cl.do(http.MethodGet, projects, "")
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("a well-funded staff-suspended account: %d %s, want a refusal", rec.Code, rec.Body.String())
	}
	var body ErrorBody
	decode(t, rec, &body)
	if strings.Contains(body.Message, "no credit remaining") {
		t.Fatalf("the refusal %q blames the balance; adding credit will not lift this one", body.Message)
	}
}

// TestIntegrationOnlyTheOwnerReachesASuspendedAccount is design C4's owner
// exception, which is what keeps suspension from being a trap door.
//
// Without it: a suspended account cannot be selected, every route that could pay
// for it is account-scoped, and so nobody — the owner included — can ever
// restore it. With it, and only for the owner, the account is reachable and what
// is reachable inside it is the balance and checkout (C5).
func TestIntegrationOnlyTheOwnerReachesASuspendedAccount(t *testing.T) {
	v := newAdminEnv(t)
	// "fixture-owner" is the subject newAdminEnv gives the account's owner. The
	// member below holds the unrestricted role and is still not the owner, which
	// is the distinction under test: administering an account and owning it are
	// not the same authority.
	owner := v.session("fixture-owner", roleAdmin)
	member := v.session("admin", roleAdmin)
	setBalance(t, v.db, v.sc.AccountID(), 0)

	sel := "/api/v1/accounts/" + v.sc.AccountID().String() + "/select"

	rec := member.do(http.MethodPost, sel, "")
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("member selecting a suspended account: %d %s, want 402", rec.Code, rec.Body.String())
	}
	var body ErrorBody
	decode(t, rec, &body)
	// The two refusals must not share wording. Somebody told they lack
	// permission goes looking for whoever can grant it; what is true is that the
	// account is stopped and its owner can restart it.
	if strings.Contains(strings.ToLower(body.Message), "permission") {
		t.Fatalf("the refusal %q reads as a permission failure; it is a suspension", body.Message)
	}
	if !strings.Contains(body.Message, "owner") {
		t.Fatalf("the refusal %q does not name the remedy or who holds it", body.Message)
	}

	if rec := owner.do(http.MethodPost, sel, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("owner selecting a suspended account: %d %s, want 204 — otherwise nobody can pay for it",
			rec.Code, rec.Body.String())
	}

	// And what the owner reaches inside it is nothing but the way to fix it: the
	// scoped routes are still refused.
	rec = owner.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", "")
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("owner reading inventory in a suspended account: %d %s, want 402 (design C5)",
			rec.Code, rec.Body.String())
	}

	// The account is LISTED for the owner rather than omitted, and shown as
	// suspended: the picker is how they reach it (task 6.1b).
	rec = owner.do(http.MethodGet, "/api/v1/accounts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", rec.Code, rec.Body.String())
	}
	var listed []accountView
	decode(t, rec, &listed)
	var found *accountView
	for i := range listed {
		if listed[i].ID == v.sc.AccountID() {
			found = &listed[i]
		}
	}
	if found == nil {
		t.Fatal("a suspended account was omitted from its owner's picker; they are the only person who can restore it")
	}
	if !found.Suspended || found.SuspendedReason == "" {
		t.Fatalf("the picker shows the account as %+v; it must say it is suspended and why", *found)
	}
	if !found.Owner {
		t.Fatal("the picker does not tell the owner they are the owner, so the console cannot offer them checkout")
	}
}

// TestIntegrationCheckoutRestoresASuspendedAccount is the whole arc of C4, C5
// and C8 in one: the owner of a stopped account pays, and it works again.
func TestIntegrationCheckoutRestoresASuspendedAccount(t *testing.T) {
	v := newAdminEnv(t)
	owner := v.session("fixture-owner", roleAdmin)
	setBalance(t, v.db, v.sc.AccountID(), 0)

	acct := "/api/v1/accounts/" + v.sc.AccountID().String()
	if rec := owner.do(http.MethodGet, acct+"/projects", ""); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("before paying: %d, want 402", rec.Code)
	}

	rec := owner.do(http.MethodPost, acct+"/checkout", `{"amount":50}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkout: %d %s, want 200 — the owner of a suspended account must be able to pay for it",
			rec.Code, rec.Body.String())
	}
	var after accountView
	decode(t, rec, &after)
	if after.Credits != 50 || after.Suspended {
		t.Fatalf("after checkout the account is %+v, want 50 credits and not suspended", after)
	}

	if rec := owner.do(http.MethodGet, acct+"/projects", ""); rec.Code != http.StatusOK {
		t.Fatalf("after paying: %d %s, want 200 — without a new session (spec: 'becomes usable without a new session')",
			rec.Code, rec.Body.String())
	}

	// Auditable and attributable: who, which account, how much. This is the
	// record that has to be right on the day money is involved.
	rec = owner.do(http.MethodGet, acct+"/audit?action=account.checkout", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("read audit: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "account.checkout") {
		t.Fatalf("the checkout left no audit entry: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "fixture-owner") {
		t.Fatalf("the checkout entry does not name who did it: %s", rec.Body.String())
	}
}

// TestIntegrationChallengeIsRefusedWithPaymentRequired is task 4.4 and design
// C7: the challenge API refuses for an unusable account, after the alias is
// resolved and before anything is published.
//
// The status is the assertion. A 401 would send an operator to look at an API
// key that authenticated perfectly, and a 403 to look at grants that permitted
// the action — both are working, and both diagnoses are wrong.
func TestIntegrationChallengeIsRefusedWithPaymentRequired(t *testing.T) {
	v := newEnv(t)
	setBalance(t, v.db, v.alias.AccountID, 0)

	rec := post(v.e, "/present", `{"fqdn":"`+testFQDN+`.","value":"unfunded"}`, v.ours)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("present for a suspended account: %d %s, want 402", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "challenge-fixture") {
		t.Fatalf("the refusal %s does not name the account; it lands in the log of a machine whose owner has to understand it",
			rec.Body.String())
	}

	// Nothing was published. The check sits after alias resolution and before
	// any write precisely so this is true.
	var n int64
	if err := store.CrossAccount(t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Model(&model.Challenge{}).Where("alias_id = ?", v.alias.ID).Count(&n).Error
	}); err != nil {
		t.Fatalf("count challenges: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d challenge values were written by a refused publish", n)
	}

	// Cleanup is refused too, not only present: the check is in resolve, which
	// both go through. A service that refused publication and still accepted
	// withdrawal would be a strange half-service.
	if rec := post(v.e, "/cleanup", `{"fqdn":"`+testFQDN+`.","value":"unfunded"}`, v.ours); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("cleanup for a suspended account: %d %s, want 402", rec.Code, rec.Body.String())
	}

	// And the key is unharmed: credit the account and the same key publishes.
	setBalance(t, v.db, v.alias.AccountID, 25)
	if rec := post(v.e, "/present", `{"fqdn":"`+testFQDN+`.","value":"funded"}`, v.ours); rec.Code != http.StatusOK {
		t.Fatalf("present after crediting: %d %s, want 200 — the key was valid all along",
			rec.Code, rec.Body.String())
	}
}
