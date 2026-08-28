//go:build acceptance

package acceptance

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// TestPasswordRecoveryCannotSignAnybodyIn covers authentik-and-self-signup task
// 5c.4, and the property it pins is the one that would otherwise be a silent
// hole in the approval gate.
//
// THE GATE IS A POLICY BOUND TO SPECIFIC FLOWS, not a flag Django enforces
// everywhere. `is_active` used to be that flag, and moving off it — so an
// unapproved person could be told what is happening rather than "Failed to
// authenticate." — traded a global guarantee for a per-flow one. The bill comes
// due here: any flow ending in a `user_login` stage is a way in that skips the
// deny bound at order 25 of the authentication flow, and "forgot password" is
// the obvious second front door. The enrolment blueprint wrote the warning down
// before this flow existed.
//
// SO THIS ASSERTS THE FLOW'S SHAPE, NOT A WALK THROUGH IT. Driving a real reset
// would need a disposable user, a mailbox read and a password change, and would
// still only prove that ONE path does not sign anybody in — while the thing
// that must never be true is that ANY stage of it can. Reading the stage
// bindings answers that directly and cannot pass by luck: a `user_login` stage
// added anywhere in the flow fails this, whether or not a particular walk
// happens to reach it.
func TestPasswordRecoveryCannotSignAnybodyIn(t *testing.T) {
	const flowSlug = "acmeservice-recovery"

	stages := flowStageComponents(t, flowSlug)
	if len(stages) == 0 {
		t.Fatalf("the %s flow has no stages, or does not exist. \"Forgot password\" is "+
			"rendered by the identification stage naming a recovery flow, so an absent "+
			"flow means the link is absent too", flowSlug)
	}

	for order, component := range stages {
		if component == "ak-stage-user-login" {
			t.Errorf("the recovery flow contains a user-login stage at order %d. That is a "+
				"complete bypass of the approval gate: the deny is bound to the "+
				"AUTHENTICATION flow, so anybody holding an unapproved registration could "+
				"admit themselves through \"forgot password\"", order)
		}
	}

	// And the flow must still be a RECOVERY flow — an identification stage to
	// say who you are, and an email stage so that saying so proves something.
	// Without the second, the reset is available to whoever types an address.
	var identification, email bool
	for _, c := range stages {
		switch c {
		case "ak-stage-identification":
			identification = true
		case "ak-stage-email":
			email = true
		}
	}
	if !identification || !email {
		t.Errorf("recovery flow stages are %v; want at least an identification stage and an "+
			"email stage — without the mail, a password reset is available to anybody who "+
			"can type an address", stages)
	}
}

// flowStageComponents returns the component name of each stage bound to a flow,
// keyed by its order, read from the identity provider's own API.
func flowStageComponents(t *testing.T, slug string) map[int]string {
	t.Helper()

	raw := authentikAPI(t, http.MethodGet,
		"/api/v3/flows/instances/?slug="+url.QueryEscape(slug), "")
	var flows struct {
		Results []struct {
			PK string `json:"pk"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &flows); err != nil {
		t.Fatalf("decoding the flow list: %v", err)
	}
	if len(flows.Results) != 1 {
		t.Fatalf("looking up flow %q found %d, expected 1", slug, len(flows.Results))
	}

	raw = authentikAPI(t, http.MethodGet,
		"/api/v3/flows/bindings/?target="+url.QueryEscape(flows.Results[0].PK), "")
	var bindings struct {
		Results []struct {
			Order    int `json:"order"`
			StageObj struct {
				Component string `json:"component"`
			} `json:"stage_obj"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &bindings); err != nil {
		t.Fatalf("decoding the stage bindings: %v", err)
	}

	out := make(map[int]string, len(bindings.Results))
	for _, b := range bindings.Results {
		// The API reports `ak-stage-identification-form`; the flow executor
		// reports `ak-stage-identification`. Trim so the two vocabularies match
		// and a reader comparing this against a browser sees the same names.
		out[b.Order] = trimSuffix(b.StageObj.Component, "-form")
	}
	return out
}

func trimSuffix(s, suffix string) string {
	if len(s) > len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
