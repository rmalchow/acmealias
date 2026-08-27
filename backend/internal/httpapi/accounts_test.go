package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// TestIntegrationAccountCreationIsClosedByDefault covers the half of U9 that
// matters: the refusal is applied by the ENDPOINT, not by the console omitting
// a button.
//
// A route hidden from a menu is still on the internet — discoverable by anyone
// who reads the JavaScript bundle, and working. So the test calls it directly,
// which is what a caller who has read the bundle would do.
func TestIntegrationAccountCreationIsClosedByDefault(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)

	rec := cl.do(http.MethodPost, "/api/v1/accounts", `{"slug":"sneaky","name":"Sneaky","amount":25}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/v1/accounts: %d %s, want 403 — account creation is closed unless a deployment opens it",
			rec.Code, rec.Body.String())
	}

	// And an account was not created as a side effect of being refused.
	accounts, err := listAccountsFor(t, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		if a.Slug == "sneaky" {
			t.Fatal("the refused request created an account anyway")
		}
	}
}

// TestIntegrationAccountCreationWorksWhenOpened is the other half: the switch is
// configuration, so opening it needs no code change and no redeploy of anything
// but the setting.
func TestIntegrationAccountCreationWorksWhenOpened(t *testing.T) {
	v := newAdminEnv(t)
	v.openAccountCreation(t)
	cl := v.session("admin", roleAdmin)

	rec := cl.do(http.MethodPost, "/api/v1/accounts", `{"slug":"opened","name":"Opened","amount":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/accounts with creation enabled: %d %s, want 201", rec.Code, rec.Body.String())
	}

	var created accountView
	decode(t, rec, &created)
	if !created.Selected {
		t.Fatal("a newly created account was not selected — someone who just made one meant to act in it")
	}
	// Created AND funded, in one act (design C8, task 5.4). An account that
	// exists but was never funded is a state with no purpose, so the creation
	// carrying the balance is the assertion rather than a detail of it.
	if created.Credits != 50 {
		t.Fatalf("a newly bought account holds %d credits, want 50 — creating and buying are one act", created.Credits)
	}
	if created.Suspended {
		t.Fatal("a newly bought account is suspended; it was funded in the same transaction that made it")
	}
	if created.Kind != model.AccountKindTeam {
		t.Fatalf("a bought account is %q, want team — a personal account is materialized, never requested", created.Kind)
	}
}

// TestIntegrationCheckoutRefusesAnAmountNobodyOffered is design C8: the amount
// is a fixed choice, not a number the caller supplies.
//
// There is no payment processor in the way to notice a body saying 1000000
// instead of 100, so the only thing between the balance and an arbitrary number
// is that no arbitrary number is accepted. The test calls the endpoint directly
// with a number no screen offers, which is what somebody who read the bundle
// would do.
func TestIntegrationCheckoutRefusesAnAmountNobodyOffered(t *testing.T) {
	v := newAdminEnv(t)
	v.openAccountCreation(t)
	cl := v.session("admin", roleAdmin)

	for _, amount := range []string{"1000000", "0", "-25", "26"} {
		rec := cl.do(http.MethodPost, "/api/v1/accounts", `{"slug":"greedy","name":"Greedy","amount":`+amount+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST /api/v1/accounts with amount %s: %d %s, want 400", amount, rec.Code, rec.Body.String())
		}
	}
	accounts, err := listAccountsFor(t, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		if a.Slug == "greedy" {
			t.Fatal("a refused amount created an account anyway")
		}
	}
}

