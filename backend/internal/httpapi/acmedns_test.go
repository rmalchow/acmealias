package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The acme-dns dialect (design N1–N6).
//
// The premise of the whole change is that /update is /present with a different
// spelling, so the tests that matter are the ones asserting that the two
// spellings are not two of anything: not two authorization surfaces, not two
// ways into the inventory, not two sets of rules about what a key may reach.

// acmeDNS posts an acme-dns update with the pair of headers its clients send.
func acmeDNS(v *env, user, pass, subdomain, txt string) *httptest.ResponseRecorder {
	v.t.Helper()
	body := `{"subdomain":` + quote(subdomain) + `,"txt":` + quote(txt) + `}`
	return post(v.e, "/update", body, func(r *http.Request) {
		r.Header.Set("X-Api-User", user)
		r.Header.Set("X-Api-Key", pass)
	})
}

// A publication in the acme-dns dialect is answered exactly as one by challenge
// name, and the response carries the value back — which is how a client confirms
// the write landed rather than trusting a bare 200.
func TestIntegrationAcmeDNSUpdatePublishes(t *testing.T) {
	v := newEnv(t)

	rec := acmeDNS(v, v.oursUser, v.oursPass, testLabel, "acme-dns-value")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /update: %d %s, want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		TXT string `json:"txt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if body.TXT != "acme-dns-value" {
		t.Errorf(`response {"txt": %q}, want the value that was stored — clients check it`, body.TXT)
	}

	// Answerable BY THE TIME THE RESPONSE ARRIVES, which is the same promise
	// /present makes and is what the shared zone-view refresh is for.
	v.assertAnswers("acme-dns-value")

	// And it ADDS rather than displaces, exactly as /present does: a certificate
	// covering example.com and *.example.com validates two authorizations at one
	// name, and the CA expects both values there at once.
	if rec := acmeDNS(v, v.oursUser, v.oursPass, testLabel, "second-value"); rec.Code != http.StatusOK {
		t.Fatalf("second update: %d %s", rec.Code, rec.Body.String())
	}
	v.assertAnswers("acme-dns-value", "second-value")
}

// The property design N6 names, and the one worth breaking the change over: a
// key that cannot publish for an alias by challenge name cannot publish for it
// by label either. Two spellings must not be two authorization surfaces.
func TestIntegrationAcmeDNSIsNotASecondAuthorizationSurface(t *testing.T) {
	v := newEnv(t)

	// The other project's key, which /present refuses for this alias.
	v.callAs(v.theirs, "/present", testFQDN, "should-not-publish", http.StatusForbidden)

	rec := acmeDNS(v, v.theirsUser, v.theirsPass, testLabel, "should-not-publish")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /update with a key for another project: %d %s, want 403 — the label spelling "+
			"must not reach an alias the challenge-name spelling cannot", rec.Code, rec.Body.String())
	}
	// Nothing was written. Asserted on the rows rather than on the zone view,
	// because the view is only reloaded after a SUCCESSFUL publish — a refused
	// one leaves it as it was, which here is before it has ever been loaded.
	if n := v.rows(); n != 0 {
		t.Fatalf("a refused publication wrote %d challenge row(s)", n)
	}

	// A label nobody issued is refused identically, and with the SAME message as
	// an alias belonging to somebody else: an authenticated caller must not be
	// able to map the inventory by the difference between the two.
	unknown := acmeDNS(v, v.oursUser, v.oursPass, "no-such-label", "x")
	if unknown.Code != http.StatusForbidden {
		t.Fatalf("POST /update for an unknown label: %d, want 403", unknown.Code)
	}
	if unknown.Body.String() != rec.Body.String() {
		t.Errorf("an unknown label and another account's alias answer differently:\n %s\n %s",
			unknown.Body.String(), rec.Body.String())
	}
}

// The credential is the same credential. acme-dns sends its half in two headers
// and lego sends the same pair as basic auth; the middleware resolves both to
// one key, and everything downstream — expiry, revocation, the address
// restrictions, the grants — applies identically (design N5).
func TestIntegrationAcmeDNSHeadersAreTheSameCredential(t *testing.T) {
	v := newEnv(t)

	// Same key, both transports, same outcome.
	v.call("/present", testFQDN, "by-basic-auth", http.StatusOK)
	if rec := acmeDNS(v, v.oursUser, v.oursPass, testLabel, "by-headers"); rec.Code != http.StatusOK {
		t.Fatalf("POST /update: %d %s", rec.Code, rec.Body.String())
	}
	v.assertAnswers("by-basic-auth", "by-headers")

	// A wrong password is the same opaque 401 every other rejection is.
	if rec := acmeDNS(v, v.oursUser, "not-the-secret", testLabel, "x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d, want 401", rec.Code)
	}
	if rec := acmeDNS(v, "k_00000000", v.oursPass, testLabel, "x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: %d, want 401", rec.Code)
	}

	// Revocation reaches this dialect too — there is one key row, and one place
	// that decides whether it still works.
	sc := store.ScopeForKey(&model.APIKey{AccountID: v.alias.AccountID})
	k, err := store.FindAPIKeyByIdentifier(t.Context(), v.db, v.oursUser)
	if err != nil {
		t.Fatalf("find key: %v", err)
	}
	if err := store.RevokeAPIKey(t.Context(), v.db, sc, k.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if rec := acmeDNS(v, v.oursUser, v.oursPass, testLabel, "after-revocation"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked key published through /update: %d", rec.Code)
	}
}

// A malformed acme-dns body is a 400 that names ACME-DNS's fields, not ours.
// Telling an acme.sh operator we expected "fqdn" and "value" would send them to
// the wrong documentation.
func TestIntegrationAcmeDNSBodyErrorsNameItsOwnFields(t *testing.T) {
	v := newEnv(t)

	for _, body := range []string{
		`{}`,
		`{"subdomain":"` + testLabel + `"}`,
		`{"txt":"value-only"}`,
		`{"subdomain":"` + testLabel + `","txt":""}`,
		// A name rather than a label. It cannot name an alias, and refusing it
		// as malformed keeps "fix your config" distinct from "fix your grants".
		`{"subdomain":"` + testLabel + `.` + testZone + `","txt":"x"}`,
		`not json at all`,
	} {
		rec := post(v.e, "/update", body, func(r *http.Request) {
			r.Header.Set("X-Api-User", v.oursUser)
			r.Header.Set("X-Api-Key", v.oursPass)
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST /update %s: %d, want 400", body, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "subdomain") || !strings.Contains(rec.Body.String(), "txt") {
			t.Errorf("POST /update %s: %s — the message must name acme-dns's own fields",
				body, rec.Body.String())
		}
	}
	if n := v.rows(); n != 0 {
		t.Fatalf("a malformed body wrote %d challenge row(s)", n)
	}
}

// /register refuses, says where credentials come from, and creates nothing
// (design N3, tasks 3.1–3.4).
//
// The refusal is the feature. A 404 would send an operator looking for a base
// path they mistyped; the truth is that this service does not mint registrations
// at all, and only a route can say so.
func TestIntegrationAcmeDNSRegisterRefusesAndCreatesNothing(t *testing.T) {
	v := newEnv(t)

	before := struct{ accounts, projects, domains, aliases, keys int64 }{}
	count := func() (n struct{ accounts, projects, domains, aliases, keys int64 }) {
		v.db.Model(&model.Account{}).Count(&n.accounts)
		v.db.Model(&model.Project{}).Count(&n.projects)
		v.db.Model(&model.Domain{}).Count(&n.domains)
		v.db.Model(&model.Alias{}).Count(&n.aliases)
		v.db.Model(&model.APIKey{}).Count(&n.keys)
		return n
	}
	before = count()

	// Unauthenticated, which is how every acme-dns client calls it.
	rec := post(v.e, "/register", `{"allowfrom":["203.0.113.0/24"]}`, nil)
	if rec.Code == http.StatusNotFound {
		t.Fatal("/register answered 404. A client reports that as a wrong URL and its operator goes " +
			"looking for a base path they mistyped; the truth is that credentials are not minted here")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /register: %d %s, want 403", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"console", "acme-dns credentials", "/update"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not mention %q, so it does not say where to go: %s", want, body)
		}
	}

	// And with credentials, in case somebody decides to try. It must not become
	// an authenticated registration endpoint either: a client that calls
	// /register calls it unauthenticated, so an authenticated one is a route
	// that exists only to be able to say it exists.
	authed := post(v.e, "/register", `{}`, func(r *http.Request) {
		r.Header.Set("X-Api-User", v.oursUser)
		r.Header.Set("X-Api-Key", v.oursPass)
	})
	if authed.Code != http.StatusForbidden {
		t.Errorf("POST /register with credentials: %d, want the same 403", authed.Code)
	}

	if after := count(); after != before {
		t.Errorf("an unauthenticated publication-protocol request changed the inventory: %+v -> %+v.\n"+
			"This is the exact shape tenancy-and-permissions exists to prevent.", before, after)
	}
}

// A value published with no way to withdraw it still stops being answered
// (design N4, task 5.1).
//
// acme-dns has only /update: a client speaking it never tells us it is finished.
// So the withdrawal comes from ChallengeLifetime and the sweep, and this asserts
// the whole path — that the value goes, and that the history row it leaves
// behind is NOT reported as abandoned for behaving exactly as its protocol
// specifies.
func TestIntegrationAcmeDNSPublicationExpiresWithoutASignal(t *testing.T) {
	v := newEnv(t)

	if rec := acmeDNS(v, v.oursUser, v.oursPass, testLabel, "unwithdrawn"); rec.Code != http.StatusOK {
		t.Fatalf("POST /update: %d %s", rec.Code, rec.Body.String())
	}
	v.assertAnswers("unwithdrawn")

	// The interaction was opened in the acme-dns form, which is what the sweep
	// reads back to tell an expiry from an abandonment.
	var in model.ChallengeInteraction
	if err := v.db.Where("alias_id = ?", v.alias.ID).First(&in).Error; err != nil {
		t.Fatalf("read interaction: %v", err)
	}
	if in.FQDNForm != store.FormAcmeDNS {
		t.Fatalf("the interaction records form %q, want %q — without it the sweep cannot tell an "+
			"expiry from a client that vanished", in.FQDNForm, store.FormAcmeDNS)
	}

	// Age the value and the row past the lifetime, then run what the sweeper
	// runs. Nothing here is a client signal, because there is none to send.
	past := time.Now().Add(-2 * time.Hour)
	if err := v.db.Model(&model.Challenge{}).Where("alias_id = ?", v.alias.ID).
		Update("expires_at", past).Error; err != nil {
		t.Fatalf("age challenge: %v", err)
	}
	if err := v.db.Model(&model.ChallengeInteraction{}).Where("id = ?", in.ID).
		Update("published_at", past).Error; err != nil {
		t.Fatalf("age interaction: %v", err)
	}
	if _, err := store.DeleteExpired(t.Context(), v.db); err != nil {
		t.Fatalf("sweep challenges: %v", err)
	}
	if err := v.view.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	v.assertAnswers()

	closed, err := store.AbandonStaleInteractions(t.Context(), v.db, time.Hour)
	if err != nil {
		t.Fatalf("close stale: %v", err)
	}
	if closed.Abandoned != 0 {
		t.Errorf("an acme-dns publication was counted as abandoned. Its protocol has no withdrawal, "+
			"so expiring is its normal ending; reporting that as a fault would train somebody to "+
			"ignore the column (got %+v)", closed)
	}
	var after model.ChallengeInteraction
	if err := v.db.First(&after, "id = ?", in.ID).Error; err != nil {
		t.Fatalf("reload interaction: %v", err)
	}
	if after.WithdrawnAt == nil {
		t.Error("the interaction was left open; the console would report it as still in flight forever")
	}
	if after.Abandoned {
		t.Error("the interaction was marked abandoned")
	}
}

// The credentials file, and the loop it closes (design N3, tasks 4.1–4.3).
//
// The four field names are acme-dns's and are not ours to tidy: cert-manager
// unmarshals a Kubernetes Secret into exactly them. A near-miss here fails at
// renewal time, weeks later, inside a cluster — which is why this asserts the
// literal JSON keys rather than a Go struct that would happily rename them.
func TestIntegrationAcmeDNSCredentialsAreWhatClientsRead(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	acct := "/api/v1/accounts/" + v.sc.AccountID().String()

	rec := cl.do(http.MethodPost, acct+"/projects/"+itoa(v.projID)+"/domains",
		`{"name":"shop.example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create domain: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Domains []domainView `json:"domains"`
	}
	decode(t, rec, &created)
	if len(created.Domains) == 0 {
		t.Fatalf("no domain in %s", rec.Body.String())
	}
	d := created.Domains[0]

	rec = cl.do(http.MethodPost, acct+"/domains/"+itoa(d.ID)+"/acme-dns", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue credentials: %d %s", rec.Code, rec.Body.String())
	}

	// Decoded as a bare map, on purpose: the point of the test is the KEYS on
	// the wire, and a typed decode would pass against a renamed field.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	creds, ok := body["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("no credentials object in %s", rec.Body.String())
	}
	for _, field := range []string{"username", "password", "fulldomain", "subdomain"} {
		s, ok := creds[field].(string)
		if !ok || s == "" {
			t.Errorf("credentials.%s is missing or empty; cert-manager reads exactly these four "+
				"keys out of a Kubernetes Secret and nobody notices a typo there", field)
		}
	}
	if creds["subdomain"] != d.AliasLabel {
		t.Errorf("subdomain is %v, want the alias label %q — that is what POST /update names",
			creds["subdomain"], d.AliasLabel)
	}
	if got, want := creds["fulldomain"], d.AliasLabel+"."+strings.TrimSuffix(v.opts.Config.Zone, "."); got != want {
		t.Errorf("fulldomain is %v, want %q — the name the customer's CNAME points at", got, want)
	}

	// The key it hands out is a PROJECT key, which is the whole reason
	// api-key-kinds sits before this change: the file somebody pastes into a
	// Secret and never looks at again carries a credential that is bound to one
	// project and dies with it, rather than one assembled by hand out of the
	// grant grammar.
	var issued struct {
		Key    keyView `json:"key"`
		Notice string  `json:"notice"`
		CNAME  string  `json:"cname"`
	}
	decode(t, rec, &issued)
	if issued.Key.Kind != model.APIKeyKindProject {
		t.Errorf("the credentials carry a %q key, want %q", issued.Key.Kind, model.APIKeyKindProject)
	}
	if issued.Key.ProjectID == nil || *issued.Key.ProjectID != v.projID {
		t.Errorf("the key is bound to %v rather than the domain's project", issued.Key.ProjectID)
	}
	if issued.Notice == "" {
		t.Error("no show-once notice; the secret is in this response and nowhere else, ever")
	}
	if !strings.Contains(issued.CNAME, d.ChallengeName) {
		t.Errorf("the CNAME line %q does not name the record the customer publishes", issued.CNAME)
	}

	// The secret appears here and in no listing afterwards. Same rule as the key
	// screen, and it is worth re-asserting on a second route that produces one.
	password := creds["password"].(string)
	list := cl.do(http.MethodGet, acct+"/projects/"+itoa(v.projID)+"/keys", "")
	if strings.Contains(list.Body.String(), password) {
		t.Fatal("the key listing returned the secret from the credentials file")
	}
}
