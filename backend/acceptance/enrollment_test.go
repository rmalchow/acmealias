//go:build acceptance

package acceptance

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The front door, walked the way a stranger walks it (`self-service-signup`
// group 7).
//
// WHAT THIS TEST IS ACTUALLY FOR, since "registration works" undersells it.
// Until the enrolment flow existed, this product was closed BY OMISSION: the
// blueprint defined one flow, bound no enrolment flow to it, and so the sign-in
// page carried no *Sign up* link. There was no policy to reverse and nothing
// refusing anybody (design S1). What this asserts is the arrangement that
// replaced that accident — a door, with a person standing in front of it — and
// in particular the property that is easy to give away and impossible to get
// back: NO ROW IS WRITTEN IN OUR DATABASE FOR SOMEBODY WE HAVE NOT ADMITTED
// (design S5).
//
// The gate is authentik's, not ours, and that is the decision the test pins.
// Held inactive at the provider, an unapproved person never reaches us — no
// session, no `accounts` row, no slug, no audit entry. If approval lived in our
// service instead, anybody able to fill in a signup form could make us write
// rows. So `store.Usable` has no clause about approval, `accounts` has no
// `approved_at` column, and there is nothing here to query for a pending person
// — which is why the assertion below is about WHEN the account came into
// existence rather than about a flag on it.

// The identity provider's admin token, from development/.env
// (AUTHENTIK_BOOTSTRAP_TOKEN). Approving somebody is setting `is_active` on
// their user, and there is deliberately no mechanism of ours to do it with: the
// decision is about an identity, so it is taken in the provider's own interface
// and recorded in the provider's own event log (design S9).
const authentikAdminToken = "dev-only-authentik-bootstrap-token"

// The enrolment flow's slug, as the blueprint names it. Hard-coded rather than
// discovered, because the *Sign up* link resolving to this exact flow is part of
// what is being asserted.
const enrollmentFlowSlug = "acmeservice-enrollment"

// TestSelfEnrolmentWaitsForApproval walks the whole of it: register, confirm the
// address, be refused, be approved, sign in, and hold exactly one account that
// did not exist a moment ago.
func TestSelfEnrolmentWaitsForApproval(t *testing.T) {
	username := fmt.Sprintf("stranger-%d", time.Now().UnixNano())
	address := username + "@corp.example"
	password := "enrol-" + username
	t.Cleanup(func() { deleteAuthentikUser(t, username) })

	// --- register -----------------------------------------------------------
	//
	// One page: username, name, address, password twice. The flow creates the
	// user INACTIVE on the path users/pending-verification and mails a link.
	client := &http.Client{Jar: newLaxJar(), Timeout: 30 * time.Second}
	step, err := enrolStep(client, "", map[string]string{
		"component":       "ak-stage-prompt",
		"username":        username,
		"name":            "A Stranger",
		"email":           address,
		"password":        password,
		"password_repeat": password,
	})
	if err != nil {
		t.Fatalf("submitting the enrolment form: %v", err)
	}
	if step.Component != "ak-stage-email" {
		t.Fatalf("after the enrolment form the flow is at %q, expected the email "+
			"verification stage — verification runs BEFORE approval so that the "+
			"queue a person reads contains only addresses that exist (design S2)",
			step.Component)
	}

	if got := authentikUser(t, username); got.IsActive {
		t.Fatalf("%s was created ACTIVE; `create_users_as_inactive` on the "+
			"enrolment flow's user_write stage IS the approval gate (design S3)", username)
	}

	// --- confirm the address ------------------------------------------------
	//
	// The link is read out of mailpit, which is the only way to prove the
	// message was actually sent to the address that was typed rather than to
	// nobody.
	link := confirmationLink(t, address)
	confirm := &http.Client{Jar: newLaxJar(), Timeout: 30 * time.Second}
	tok := link.Query().Get("flow_token")
	if tok == "" {
		t.Fatalf("no flow_token in the confirmation link %s", link)
	}
	step, err = enrolStep(confirm, "flow_token="+tok, nil)
	if err != nil {
		t.Fatalf("following the confirmation link: %v", err)
	}
	if step.Component != "ak-stage-prompt" {
		t.Fatalf("after confirming the address the flow is at %q, expected the "+
			"notice telling the person their registration is under review — the "+
			"one part of this design that must not be skipped (design S3)", step.Component)
	}
	// Finishing the flow moves them into the reviewer's queue. The path is the
	// queue: authentik renders paths as a tree, so users/pending-approval is a
	// folder containing exactly the people whose address is proved and whose
	// admission is not.
	if _, err := enrolStep(confirm, "", map[string]string{"component": "ak-stage-prompt"}); err != nil {
		t.Fatalf("finishing enrolment: %v", err)
	}
	user := authentikUser(t, username)
	if user.IsActive {
		t.Fatalf("confirming the address ACTIVATED %s. `is_active` here means "+
			"\"we have admitted this person\", not \"they proved their address\" — "+
			"the email stage's activate_user_on_success must stay false, or the "+
			"approval gate is opened by the mail that was only meant to prove an "+
			"address exists", username)
	}
	if user.Path != "users/pending-approval" {
		t.Fatalf("%s is on path %q after confirming, expected users/pending-approval — "+
			"the reviewer's queue is the path, and a person who never clicked the "+
			"link must not be in it (design S9)", username, user.Path)
	}

	// --- refused, and nothing written for them ------------------------------
	if _, err := signIn(username, password); err == nil {
		t.Fatalf("%s signed in while unapproved; the whole of the gate is that "+
			"they cannot (design S3)", username)
	}

	// --- approved -----------------------------------------------------------
	approvedAt := time.Now().UTC()
	approveAuthentikUser(t, user.PK)

	s, err := signIn(username, password)
	if err != nil {
		t.Fatalf("%s could not sign in after being approved: %v", username, err)
	}

	var accounts []struct {
		Name      string    `json:"name"`
		Kind      string    `json:"kind"`
		Owner     bool      `json:"owner"`
		CreatedAt time.Time `json:"createdAt"`
	}
	if err := s.getJSON("/api/v1/accounts", &accounts); err != nil {
		t.Fatalf("GET /api/v1/accounts as %s: %v", username, err)
	}
	// P18, demonstrated rather than assumed: a self-enrolled person holds their
	// own personal account and membership of NOTHING else. No group at the
	// identity provider gives them one — the enrolment flow places them in no
	// group at all, and nothing has been derived from groups since C10.
	if len(accounts) != 1 || accounts[0].Kind != "personal" || !accounts[0].Owner {
		t.Fatalf("%s holds %+v, expected exactly one personal account they own "+
			"(`tenancy-and-permissions` P18)", username, accounts)
	}
	// AND THIS IS THE PROPERTY WORTH THE WHOLE DESIGN. The account was
	// materialized by `store.EnsurePersonalAccount` on the FIRST SIGN-IN, which
	// is after the approval — so it did not exist while they were waiting. There
	// is nothing to query for a pending person because there is nothing there,
	// which is what makes "every row in `accounts` belongs to somebody we
	// admitted" true by construction rather than by a predicate somebody has to
	// remember to consult (design S5).
	if accounts[0].CreatedAt.Before(approvedAt) {
		t.Fatalf("%s's account was created at %s, BEFORE they were approved at %s — "+
			"the service must hold nothing at all for somebody who has not been "+
			"admitted (design S5)", username, accounts[0].CreatedAt, approvedAt)
	}
}

// TestEnrolmentIsOfferedFromTheSignInForm is the smallest assertion in this file
// and the one that would have caught the original state of the product.
//
// The *Sign up* link is not a template, a setting or a page: it is
// `enrollment_flow` on the identification stage, and the identification stage
// renders the link if and only if that field names a flow. Nothing else in the
// suite would notice its absence — every other test signs in as somebody the
// blueprint created — and its absence is precisely what "closed by omission"
// was (design S1).
func TestEnrolmentIsOfferedFromTheSignInForm(t *testing.T) {
	client := &http.Client{Jar: newLaxJar(), Timeout: 30 * time.Second}
	ex := authURL + "/api/v3/flows/executor/default-authentication-flow/?query="
	req, _ := http.NewRequest(http.MethodGet, ex, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", ex, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var challenge struct {
		EnrollURL string `json:"enroll_url"`
	}
	if err := json.Unmarshal(raw, &challenge); err != nil {
		t.Fatalf("decoding the identification challenge: %v", err)
	}
	if !strings.Contains(challenge.EnrollURL, enrollmentFlowSlug) {
		t.Fatalf("the sign-in form offers enrol_url %q, expected the %s flow — "+
			"without it a stranger sees a username field, a password field and no "+
			"way to become somebody who has either (design S1)",
			challenge.EnrollURL, enrollmentFlowSlug)
	}
}

// enrolStep drives one step of the enrolment flow. query is the flow's own query
// string (empty to start, `flow_token=…` to resume from the confirmation link);
// payload nil means GET — ask what the flow wants without answering.
func enrolStep(client *http.Client, query string, payload map[string]string) (flowStep, error) {
	ex := fmt.Sprintf("%s/api/v3/flows/executor/%s/?query=%s",
		authURL, enrollmentFlowSlug, url.QueryEscape(query))
	if payload == nil {
		return flowCall(client, http.MethodGet, ex, nil)
	}
	// The GET first, for the same reason executeFlow does it: the executor plans
	// the flow on the first request, and posting to a flow that has not been
	// planned answers the plan rather than the submission.
	if _, err := flowCall(client, http.MethodGet, ex, nil); err != nil {
		return flowStep{}, err
	}
	step, err := flowCall(client, http.MethodPost, ex, payload)
	if err != nil {
		return flowStep{}, err
	}
	if msg := step.errorText(); msg != "" && step.Component != "ak-stage-email" {
		// The email stage always reports `email-sent` as a field error: its
		// challenge response is declared invalid on purpose, so that the page
		// re-renders while the person goes to read their mail.
		return step, fmt.Errorf("enrolment stopped at %q: %s", step.Component, msg)
	}
	return step, nil
}

type authentikUserView struct {
	PK       int    `json:"pk"`
	Username string `json:"username"`
	IsActive bool   `json:"is_active"`
	Path     string `json:"path"`
}

func authentikAPI(t *testing.T, method, path string, body string) []byte {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, authURL+path, r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+authentikAdminToken)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: got %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw
}

