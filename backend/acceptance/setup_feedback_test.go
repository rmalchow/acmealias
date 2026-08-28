//go:build acceptance

package acceptance

import (
	"net/http"
	"strings"
	"testing"
)

// What the setup screen tells a customer about their own DNS
// (domain-setup-feedback task 3.4).
//
// The unit tests cover the checker against a resolver they control, and the
// arc tests use the check as a MEANS — onboardDomain waits for "correct"
// because a domain has to activate before anything else can happen. Neither
// asserts the thing the feature exists for: that a customer who has not
// published their delegation is told so, distinguishably, and is given the
// exact record to publish.
//
// That distinction is the whole feature. "Not checked", "checked and absent"
// and "checked and correct" are three different sentences, and a screen that
// collapses any two of them sends somebody to look at the wrong thing —
// which is the failure domain-setup-feedback was written to prevent. Only an
// end-to-end test can tell them apart, because only here is the resolver real
// and the answer genuinely absent rather than mocked as absent.
//
// Both halves are asserted on ONE load balancer holding TWO domains, which is
// deliberate: it is also the join domainChecks does by challenge name, and a
// regression there reads as every domain but the first reporting somebody
// else's record as missing.
func TestTheSetupScreenSaysWhetherTheDelegationIsPublished(t *testing.T) {
	s := mustSignIn(t, readOnlyUser, devPassword)
	personal := s.personalAccount(t)
	s.selectInto(t, personal)

	slug := unique("acc-feedback")
	var project projectRef
	if err := s.json(http.MethodPost, s.acct("/projects"),
		map[string]string{"slug": slug, "name": "Acceptance (setup feedback)"},
		http.StatusCreated, &project); err != nil {
		t.Fatalf("creating a project: %v", err)
	}
	t.Cleanup(func() { _, _, _ = s.do(http.MethodDelete, s.acct("/projects/"+project.ID), nil) })

	var lb lbRef
	if err := s.json(http.MethodPost, s.acct("/projects/"+project.ID+"/loadbalancers"),
		map[string]string{"slug": "prod", "name": "Acceptance (setup feedback)"},
		http.StatusCreated, &lb); err != nil {
		t.Fatalf("creating a load balancer: %v", err)
	}

	// Both under example.com, the only zone the fake internet answers for. The
	// difference between them is not the name — it is that one of them has a
	// CNAME published and the other does not.
	published := newDomain(t, s, lb.ID, "published-"+slug+".example.com")
	absent := newDomain(t, s, lb.ID, "absent-"+slug+".example.com")

	// Before anything is published, BOTH read as absent. This is the assertion
	// that a plain "not correct" would have passed while meaning nothing: the
	// check ran, the answer came back, and the record is not there.
	checks := verification(t, s, lb.ID)
	for _, d := range []domainRef{published, absent} {
		got := checks[strings.TrimSuffix(d.Name, ".")]
		if got.Delegation.Status != "missing" {
			t.Errorf("before publishing, %s reads %q, want \"missing\" — the check ran against a "+
				"real resolver and the record genuinely is not there, which is a different "+
				"sentence from \"not checked\"", d.Name, got.Delegation.Status)
		}
		// The record to publish, carried whatever the status. Help that says a
		// record is missing without saying which record is not help
		// (domain-setup-feedback D5).
		want := aliasNameFor(d.AliasLabel)
		if len(got.Delegation.Expected) == 0 || !hasSuffixFold(got.Delegation.Expected[0], want) {
			t.Errorf("%s reports its delegation missing and offers expected=%v, which does not "+
				"name %s — the customer cannot fix a record we decline to state",
				d.Name, got.Delegation.Expected, want)
		}
	}

	// Publish exactly one of them, the way a customer would.
	onboardDomain(t, s, lb.ID, published)

	checks = verification(t, s, lb.ID)
	if got := checks[strings.TrimSuffix(published.Name, ".")]; got.Delegation.Status != "correct" {
		t.Errorf("after publishing, %s reads %q (found %q), want \"correct\"",
			published.Name, got.Delegation.Status, got.Delegation.Found)
	}
	// And the other one is untouched. The two domains sit on one load balancer
	// and are answered from one pass over DNS, so this is where a join on the
	// wrong key would show: a delegation belonging to one domain reported as
	// covering the other.
	if got := checks[strings.TrimSuffix(absent.Name, ".")]; got.Delegation.Status != "missing" {
		t.Errorf("after publishing %s, the unpublished %s reads %q (found %q), want \"missing\" — "+
			"one domain's delegation must not answer for another's",
			published.Name, absent.Name, got.Delegation.Status, got.Delegation.Found)
	}
}

// domainCheck is the per-domain half of the verification answer
// (internal/httpapi/admin_plan.go, domainChecks).
type acceptanceDomainCheck struct {
	Name       string `json:"name"`
	Delegation struct {
		Status   string   `json:"status"`
		Expected []string `json:"expected"`
		Found    string   `json:"found"`
	} `json:"delegation"`
}

// verification reads the load balancer's setup screen, keyed by domain name.
//
// It uses the GET, not the POST beside it: the GET is what the screen runs on
// open and it RECORDS NOTHING, which is what this test wants — asserting what a
// customer is told, without activating anything as a side effect. onboardDomain
// uses the POST precisely because it does want the side effect.
func verification(t *testing.T, s *session, lb string) map[string]acceptanceDomainCheck {
	t.Helper()
	var res struct {
		Resolver string                  `json:"resolver"`
		Domains  []acceptanceDomainCheck `json:"domains"`
	}
	if err := s.json(http.MethodGet, s.acct("/loadbalancers/"+lb+"/verification"),
		nil, http.StatusOK, &res); err != nil {
		t.Fatalf("reading the verification for %s: %v", lb, err)
	}
	if res.Resolver == "" {
		// Every domain would read "not checked", and the test would be
		// asserting the deployment's silence rather than the feature
		// (domain-setup-feedback D4).
		t.Fatalf("the service has no verification resolver configured (ACME_VERIFY_RESOLVER, " +
			"development/.env), so nothing is checked and this test can prove nothing")
	}
	out := make(map[string]acceptanceDomainCheck, len(res.Domains))
	for _, d := range res.Domains {
		out[strings.TrimSuffix(d.Name, ".")] = d
	}
	return out
}

func newDomain(t *testing.T, s *session, lb, name string) domainRef {
	t.Helper()
	var d domainRef
	if err := s.json(http.MethodPost, s.acct("/loadbalancers/"+lb+"/domains"),
		map[string]string{"name": name}, http.StatusCreated, &d); err != nil {
		t.Fatalf("creating the domain %s: %v", name, err)
	}
	return d
}

func hasSuffixFold(s, suffix string) bool {
	s = strings.TrimSuffix(strings.ToLower(s), ".")
	suffix = strings.TrimSuffix(strings.ToLower(suffix), ".")
	return strings.HasSuffix(s, suffix)
}
