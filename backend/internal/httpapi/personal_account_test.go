package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// personalSession materializes a user's personal account the way sign-in does,
// and returns a client acting inside it.
//
// It goes through store.EnsurePersonalAccount rather than inserting rows,
// because the thing under test in this file is the account the production path
// produces — a fixture that built its own could give it roles, members or a
// balance that the real one never has.
func personalSession(t *testing.T, v *adminEnv, subject string) (*client, model.ID) {
	t.Helper()
	ctx := t.Context()
	user, err := store.FindOrCreateUser(ctx, v.db, subject, strings.ToUpper(subject), "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc, err := store.EnsurePersonalAccount(ctx, v.db, user)
	if err != nil {
		t.Fatalf("materialize personal account: %v", err)
	}
	now := time.Now().UTC()
	id := acc.ID
	s := &model.Session{
		ID: subject + "-personal-" + hexish(), Subject: subject, DisplayName: strings.ToUpper(subject),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
		SelectedAccountID: &id,
	}
	if err := store.CreateSession(ctx, v.db, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &client{t: t, e: v.e, sessionID: s.ID}, acc.ID
}

// TestIntegrationAPersonalAccountHasNoRoster is task 3.3 and design C9.
//
// The assertion is the STATUS, not the emptiness. An empty list would read as
// "there are none yet" and invite a client to render an Add button for something
// that can never exist; a refusal says "there cannot be any", which is the true
// sentence. And it is not a 403: somebody told they lack permission goes looking
// for whoever can grant it, and in an account of one there is nobody.
func TestIntegrationAPersonalAccountHasNoRoster(t *testing.T) {
	v := newAdminEnv(t)
	cl, accountID := personalSession(t, v, "solo")
	acct := "/api/v1/accounts/" + accountID.String()

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, acct + "/roles", ""},
		{http.MethodPost, acct + "/roles", `{"name":"ops","grants":[]}`},
		{http.MethodGet, acct + "/members", ""},
		{http.MethodPost, acct + "/members", `{"subject":"someone","roleId":"` + model.NewID().String() + `"}`},
	} {
		rec := cl.do(tc.method, tc.path, tc.body)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s %s in a personal account: %d %s, want 409 — a refusal, not an empty collection",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
		var body ErrorBody
		decode(t, rec, &body)
		if strings.Contains(strings.ToLower(body.Message), "permission") {
			t.Fatalf("the refusal %q reads as a permission failure; there is nobody to ask (design C11)", body.Message)
		}
	}
}