func authentikUser(t *testing.T, username string) authentikUserView {
	t.Helper()
	raw := authentikAPI(t, http.MethodGet, "/api/v3/core/users/?username="+url.QueryEscape(username), "")
	var page struct {
		Results []authentikUserView `json:"results"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decoding the user list: %v", err)
	}
	if len(page.Results) != 1 {
		t.Fatalf("looking up %s at the identity provider found %d users, expected 1", username, len(page.Results))
	}
	return page.Results[0]
}

// approveAuthentikUser is the whole of what a reviewer does: tick `Active`.
func approveAuthentikUser(t *testing.T, pk int) {
	t.Helper()
	authentikAPI(t, http.MethodPatch, fmt.Sprintf("/api/v3/core/users/%d/", pk), `{"is_active":true,"path":"users"}`)
}

func deleteAuthentikUser(t *testing.T, username string) {
	t.Helper()
	raw := authentikAPI(t, http.MethodGet, "/api/v3/core/users/?username="+url.QueryEscape(username), "")
	var page struct {
		Results []authentikUserView `json:"results"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return
	}
	for _, u := range page.Results {
		authentikAPI(t, http.MethodDelete, fmt.Sprintf("/api/v3/core/users/%d/", u.PK), "")
	}
}

var confirmationLinkPattern = regexp.MustCompile(`http://auth\.localhost/if/flow/[^\s"'<>]*flow_token=[A-Za-z0-9]+`)