// TestIntegrationPermittedActionsAccompanyEveryRow is task 7.3: the console has
// to render controls from a per-object answer, not from the caller's role name.
//
// The role name said nothing about WHICH objects, which is the thing this whole
// change exists to replace — so a report that merely restated the role would be
// no better than what it replaced.
func TestIntegrationPermittedActionsAccompanyEveryRow(t *testing.T) {
	v := newAdminEnv(t)

	for _, tc := range []struct {
		role, group string
		wantWrite   bool
		wantAdmin   bool
	}{
		{"ADMIN", roleAdmin, true, true},
		{"CONFIG_ADMIN", roleConfig, true, false},
		{"READ_ONLY", roleRead, false, false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			cl := v.session("perm-"+tc.role, tc.group)
			rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("list projects: %d %s", rec.Code, rec.Body.String())
			}
			var projects []projectView
			decode(t, rec, &projects)
			if len(projects) == 0 {
				t.Fatal("no projects listed")
			}
			// The answer rides along with the row rather than costing a request
			// per row, which is what keeps a table of fifty projects one query.
			actions := projects[0].PermittedActions
			if !contains(actions, "inventory.view") {
				t.Fatalf("%s: permittedActions = %v, want it to include reading", tc.role, actions)
			}
			if got := contains(actions, "inventory.write"); got != tc.wantWrite {
				t.Errorf("%s: may write = %v, want %v (actions: %v)", tc.role, got, tc.wantWrite, actions)
			}
			// project.admin is governed by MEMBERSHIP of the unrestricted role
			// (design P14), so the report must answer for it the way the handler
			// does — otherwise a role holding a wildcard grant is told it may
			// create projects and then refused when it tries.
			if got := contains(actions, "project.admin"); got != tc.wantAdmin {
				t.Errorf("%s: may administer projects = %v, want %v (actions: %v)", tc.role, got, tc.wantAdmin, actions)
			}
		})
	}
}

// The report is not the enforcement point: a request for something it did not
// offer is still refused when it arrives.
func TestIntegrationTheReportDoesNotAuthorize(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("reporter", roleRead)

	rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", "")
	var projects []projectView
	decode(t, rec, &projects)
	if len(projects) == 0 {
		t.Fatal("no projects listed")
	}
	if contains(projects[0].PermittedActions, "inventory.write") {
		t.Fatal("setup: READ_ONLY was offered write")
	}

	// Ask for it anyway.
	path := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + projects[0].ID.String()
	if rec := cl.do(http.MethodPatch, path, `{"name":"Renamed"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH as READ_ONLY: %d %s, want 403", rec.Code, rec.Body.String())
	}
}

// TestIntegrationARefusedEscalationWritesNoEntry is task 8.4.
//
// A refused write must leave no change entry, because an audit trail that
// records attempts as though they were changes tells an operator that something
// happened when nothing did — and the one question the trail exists to answer is
// "what happened".
//
// The refusal is still visible: it is logged, and the request is answered with a
// message naming the grant that was not covered. What it is not is a row in the
// list of changes.
func TestIntegrationARefusedEscalationWritesNoEntry(t *testing.T) {
	v := newAdminEnv(t)
	// KEY_ROTATION may mint keys but holds no inventory write, so a role granting
	// one is an escalation it cannot issue.
	cl := v.session("escalator", roleKey)
	auditAt := "/api/v1/accounts/" + v.sc.AccountID().String() + "/audit"

	before := entriesAt(t, cl, auditAt)

	body := `{"name":"too-much","grants":[{"type":"*","action":"inventory.write","inheritable":true}]}`
	rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/roles", body)
	if rec.Code == http.StatusCreated {
		t.Fatal("a role conferring more than the writer holds was created")
	}

	after := entriesAt(t, cl, auditAt)
	if len(after) != len(before) {
		t.Fatalf("the refused request wrote %d audit entries, want none", len(after)-len(before))
	}
}

func entriesAt(t *testing.T, cl *client, auditAt string) []auditView {
	t.Helper()
	var es []auditView
	decode(t, cl.do(http.MethodGet, auditAt+"?limit=1000", ""), &es)
	return es
}

// TestIntegrationGrantsAreRecordedInFull is task 8.2: an entry has to say what
// authority was conferred, in full, or it cannot answer the question it exists
// for. A reference to a grant row would go stale the moment the role was edited.
func TestIntegrationGrantsAreRecordedInFull(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("roler", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	body := `{"name":"readers","grants":[{"type":"*","action":"inventory.view","inheritable":true}]}`
	if rec := cl.do(http.MethodPost, acct+"/roles", body); rec.Code != http.StatusCreated {
		t.Fatalf("create role: %d %s", rec.Code, rec.Body.String())
	}

	var es []auditView
	decode(t, cl.do(http.MethodGet, acct+"/audit?action="+authz.ActionRoleCreate, ""), &es)
	if len(es) == 0 {
		t.Fatal("no entry for the role creation")
	}
	recorded := string(es[0].After)
	for _, want := range []string{"inventory.view", "inheritable", `"type":"*"`} {
		if !strings.Contains(recorded, want) {
			t.Errorf("the entry does not record %s: %s", want, recorded)
		}
	}
}
