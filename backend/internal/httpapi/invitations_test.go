package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/mail"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// captureMail is a mail.Sender that keeps what was sent, so a test can read the
// link out of the message the way the recipient would.
//
// Reading the token out of the MESSAGE rather than out of the database is the
// point: it is the only way to assert that what we put on the wire actually
// works, which is the failure nobody notices — an invitation that is perfectly
// correct in the table and unusable in the inbox.
type captureMail struct {
	sent []mail.Message
}

func (m *captureMail) Send(_ context.Context, msg mail.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

// sentTo returns the last message addressed to an address.
func (m *captureMail) sentTo(t *testing.T, addr string) mail.Message {
	t.Helper()
	for i := len(m.sent) - 1; i >= 0; i-- {
		if m.sent[i].To == addr {
			return m.sent[i]
		}
	}
	t.Fatalf("no message was sent to %s; %d messages went out", addr, len(m.sent))
	return mail.Message{}
}

// tokenIn pulls the token out of the link in a message body.
func tokenIn(t *testing.T, m mail.Message) string {
	t.Helper()
	const marker = "?token="
	i := strings.Index(m.Body, marker)
	if i < 0 {
		t.Fatalf("the message carries no invitation link:\n%s", m.Body)
	}
	rest := m.Body[i+len(marker):]
	if j := strings.IndexAny(rest, " \n\r"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// invitedSession creates a session for somebody whose VERIFIED address is addr,
// belonging to no account at all.
//
// Belonging to nothing is the state the whole accept flow has to work in: Alice
// authenticates because she was invited, and at that instant she is a member of
// nothing (design V5). A fixture that quietly gave her a membership would test a
// path no invitee is ever on.
func (v *adminEnv) invitedSession(t *testing.T, subject, addr string) *client {
	t.Helper()
	ctx := t.Context()
	if _, err := store.FindOrCreateUser(ctx, v.db, subject, subject, addr); err != nil {
		t.Fatalf("create user %s: %v", subject, err)
	}
	now := time.Now().UTC()
	s := &model.Session{
		ID: subject + "-session-" + hexish(), Subject: subject, DisplayName: subject,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
		// No selection. This is what a session looks like immediately after a
		// first sign-in.
		SelectedAccountID: nil,
	}
	if err := store.CreateSession(ctx, v.db, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &client{t: t, e: v.e, sessionID: s.ID}
}

func (v *adminEnv) inviteAs(t *testing.T, cl *client, addr string) invitationView {
	t.Helper()
	rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/invitations",
		`{"email":"`+addr+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite %s: %d %s", addr, rec.Code, rec.Body.String())
	}
	var out invitationView
	decode(t, rec, &out)
	return out
}

// TestInvitationEndToEnd walks the whole thing the way a person does: an
// administrator invites an address, the invitee — who belongs to NOTHING —
// resolves the link out of the message and accepts, and lands in the account
// holding no role.
func TestIntegrationInvitationEndToEnd(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)

	inv := v.inviteAs(t, admin, "alice@corp.example")
	if inv.Email != "alice@corp.example" {
		t.Fatalf("invited %q", inv.Email)
	}

	msg := v.mail.sentTo(t, "alice@corp.example")
	// The subject grammar: the noun the message concerns FIRST, and no constant
	// prefix identifying us — a prefix spends the most valuable characters in an
	// inbox on what the sender column already shows (transactional-email T4).
	if !strings.HasPrefix(msg.Subject, "Invitation to ") {
		t.Errorf("subject %q does not lead with what the message is about", msg.Subject)
	}
	if strings.HasPrefix(msg.Subject, "[") {
		t.Errorf("subject %q carries a prefix", msg.Subject)
	}
	token := tokenIn(t, msg)

	alice := v.invitedSession(t, "alice-subject", "alice@corp.example")

	// Resolving renders the offer and consumes nothing, so it can be done
	// repeatedly — which is what happens when a mail gateway follows the link
	// before Alice does.
	for i := 0; i < 2; i++ {
		rec := alice.do(http.MethodPost, "/api/v1/invitations/resolve", `{"token":"`+token+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("resolve %d: %d %s", i, rec.Code, rec.Body.String())
		}
		var offer offerView
		decode(t, rec, &offer)
		if offer.Account == "" {
			t.Error("the offer names no account, so the screen cannot say what is being joined")
		}
		// And it names no role, because the invitation offers none.
		if strings.Contains(rec.Body.String(), "role") {
			t.Errorf("the offer mentions a role: %s", rec.Body.String())
		}
	}

	rec := alice.do(http.MethodPost, "/api/v1/invitations/accept", `{"token":"`+token+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}

	// The token is spent.
	if rec := alice.do(http.MethodPost, "/api/v1/invitations/resolve", `{"token":"`+token+`"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("resolve after accepting: %d, want 404", rec.Code)
	}

	// She is a member, holding no role, and the account is her selection — so
	// she is not left in the empty state immediately after joining something.
	rec = alice.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	var me struct {
		SelectedAccountID *model.ID `json:"selectedAccountId"`
		Permissions       []string  `json:"permissions"`
		Roles             []string  `json:"roles"`
	}
	decode(t, rec, &me)
	if me.SelectedAccountID == nil || *me.SelectedAccountID != v.sc.AccountID() {
		t.Errorf("after accepting, the session's selection is %v, want the account just joined", me.SelectedAccountID)
	}
	if len(me.Roles) != 0 {
		t.Errorf("the new member holds roles %v; an invitation names no role", me.Roles)
	}
	if len(me.Permissions) != 0 {
		t.Errorf("the new member holds permissions %v; a member with no role must authorise nothing", me.Permissions)
	}

	// And what she can see of the account is nothing — which is correct, and is
	// exactly why the console has to say so rather than render an empty table.
	rec = alice.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("a roleless member's project list: %d %s, want 200 and an empty list — "+
			"row-level security empties rather than fails, and the screen must explain that", rec.Code, rec.Body.String())
	}
	var projects []json.RawMessage
	decode(t, rec, &projects)
	if len(projects) != 0 {
		t.Errorf("a member holding no role saw %d projects", len(projects))
	}

	// Finally, an administrator gives her a role and the account opens up.
	rec = admin.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/members", "")
	var members []memberView
	decode(t, rec, &members)
	var waiting *memberView
	for i := range members {
		if members[i].RoleID == nil {
			waiting = &members[i]
		}
	}
	if waiting == nil {
		t.Fatal("the roster shows nobody waiting for a role; the new member is invisible as a state to act on")
	}
	rec = admin.do(http.MethodPut,
		"/api/v1/accounts/"+v.sc.AccountID().String()+"/members/"+waiting.ID.String()+"/role",
		`{"roleId":"`+v.roleID.String()+`"}`)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("assign a role: %d %s", rec.Code, rec.Body.String())
	}
}

// TestInvitationIsBoundToItsAddress is the security property of this whole
// change, and the one a plausible implementation gets wrong.
//
// A forwarded link confers nothing. Holding a link sent to alice@corp.example IS
// proof of control of that address — it is exactly the proof a confirmation mail
// provides — so the flow must refuse anybody else, and must offer no way to
// attach the invited address to whoever is signed in. That missing operation is
// what stops "bound" collapsing into "bearer with two extra clicks".
func TestIntegrationInvitationIsBoundToItsAddress(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)
	v.inviteAs(t, admin, "alice@corp.example")
	token := tokenIn(t, v.mail.sentTo(t, "alice@corp.example"))

	// Mallory was forwarded the message. She has a perfectly good session and a
	// perfectly good verified address, and neither of them is the invited one.
	mallory := v.invitedSession(t, "mallory-subject", "mallory@elsewhere.example")

	// She may RESOLVE it — the offer says only which account, which the
	// forwarded message already said — and she may not accept it.
	if rec := mallory.do(http.MethodPost, "/api/v1/invitations/resolve", `{"token":"`+token+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("resolve as the wrong user: %d %s", rec.Code, rec.Body.String())
	}
	rec := mallory.do(http.MethodPost, "/api/v1/invitations/accept", `{"token":"`+token+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a forwarded link was accepted by somebody else: %d %s, want 403", rec.Code, rec.Body.String())
	}
	// The refusal shows the address MASKED. Somebody holding a forwarded link
	// must not learn the full address from the message telling them they may not
	// use it — while the legitimate recipient, signed in as the wrong one of
	// their own identities, still has to recognise which is wanted.
	body := rec.Body.String()
	if strings.Contains(body, "alice@corp.example") {
		t.Errorf("the refusal discloses the invited address in full: %s", body)
	}
	if !strings.Contains(body, "@corp.example") {
		t.Errorf("the refusal masks the domain too, leaving nothing the right person could recognise: %s", body)
	}

	// And declining is refused for the same reason: it consumes the invitation,
	// so letting a stranger decline would let a forwarded link destroy an offer
	// meant for somebody else.
	if rec := mallory.do(http.MethodPost, "/api/v1/invitations/decline", `{"token":"`+token+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("a stranger declined somebody else's invitation: %d %s", rec.Code, rec.Body.String())
	}

	// THE OPERATION THAT MUST NOT EXIST. There is no route by which a signed-in
	// user acquires the invited address. If one is ever added, this fails — and
	// it is written as a route scan rather than as a call, because the failure
	// mode is somebody adding a helpful "claim this address" endpoint without
	// realising it is the entire binding.
	for _, r := range v.e.Routes() {
		p := strings.ToLower(r.Path)
		if strings.Contains(p, "claim") || strings.Contains(p, "verify-email") ||
			strings.Contains(p, "add-email") || strings.Contains(p, "addresses") {
			t.Errorf("%s %s looks like a way to attach an address to the signed-in user. "+
				"Holding a link sent to an address IS proof of control of it, so such an operation "+
				"turns every bound invitation into a bearer one (design V2)", r.Method, r.Path)
		}
	}

	// Alice, meanwhile, still can.
	alice := v.invitedSession(t, "alice-subject", "alice@corp.example")
	if rec := alice.do(http.MethodPost, "/api/v1/invitations/accept", `{"token":"`+token+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("the invited person could not accept: %d %s", rec.Code, rec.Body.String())
	}
}

// TestInvitationsAppearWithoutTheLink is the second way in (design V11).
//
// The mail is not reliable: it lands in spam, a gateway rewrites the URL, the
// recipient reads it on a phone and signs in on a laptop. Somebody signed in as
// the invited address sees their invitations regardless — which grants nothing
// the link grants, since being signed in as the address is strictly stronger
// evidence than holding a message sent to it.
func TestIntegrationInvitationsAppearWithoutTheLink(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)
	v.inviteAs(t, admin, "alice@corp.example")

	alice := v.invitedSession(t, "alice-subject", "alice@corp.example")
	rec := alice.do(http.MethodGet, "/api/v1/invitations", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list my invitations: %d %s", rec.Code, rec.Body.String())
	}
	var mine []offerView
	decode(t, rec, &mine)
	if len(mine) != 1 {
		t.Fatalf("found %d invitations for the invited address, want 1 — somebody whose mail was "+
			"eaten by a spam filter has to find the thing they came for", len(mine))
	}

	// Somebody else's is not presented.
	mallory := v.invitedSession(t, "mallory-subject", "mallory@elsewhere.example")
	rec = mallory.do(http.MethodGet, "/api/v1/invitations", "")
	decode(t, rec, &mine)
	if len(mine) != 0 {
		t.Fatalf("an address with no invitations was shown %d", len(mine))
	}
}

// TestIntegrationInvitationAnsweredWithoutItsToken covers the second way in, and
// the property that makes it safe.
//
// An identifier is guessable in a way a 256-bit token is not, so the lookup
// requires the invited address IN ITS WHERE CLAUSE rather than comparing
// afterwards. Somebody else's identifier is therefore indistinguishable from one
// belonging to no invitation at all — the endpoint answers no question about
// whether an invitation exists.
func TestIntegrationInvitationAnsweredWithoutItsToken(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)
	v.inviteAs(t, admin, "alice@corp.example")

	alice := v.invitedSession(t, "alice-subject", "alice@corp.example")
	rec := alice.do(http.MethodGet, "/api/v1/invitations", "")
	var mine []offerView
	decode(t, rec, &mine)
	if len(mine) != 1 || mine[0].ID == nil {
		t.Fatalf("the listing carries no identifier to answer with: %s", rec.Body.String())
	}
	id := mine[0].ID.String()

	// Mallory, holding the identifier, gets the same answer she would get for an
	// identifier nobody ever issued.
	mallory := v.invitedSession(t, "mallory-subject", "mallory@elsewhere.example")
	hers := mallory.do(http.MethodPost, "/api/v1/invitations/accept", `{"id":"`+id+`"}`)
	nothing := mallory.do(http.MethodPost, "/api/v1/invitations/accept",
		`{"id":"01900000-0000-7000-8000-000000000000"}`)
	if hers.Code != nothing.Code {
		t.Fatalf("somebody else's invitation answers %d and a nonexistent one answers %d — "+
			"the difference is an oracle telling anybody holding an identifier whether it names "+
			"a real invitation", hers.Code, nothing.Code)
	}
	if hers.Code == http.StatusOK {
		t.Fatal("a stranger accepted an invitation by naming its identifier")
	}

	// And Alice, who is the invited address, can — with no token anywhere.
	if rec := alice.do(http.MethodPost, "/api/v1/invitations/accept", `{"id":"`+id+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("accepting off one's own list: %d %s", rec.Code, rec.Body.String())
	}
}

// TestInvitationDeclineIsDistinguishableFromSilence.
//
// The whole reason declining exists: fourteen days of silence tells an
// administrator neither that the person never saw it nor that they said no, and
// the two call for opposite responses.
func TestIntegrationInvitationDeclineIsVisible(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()
	v.inviteAs(t, admin, "alice@corp.example")
	token := tokenIn(t, v.mail.sentTo(t, "alice@corp.example"))

	alice := v.invitedSession(t, "alice-subject", "alice@corp.example")
	if rec := alice.do(http.MethodPost, "/api/v1/invitations/decline", `{"token":"`+token+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("decline: %d %s", rec.Code, rec.Body.String())
	}

	// It leaves the pending list.
	rec := admin.do(http.MethodGet, acct+"/invitations", "")
	var pending []invitationView
	decode(t, rec, &pending)
	if len(pending) != 0 {
		t.Fatalf("a declined invitation is still pending (%d)", len(pending))
	}

	// The audit trail says DECLINED rather than nothing, which is what makes it
	// distinguishable from a message that never arrived.
	rec = admin.do(http.MethodGet, acct+"/audit?action=invitation.decline", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invitation.decline") {
		t.Errorf("no decline was recorded; to the account this is indistinguishable from silence, "+
			"and the two call for opposite responses: %s", rec.Body.String())
	}

	// And a decline is not a block: the account may ask again.
	v.inviteAs(t, admin, "alice@corp.example")
}

// TestInvitationAuditNeverCarriesTheToken.
//
// An audit trail containing a usable credential is a credential store with a
// retention policy.
func TestIntegrationInvitationAuditNeverCarriesTheToken(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()
	v.inviteAs(t, admin, "alice@corp.example")
	token := tokenIn(t, v.mail.sentTo(t, "alice@corp.example"))

	rec := admin.do(http.MethodGet, acct+"/audit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, token) {
		t.Fatal("the audit trail contains the invitation token")
	}
	if strings.Contains(body, "token") {
		t.Errorf("an audit entry mentions a token at all: %s", body)
	}
}

// TestInvitationRateLimitRefusesAndRecords.
func TestIntegrationInvitationRateLimitIsEnforcedAtTheRoute(t *testing.T) {
	v := newAdminEnvWithMail(t)
	v.opts.Config.InvitationsPerAccountPerHour = 1
	v.opts.Config.InvitationsPerAddressPerDay = 5
	v.reregister(t)

	admin := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()
	v.inviteAs(t, admin, "first@corp.example")

	rec := admin.do(http.MethodPost, acct+"/invitations", `{"email":"second@corp.example"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a second invitation within the limit: %d %s, want 429", rec.Code, rec.Body.String())
	}
	// The refusal says when to retry. Without it, "you are doing that too much"
	// is an instruction to retry in a loop — which is what the limit was trying
	// to stop.
	if rec.Header().Get("Retry-After") == "" {
		t.Error("the refusal carries no Retry-After")
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "try again") {
		t.Errorf("the message does not say when to retry: %s", rec.Body.String())
	}
	// And nothing was created.
	rec = admin.do(http.MethodGet, acct+"/invitations", "")
	var pending []invitationView
	decode(t, rec, &pending)
	if len(pending) != 1 {
		t.Fatalf("%d invitations outstanding after a refused one, want 1", len(pending))
	}
}

// TestInvitationDoesNotRevealWhetherTheAddressHasAUser.
//
// A different status or body would make this endpoint an oracle telling any
// account administrator whether a given address has an account with us.
func TestIntegrationInvitationRevealsNothingAboutTheAddress(t *testing.T) {
	v := newAdminEnvWithMail(t)
	admin := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	// One address that has a user, one that does not.
	if _, err := store.FindOrCreateUser(t.Context(), v.db, "known-subject", "Known", "known@corp.example"); err != nil {
		t.Fatal(err)
	}

	known := admin.do(http.MethodPost, acct+"/invitations", `{"email":"known@corp.example"}`)
	unknown := admin.do(http.MethodPost, acct+"/invitations", `{"email":"unknown@corp.example"}`)

	if known.Code != unknown.Code {
		t.Fatalf("statuses differ: %d for an address with a user, %d for one without", known.Code, unknown.Code)
	}
	var a, b invitationView
	decode(t, known, &a)
	decode(t, unknown, &b)
	// Everything except the identifier, the address and the timestamps must
	// match; those three differ for reasons that have nothing to do with whether
	// a user exists.
	if a.InvitedBy != b.InvitedBy {
		t.Errorf("responses differ beyond the address: %s vs %s", known.Body.String(), unknown.Body.String())
	}
}
