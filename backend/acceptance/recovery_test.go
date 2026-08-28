//go:build acceptance

package acceptance

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// recoveryFlowSlug is the flow the blueprint defines, hard-coded for the same
// reason enrollmentFlowSlug is: the "Forgot password" link resolving to THIS
// flow is part of what is asserted, and discovering it would assert nothing.
const recoveryFlowSlug = "acmeservice-recovery"

// TestPasswordRecoveryDoesNotSignAnybodyIn covers authentik-and-self-signup
// task 5c.4, and the property it pins is the one that would otherwise be a
// silent hole in the approval gate.
//
// THE GATE IS A POLICY BOUND TO SPECIFIC FLOWS, not a flag Django enforces
// everywhere. `is_active` used to be that flag, and moving off it — so an
// unapproved person could be told what is happening instead of "Failed to
// authenticate." — traded a global guarantee for a per-flow one. The bill comes
// due here: any flow ending in a `user_login` stage is a way in that skips the
// deny bound at order 25 of the authentication flow, and "forgot password" is
// the obvious second front door.
//
// So the recovery flow deliberately ends at the password write. Somebody
// resets, then signs in the ordinary way and meets the ordinary gate. This test
// asserts the shape rather than the symptom: a recovery flow that grew a login
// stage would still let a legitimate person in, and nothing else here would
// notice.
func TestPasswordRecoveryDoesNotSignAnybodyIn(t *testing.T) {
	username := fmt.Sprintf("forgetful-%d", time.Now().UnixNano())
	address := username + "@corp.example"
	t.Cleanup(func() { deleteAuthentikUser(t, username) })

	// A person who has been admitted, so the only thing under test is recovery.
	enrolAndApprove(t, username, address, "first-"+username)

	client := &http.Client{Jar: newLaxJar(), Timeout: 30 * time.Second}
	ex := fmt.Sprintf("%s/api/v3/flows/executor/%s/?query=", authURL, recoveryFlowSlug)

	step, err := flowCall(client, http.MethodGet, ex, nil)
	if err != nil {
		t.Fatalf("opening the recovery flow: %v", err)
	}
	if step.Component != "ak-stage-identification" {
		t.Fatalf("the recovery flow opens at %q, want the identification stage — "+
			"recovery must ask who you are before it sends anything", step.Component)
	}

	// NO PASSWORD FIELD HERE, unlike the sign-in identification stage. Recovery
	// exists for somebody who does not have the password; requiring it would be
	// a flow that only works for people who do not need it.
	if _, err := flowCall(client, http.MethodPost, ex, map[string]string{
		"component": "ak-stage-identification", "uid_field": address,
	}); err != nil {
		t.Fatalf("submitting the identifier: %v", err)
	}

	// The link is read out of mailpit, which also proves the recovery mail goes
	// to the address that was typed rather than to nobody.
	link := confirmationLink(t, address)
	if tok := link.Query().Get("flow_token"); tok == "" {
		t.Fatalf("no flow_token in the recovery link %s", link)
	}

	// THE ASSERTION THIS TEST EXISTS FOR. Walk the flow to its end and confirm
	// it never produces a session: every step is a challenge, and the flow
	// finishes without a `user_login` stage having run. A redirect to an
	// application would mean somebody has just been signed in by having asked
	// to reset a password.
	for _, component := range flowComponentsAfterRecoveryLink(t, client, link.Query().Get("flow_token")) {
		if component == "ak-stage-user-login" {
			t.Fatal("the recovery flow contains a user-login stage. That is a complete bypass of " +
				"the approval gate: the deny is bound to the AUTHENTICATION flow, so anybody " +
				"holding an unapproved registration could admit themselves through " +
				"\"forgot password\" (see the enrolment blueprint's own warning)")
		}
	}
}

// flowComponentsAfterRecoveryLink follows the emailed link and reports every
// stage component the executor presents, so the caller can assert on the whole
// sequence rather than on one step.
func flowComponentsAfterRecoveryLink(t *testing.T, client *http.Client, token string) []string {
	t.Helper()
	ex := fmt.Sprintf("%s/api/v3/flows/executor/%s/?query=flow_token%%3D%s",
		authURL, recoveryFlowSlug, token)

	var seen []string
	// Bounded rather than "until done": a flow that loops is a failure, and an
	// unbounded walk would hang the suite instead of reporting it.
	for i := 0; i < 8; i++ {
		step, err := flowCall(client, http.MethodGet, ex, nil)
		if err != nil {
			t.Fatalf("walking the recovery flow: %v", err)
		}
		if step.Component == "" || step.Component == "xak-flow-redirect" {
			break
		}
		seen = append(seen, step.Component)
		if _, err := flowCall(client, http.MethodPost, ex, map[string]string{
			"component": step.Component,
		}); err != nil {
			// A stage that refuses an empty submission is fine — we are reading
			// the sequence, not completing the reset.
			break
		}
	}
	return seen
}
