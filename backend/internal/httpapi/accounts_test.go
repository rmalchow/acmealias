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
