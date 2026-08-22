package httpapi

import (
	"net/http"
	"testing"
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
	cl := v.session("admin", groupAdmin)

	rec := cl.do(http.MethodPost, "/api/v1/accounts", `{"slug":"sneaky","name":"Sneaky"}`)
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
	cl := v.session("admin", groupAdmin)

	rec := cl.do(http.MethodPost, "/api/v1/accounts", `{"slug":"opened","name":"Opened"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/accounts with creation enabled: %d %s, want 201", rec.Code, rec.Body.String())
	}

	var created accountView
	decode(t, rec, &created)
	if !created.Selected {
		t.Fatal("a newly created account was not selected — someone who just made one meant to act in it")
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
		{"ADMIN", groupAdmin, true, true},
		{"CONFIG_ADMIN", groupConfig, true, false},
		{"READ_ONLY", groupRead, false, false},
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
	cl := v.session("reporter", groupRead)

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