// confirmationLink reads the message actually sent to address out of mailpit and
// returns the link in it.
func confirmationLink(t *testing.T, address string) *url.URL {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		id := ""
		var page struct {
			Messages []struct {
				ID      string           `json:"ID"`
				To      []mailpitAddress `json:"To"`
				Subject string           `json:"Subject"`
			} `json:"messages"`
		}
		if err := getJSONFrom(mailpitAPI+"/messages?limit=50", &page); err == nil {
			for _, m := range page.Messages {
				for _, to := range m.To {
					if strings.EqualFold(to.Address, address) {
						id = m.ID
					}
				}
			}
		}
		if id != "" {
			var msg struct {
				Text string `json:"Text"`
				HTML string `json:"HTML"`
			}
			if err := getJSONFrom(mailpitAPI+"/message/"+id, &msg); err != nil {
				t.Fatalf("reading the confirmation message: %v", err)
			}
			found := confirmationLinkPattern.FindString(msg.Text + msg.HTML)
			if found == "" {
				t.Fatalf("the message to %s carries no flow link:\n%s", address, msg.Text)
			}
			u, err := url.Parse(found)
			if err != nil {
				t.Fatalf("parsing the confirmation link %q: %v", found, err)
			}
			return u
		}
		if time.Now().After(deadline) {
			t.Fatalf("no confirmation message reached %s within 20s. Enrolment sends "+
				"mail through AUTHENTIK_EMAIL__HOST, which must be the compose "+
				"service name `mailpit` and NOT mail.localhost: that name is the web "+
				"interface, served through the HTTP overlay, which SMTP does not "+
				"traverse", address)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func getJSONFrom(u string, into any) error {
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %d", u, resp.StatusCode)
	}
	return json.Unmarshal(raw, into)
}
