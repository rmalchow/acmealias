//go:build acceptance

package acceptance

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The team account, end to end (task 7.4).

type roleRef struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Unrestricted bool   `json:"unrestricted"`
}

type auditRow struct {
	Action      string `json:"action"`
	ObjectLabel string `json:"objectLabel"`
}

// TestTheTeamAccountArc walks the other half of the model: an account that is
// created by paying, holds other people, and works only while it has credit.
//
// The whole arc in one test, because it is one arc and the steps only mean
// anything in order: buy, build, invite, run out, be refused — in the console
// AND at the challenge API, which are different refusals in different planes —
// pay again, and work again. Splitting it would mean either five tests sharing
// hidden state or five tests each re-buying an account.
//
// The two REFUSALS in the middle are what this exists to pin. Design C4 reverses
// `metering-and-credits`' overdraft: a lapsed team account does not half-work,
// it stops. And it stops with 402 in both planes, never 401 or 403 — the caller's
// authority is fine and their API key is fine, and a status that sends an
// operator to look at either wastes the days that the certificate on the wire is
// still buying them (design C7).
func TestTheTeamAccountArc(t *testing.T) {
	owner := mustSignIn(t, adminUser, devPassword)
	// The person to be invited signs in FIRST, and not for convenience: a member
	// is named by identity-provider subject, and authentik's provider hashes it
	// (sub_mode: hashed_user_id), so nobody knows what to write down until that
	// person has signed in once. It is the same fact that makes
	// development/scripts/seed.sh own its account with a stand-in.
	member := mustSignIn(t, configAdminUser, devPassword)

	// --- 1. buying one, which is the only way one comes into existence -------
	slug := unique("acc-team")
	var account accountView
	status, raw, err := owner.do(http.MethodPost, "/api/v1/accounts",
		map[string]any{"slug": slug, "name": "Acceptance (team)", "amount": 25})
	if err != nil {
		t.Fatalf("creating a team account: %v", err)
	}
	if status == http.StatusForbidden {
		t.Fatalf("POST /api/v1/accounts was refused: %s\n"+
			"this deployment has account creation closed; the dev stack sets "+
			"ACME_ALLOW_ACCOUNT_CREATION=true in development/.env, which is what makes "+
			"the console's Add account and Upgrade reachable at all", raw)
	}
	if status != http.StatusCreated {
		t.Fatalf("creating a team account: got %d, want 201: %s", status, raw)
	}
	if err := json.Unmarshal(raw, &account); err != nil {
		t.Fatalf("decoding the created account: %v", err)
	}
	switch {
	case account.Kind != "team":
		t.Fatalf("the created account has kind %q, want team", account.Kind)
	case account.Credits != 25:
		t.Fatalf("the created account holds %d credits, want the 25 that was paid — "+
			"creating and funding are one transaction (task 5.4)", account.Credits)
	case account.Suspended:
		t.Fatalf("the account is suspended the moment it is bought (%s); an account that "+
			"exists but was never funded is a state with no purpose", account.SuspendedReason)
	case !account.Owner:
		t.Fatalf("%s bought the account and does not own it", adminUser)
	case !account.Selected:
		t.Fatalf("the account was not selected after being created; somebody who has just " +
			"bought an account meant to work in it")
	}
	// The service selected it as part of creating it, so this session is already
	// acting in it — recorded here rather than re-selected, because re-selecting
	// would hide it if it stopped doing so.
	owner.account = account.ID
	owner.accountBySlug(t, slug) // and it is in the picker

	// --- 2. inventory, and a key that can publish for it ---------------------
	var project projectRef
	if err := owner.json(http.MethodPost, owner.acct("/projects"),
		map[string]string{"slug": "team", "name": "Acceptance (team)"},
		http.StatusCreated, &project); err != nil {
		t.Fatalf("creating a project in the new account: %v", err)
	}
	// Under example.com, the zone the fake internet serves, because the domain
	// has to be able to ACTIVATE: a domain whose delegation has never been seen
	// is offline and /present refuses it (domain-activation A1). The name stays
	// unique per run.
	var domain domainRef
	if err := owner.json(http.MethodPost, owner.acct("/projects/"+project.ID+"/domains"),
		map[string]string{"name": slug + ".example.com"}, http.StatusCreated, &domain); err != nil {
		t.Fatalf("creating a domain: %v", err)
	}
	key := mintKey(t, owner, project.ID, "acceptance: team account", nil)
	const value = "acceptance-team-account-value"
	alias := aliasNameFor(domain.AliasLabel)

	publish := func(what string) int {
		return present(t, "/"+what, key.Key.Identifier, key.Secret, domain.ChallengeName, value, nil)
	}
	// The account is funded and nobody has published the delegation yet, so the
	// refusal here is about the DOMAIN and says so with 409 — the same call
	// answers 402 in step 4 for the same key and the same domain, once the money
	// runs out. The two refusals are the whole point of design A4 being a
	// separate status, and this is the only place they are seen side by side.
	if got := publish("present"); got != http.StatusConflict {
		t.Fatalf("publishing for a funded account whose domain has no delegation yet: got %d, "+
			"want 409 (domain-activation A4)", got)
	}
	// The customer's half of the setup, and the check that records it.
	onboardDomain(t, owner, project.ID, domain)

	if got := publish("present"); got != http.StatusOK {
		t.Fatalf("publishing for a funded account: got %d, want 200", got)
	}
	if got := publish("cleanup"); got != http.StatusOK {
		t.Fatalf("withdrawing for a funded account: got %d, want 200", got)
	}

	// --- 3. inviting somebody, which is what a team account is for -----------
	var roles []roleRef
	if err := owner.getJSON(owner.acct("/roles"), &roles); err != nil {
		t.Fatalf("listing the new account's roles: %v", err)
	}
	// The four default roles are seeded into every new team account (task 2.5):
	// the same names the deleted configuration used to grant globally, now rows
	// this account's owner may edit or delete.
	var admin roleRef
	for _, r := range roles {
		if r.Name == "ADMIN" {
			admin = r
		}
	}
	if admin.ID == "" || !admin.Unrestricted {
		t.Fatalf("the new team account was not seeded with an unrestricted ADMIN role; it has %v", roles)
	}
	if err := owner.json(http.MethodPost, owner.acct("/members"),
		map[string]any{"subject": member.subject, "roleId": admin.ID},
		http.StatusNoContent, nil); err != nil {
		t.Fatalf("adding %s to the account: %v", configAdminUser, err)
	}

	// The grammar has one word for the one object, and the word it lost is
	// REFUSED rather than accepted and ignored. Asserted here, through the API
	// an operator actually writes grants with, because that is where a stale
	// runbook or a copied example would put it: a role that silently held an
	// unevaluable grant would read as authority somebody was given, and the
	// first person to find out is the one who cannot do what it says.
	if status, body, err := owner.do(http.MethodPost, owner.acct("/roles"), map[string]any{
		"name":        unique("acceptance-stale-grammar"),
		"description": "a role written against the grammar before the collapse",
		"grants": []map[string]any{
			{"type": "load_balancer", "objectId": project.ID, "action": "inventory.write", "inheritable": true},
		},
	}); err != nil {
		t.Fatalf("writing a grant naming the removed object type: %v", err)
	} else if status != http.StatusBadRequest {
		t.Errorf("a grant naming load_balancer was answered %d, want 400 — there is no such "+
			"object type, and storing one would be authority nothing can evaluate: %s", status, body)
	}

	// The member's picker now offers it — an account they did not create and do
	// not own — and they can act in it.
	memberView := member.accountBySlug(t, slug)
	if memberView.Owner {
		t.Errorf("%s was invited into the account and is reported as its owner", configAdminUser)
	}
	member.selectInto(t, memberView)
	if err := member.getJSON(member.acct("/projects"), &[]projectRef{}); err != nil {
		t.Fatalf("%s listing projects in the account they were invited into: %v", configAdminUser, err)
	}

	// --- 4. the balance reaches zero -----------------------------------------
	//
	// Written directly because nothing spends credit yet; see setCredits.
	setCredits(t, account.ID, 0)

	// The member is stopped, and the account is not hidden from them: they are
	// told what happened, in a sentence that is not "you do not have permission"
	// (design C4, task 6.7). A 403 would send them to ask an administrator for a
	// grant that would not help.
	if status, body, err := member.do(http.MethodGet, member.acct("/projects"), nil); err != nil {
		t.Fatalf("%s reading a suspended account: %v", configAdminUser, err)
	} else if status != http.StatusPaymentRequired {
		t.Errorf("a member reading a suspended account: got %d, want 402: %s", status, body)
	}
	if status, body, err := member.trySelect(memberView); err != nil {
		t.Fatalf("%s selecting a suspended account: %v", configAdminUser, err)
	} else if status != http.StatusPaymentRequired {
		t.Errorf("a member selecting a suspended account: got %d, want 402: %s", status, body)
	} else if strings.Contains(strings.ToLower(string(body)), "permission") {
		t.Errorf("the suspension refusal talks about permission: %s — the account is "+
			"stopped and its owner can restart it, which is a different sentence with a "+
			"different remedy (design C4, C11)", body)
	}

	// And the challenge API, which is the sharp one: it stops renewals rather
	// than a site, and it must be recognisable as being about money.
	if got := publish("present"); got != http.StatusPaymentRequired {
		t.Errorf("/present for a suspended account: got %d, want 402", got)
	}
	if got := publish("cleanup"); got != http.StatusPaymentRequired {
		// /cleanup is refused too, and deliberately: the check sits before the
		// account's state is touched at all, so nothing was published to
		// withdraw (design C7).
		t.Errorf("/cleanup for a suspended account: got %d, want 402", got)
	}
	if vals, _, err := txtValues(serviceDNS, alias); err != nil {
		t.Fatalf("querying %s: %v", alias, err)
	} else if len(vals) != 0 {
		t.Errorf("%s answers %v after a refused publish; a 402 must leave nothing behind", alias, vals)
	}

	// --- 5. the owner can still get in, and sees why --------------------------
	//
	// The exception without which this state is a trap door: every route that
	// could pay for the account is refused to a suspended account, so if its
	// owner could not select it, nobody could ever restore it (design C4).
	suspended := owner.accountBySlug(t, slug)
	if !suspended.Suspended || suspended.SuspendedReason == "" {
		t.Errorf("the picker shows %s as usable (%+v) — a suspended account is listed AND "+
			"shown as suspended, never omitted (task 6.1b)", slug, suspended)
	}
	if status, body, err := owner.trySelect(suspended); err != nil {
		t.Fatalf("the owner selecting their suspended account: %v", err)
	} else if status != http.StatusNoContent {
		t.Fatalf("the owner cannot select their own suspended account: got %d: %s — "+
			"there is then no door back in", status, body)
	}
	if status, _, err := owner.do(http.MethodGet, owner.acct("/projects"), nil); err != nil {
		t.Fatalf("the owner reading a suspended account: %v", err)
	} else if status != http.StatusPaymentRequired {
		t.Errorf("the owner of a suspended account reads its inventory (got %d); what they "+
			"reach is the balance and checkout, and nothing else (design C5)", status)
	}

	// --- 6. paying again, and everything working again ------------------------
	var recharged accountView
	if err := owner.json(http.MethodPost, "/api/v1/accounts/"+account.ID+"/checkout",
		map[string]any{"amount": 25}, http.StatusOK, &recharged); err != nil {
		t.Fatalf("the owner adding credit to their suspended account: %v", err)
	}
	if recharged.Credits != 25 || recharged.Suspended {
		t.Fatalf("after checkout the account holds %d credits and suspended=%v",
			recharged.Credits, recharged.Suspended)
	}

	// Without a new session on either side: the member's session is the one it
	// was before the balance moved, and the key is the key it was. The funding
	// answer is a property of the account read per request, which is what task
	// 4.6 asks for in both directions.
	if err := member.getJSON(member.acct("/projects"), &[]projectRef{}); err != nil {
		t.Errorf("%s is still refused after the account was recharged: %v", configAdminUser, err)
	}
	if got := publish("present"); got != http.StatusOK {
		t.Errorf("/present after the account was recharged: got %d, want 200", got)
	}
	if got := publish("cleanup"); got != http.StatusOK {
		t.Errorf("/cleanup after the account was recharged: got %d, want 200", got)
	}

	// --- 7. and it was all written down --------------------------------------
	//
	// The one property the stand-in checkout has to keep is that it is a REAL
	// charge: the same row, the same balance change and the same audit entry a
	// payment processor will produce (design C8). The entry is what has to be
	// right on the day money is involved, so it is asserted now, while none is.
	var trail []auditRow
	if err := owner.getJSON(owner.acct("/audit?limit=200"), &trail); err != nil {
		t.Fatalf("reading the account's audit trail: %v", err)
	}
	var checkouts int
	for _, e := range trail {
		if e.Action == "account.checkout" {
			checkouts++
		}
	}
	// Two: the credit that created the account, and the one that restored it.
	if checkouts != 2 {
		t.Errorf("the trail holds %d account.checkout entries, want 2 — one for the purchase "+
			"that created the account and one for the recharge (tasks 5.2, 5.3)", checkouts)
	}

	// --- 8. the half of certificate observation a team account is sold --------
	//
	// The other side of the pair in personal_test.go (domain-diagnostics 12.10,
	// G15). The state is served to both kinds of account; the HISTORY is served
	// here and refused there, with 418 and a sentence about account kinds.
	//
	// It runs last because it restarts the api to force an observation pass, and
	// because the account has to be funded for any of it to be readable — which
	// it is again, two steps up.
	state := observeDomain(t, owner, project.ID, domain.ID)
	if len(state.Addresses) == 0 {
		t.Fatalf("nothing was observed for %s in a team account", domain.Name)
	}
	history := historyRows(t, owner, domain.ID)
	if len(history) == 0 {
		t.Fatalf("the observation history of %s is empty in a team account, while its state "+
			"carries %d address(es) — the two read the same rows", domain.Name, len(state.Addresses))
	}
	// THE SAME OBJECTS, and that is the assertion rather than a formality: the
	// console renders one dialog from both reads, so a history row that dropped
	// a field or keyed an address differently would render as a blank line
	// underneath a populated one.
	seen := map[string]bool{}
	for _, h := range history {
		seen[h.key()] = true
	}
	for _, a := range state.Addresses {
		if !seen[a.key()] {
			t.Errorf("the state reports %s/%s on node %s and the history has no row for it: %+v",
				a.Address, a.Family, a.NodeLabel, history)
		}
	}
	if h := history[0]; h.Outcome == "" || h.Band == "" || h.ObservedAt.IsZero() {
		t.Errorf("a history row is missing the fields that make it readable: %+v — outcome, "+
			"band and observedAt are what a row IS", h)
	}
}