// TestIntegrationGrantsStayLiveInAPersonalAccount is task 3.4, and it is here
// because it is the task most likely to be deleted by accident.
//
// Roles are how PEOPLE get authority and exist only where there are people to
// give them to. Grants are also how a KEY is scoped, and a key narrower than its
// holder is good practice in an account of one. Deleting the roles machinery
// along with the roles screen would break key scoping, and this is what would
// notice.
func TestIntegrationGrantsStayLiveInAPersonalAccount(t *testing.T) {
	v := newAdminEnv(t)
	cl, accountID := personalSession(t, v, "keyholder")
	acct := "/api/v1/accounts/" + accountID.String()

	rec := cl.do(http.MethodPost, acct+"/projects", `{"slug":"solo","name":"Solo"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project in a personal account: %d %s, want 201", rec.Code, rec.Body.String())
	}
	var proj projectView
	decode(t, rec, &proj)

	rec = cl.do(http.MethodPost, acct+"/projects/"+proj.ID.String()+"/loadbalancers", `{"slug":"eu","name":"EU"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create load balancer: %d %s", rec.Code, rec.Body.String())
	}
	var lb loadBalancerView
	decode(t, rec, &lb)
	lbPath := acct + "/loadbalancers/" + lb.ID.String()

	// The key minted here is NARROWER than its holder: the owner is
	// unrestricted — every action on every object of the account — and the key
	// gets two grants over one load balancer. That narrowing is the good
	// practice C9 says survives in an account of one, and it is what breaks if
	// somebody deletes the grant machinery along with the roles screen.
	rec = cl.do(http.MethodPost, lbPath+"/keys", `{"note":"renewals"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create a key in a personal account: %d %s, want 201 — the grant model is not conditional on kind",
			rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if len(created.Key.Grants) != 2 {
		t.Fatalf("the key holds %d grants, want two over one load balancer: %s", len(created.Key.Grants), rec.Body.String())
	}
	for _, g := range created.Key.Grants {
		if g.ObjectID == nil || *g.ObjectID != lb.ID {
			t.Fatalf("a grant names %v rather than the load balancer the key was minted for; the key is not scoped", g.ObjectID)
		}
	}

	// The subset rule itself runs on this path — a.mint hands store.CreateAPIKey
	// a real check rather than store.AllowAnyGrant, in a personal account as in
	// any other. The case where it REFUSES cannot be reached from here and that
	// is a property of the account rather than of the rule: a personal account's
	// only member is its owner, who is unrestricted, so there is nothing a key
	// could be given that they do not hold. The refusal is pinned by the
	// team-account tests, which go through the same two functions.
}

// TestIntegrationThePersonalQuotaIsRoomAndNotPermission is design C13.
//
// Two things are asserted and the second is the one that is easy to lose. The
// quota fires — five is five. And it fires as a QUOTA: 409 with a message about
// room, returned to a caller who holds every grant there is, so it cannot be
// mistaken for a permission the caller might go and ask somebody for.
func TestIntegrationThePersonalQuotaIsRoomAndNotPermission(t *testing.T) {
	v := newAdminEnv(t)
	// The quota is configuration (task 3.5a), so the test sets it rather than
	// asserting a constant. Three, not five, because what is under test is that
	// the configured number is the one enforced.
	v.opts.Config.ProjectsPerPersonalAccount = 3
	v.reregister(t)

	cl, accountID := personalSession(t, v, "quota")
	acct := "/api/v1/accounts/" + accountID.String()

	for i := range 3 {
		body := `{"slug":"p` + string(rune('a'+i)) + `","name":"P"}`
		if rec := cl.do(http.MethodPost, acct+"/projects", body); rec.Code != http.StatusCreated {
			t.Fatalf("project %d: %d %s, want 201", i, rec.Code, rec.Body.String())
		}
	}
	rec := cl.do(http.MethodPost, acct+"/projects", `{"slug":"pd","name":"P"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("the fourth project in an account limited to three: %d %s, want 409 — a lack of room, not of authority",
			rec.Code, rec.Body.String())
	}
	var body ErrorBody
	decode(t, rec, &body)
	if strings.Contains(strings.ToLower(body.Message), "permitted") || strings.Contains(strings.ToLower(body.Message), "permission") {
		t.Fatalf("the refusal %q reads as a permission failure; the caller holds every grant there is", body.Message)
	}
	if !strings.Contains(body.Message, "team account") {
		t.Fatalf("the refusal %q does not say what would resolve it", body.Message)
	}

	// A TEAM account has no project quota at all: credit is its only limit, and
	// capping projects on top of metering would charge twice for the same
	// growth. The fixture account already holds one project, so this takes it
	// past the personal limit.
	team := v.session("admin", roleAdmin)
	teamAcct := "/api/v1/accounts/" + v.sc.AccountID().String()
	for i := range 5 {
		body := `{"slug":"t` + string(rune('a'+i)) + `","name":"T"}`
		if rec := team.do(http.MethodPost, teamAcct+"/projects", body); rec.Code != http.StatusCreated {
			t.Fatalf("team project %d: %d %s, want 201 — a team account has no project quota", i, rec.Code, rec.Body.String())
		}
	}
}

// TestIntegrationTheOwnerTransfersATeamAccount is task 3.6: the owner column
// moves, the recipient becomes a member, and the change is recorded — all in one
// transaction, because an owner who is not a member cannot open their own
// account.
func TestIntegrationTheOwnerTransfersATeamAccount(t *testing.T) {
	v := newAdminEnv(t)
	owner := v.session("fixture-owner", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	// A member holding the unrestricted role may NOT transfer: administering an
	// account and owning it are different authorities.
	member := v.session("admin", roleAdmin)
	if rec := member.do(http.MethodPost, acct+"/transfer", `{"subject":"successor"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("transfer by a non-owner: %d %s, want 403", rec.Code, rec.Body.String())
	}

	if rec := owner.do(http.MethodPost, acct+"/transfer", `{"subject":"successor"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("transfer by the owner: %d %s, want 204", rec.Code, rec.Body.String())
	}

	acc, err := store.GetAccount(t.Context(), v.db, v.sc)
	if err != nil {
		t.Fatalf("read account: %v", err)
	}
	successor, err := store.FindOrCreateUser(t.Context(), v.db, "successor", "successor", "")
	if err != nil {
		t.Fatalf("read successor: %v", err)
	}
	if acc.OwnerUserID != successor.ID {
		t.Fatal("the owner column did not move")
	}
	is, err := store.IsMember(t.Context(), v.db, v.sc.AccountID(), successor.ID)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	if !is {
		t.Fatal("the new owner is not a member; an owner who is not a member cannot open their own account")
	}

	rec := member.do(http.MethodGet, acct+"/audit?action=account.transfer", "")
	if !strings.Contains(rec.Body.String(), "account.transfer") {
		t.Fatalf("the transfer left no audit entry: %s", rec.Body.String())
	}
}

// TestIntegrationAPersonalAccountIsNotTransferable is design C1 through the API.
// It is one person's own workspace; handing it over would make it a team account
// with the team parts missing.
func TestIntegrationAPersonalAccountIsNotTransferable(t *testing.T) {
	v := newAdminEnv(t)
	cl, accountID := personalSession(t, v, "unmovable")
	rec := cl.do(http.MethodPost, "/api/v1/accounts/"+accountID.String()+"/transfer", `{"subject":"somebody"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("transferring a personal account: %d %s, want 409 — nobody may do it, so there is no grant to look for",
			rec.Code, rec.Body.String())
	}
}

// TestIntegrationEveryUserHasSomewhereToWork is task 7.6's property asserted at
// this layer: the picker is never empty, so "no account selected" means "you
// have not said which" and never "you have nothing".
func TestIntegrationEveryUserHasSomewhereToWork(t *testing.T) {
	v := newAdminEnv(t)
	cl, accountID := personalSession(t, v, "newcomer")

	rec := cl.do(http.MethodGet, "/api/v1/accounts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", rec.Code, rec.Body.String())
	}
	var listed []accountView
	decode(t, rec, &listed)
	if len(listed) != 1 {
		t.Fatalf("a user with no team accounts sees %d accounts, want exactly their own personal one", len(listed))
	}
	if listed[0].ID != accountID || listed[0].Kind != model.AccountKindPersonal {
		t.Fatalf("the one account offered is %+v, want their personal account", listed[0])
	}
	if listed[0].Credits != 0 || listed[0].Suspended {
		t.Fatalf("a personal account is shown as %+v; it is free and always usable", listed[0])
	}
}
