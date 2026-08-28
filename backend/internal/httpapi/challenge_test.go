package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", for schema setup only
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// testDatabaseURLEnv mirrors internal/store's convention: the database-backed
// tests here skip, visibly, when it is unset. The body-validation and
// authentication tests below need no database and always run.
const testDatabaseURLEnv = "TEST_DATABASE_URL"

const (
	testFQDN = "_acme-challenge.shop.example.com"
	// The domain testFQDN is the challenge name of. Aliases exist for domains,
	// so a fixture holding one without the other models a state the inventory
	// service cannot produce — and, since domain-activation, one that is
	// refused.
	testDomain = "shop.example.com"
	testLabel  = "abc123def456"
	testZone   = "dev.acmeservice.internal"
	// testAliasName is what lego sends once it has followed the customer's
	// CNAME: our own name, not theirs.
	testAliasName = testLabel + "." + testZone
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// dbLookup is the production wiring of apikey.Lookup, which group 16 repeats.
func dbLookup(db *gorm.DB) apikey.Lookup {
	return func(ctx context.Context, identifier string) (*model.APIKey, error) {
		return store.FindAPIKeyByIdentifier(ctx, db, identifier)
	}
}

// --- body validation (no database) -----------------------------------------

func TestDecodeChallenge(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantFQDN   string
		wantValue  string
		wantReject bool
	}{
		{name: "default mode", body: `{"fqdn":"_acme-challenge.shop.example.com.","value":"gfj9Xq"}`,
			wantFQDN: "_acme-challenge.shop.example.com.", wantValue: "gfj9Xq"},
		{name: "extra fields are tolerated", body: `{"fqdn":"a.example.com","value":"v","future":1}`,
			wantFQDN: "a.example.com", wantValue: "v"},
		{name: "raw mode", body: `{"domain":"shop.example.com","token":"t","keyAuth":"k"}`, wantReject: true},
		{name: "malformed json", body: `{"fqdn":`, wantReject: true},
		{name: "empty body", body: ``, wantReject: true},
		{name: "not an object", body: `"fqdn"`, wantReject: true},
		{name: "missing value", body: `{"fqdn":"a.example.com"}`, wantReject: true},
		{name: "missing fqdn", body: `{"value":"v"}`, wantReject: true},
		{name: "empty fqdn", body: `{"fqdn":"","value":"v"}`, wantReject: true},
		{name: "empty value", body: `{"fqdn":"a.example.com","value":""}`, wantReject: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/present", strings.NewReader(tc.body))
			fqdn, value, err := decodeChallenge(req)
			if tc.wantReject {
				if err == nil {
					t.Fatalf("body %q was accepted as %q/%q, want 400", tc.body, fqdn, value)
				}
				return
			}
			if err != nil {
				t.Fatalf("body %q rejected: %v", tc.body, err)
			}
			if fqdn != tc.wantFQDN || value != tc.wantValue {
				t.Fatalf("got %q/%q, want %q/%q", fqdn, value, tc.wantFQDN, tc.wantValue)
			}
		})
	}
}

// The 400 must name both expected fields: that message is the only thing
// standing between an operator and an afternoon spent wondering why
// HTTPREQ_MODE=RAW "does nothing".
func TestBadBodyMessageNamesExpectedFields(t *testing.T) {
	msg, _ := errBadBody.Message.(string)
	for _, want := range []string{"fqdn", "value", "RAW"} {
		if !strings.Contains(msg, want) {
			t.Errorf("400 message %q does not mention %q", msg, want)
		}
	}
}

// --- authentication ordering (no database) ---------------------------------

// serverWithoutDB builds the routes with a database handle that would panic if
// it were ever used. That is the assertion: every test using it proves the
// request was answered before any alias lookup happened.
func serverWithoutDB(t *testing.T, keys ...*model.APIKey) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	err := RegisterChallenge(context.Background(), e, ChallengeOptions{
		DB:   &gorm.DB{}, // non-nil, unusable: touching it fails the test loudly
		Zone: testZone,
		View: zoneview.New(func(context.Context) (map[string][]store.ChallengeValue, error) { return nil, nil }),
		Lookup: func(_ context.Context, id string) (*model.APIKey, error) {
			for _, k := range keys {
				if k.Identifier == id {
					return k, nil
				}
			}
			return nil, gorm.ErrRecordNotFound
		},
		// Non-empty, so the startup guard needs no database to clear.
		TrustedProxyCIDRs: []string{"192.0.2.1/32"},
		Lifetime:          time.Hour,
		Log:               discard(),
	})
	if err != nil {
		t.Fatalf("RegisterChallenge: %v", err)
	}
	return e
}

func post(e *echo.Echo, path, body string, auth func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.RemoteAddr = "198.51.100.9:54321"
	if auth != nil {
		auth(req)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// The API must not be an oracle listing the domains we serve: without valid
// credentials, a mapped and an unmapped name are answered identically — byte for
// byte, and without the database being consulted at all.
func TestUnauthenticatedRequestsRevealNothing(t *testing.T) {
	k, _, err := apikey.New()
	if err != nil {
		t.Fatal(err)
	}
	k.ID = model.NewID()
	e := serverWithoutDB(t, k)

	answer := func(name, body string, auth func(*http.Request)) string {
		t.Helper()
		rec := post(e, "/present", body, auth)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", name, rec.Code)
		}
		return rec.Header().Get(echo.HeaderWWWAuthenticate) + "\n" + rec.Body.String()
	}

	mapped := `{"fqdn":"` + testFQDN + `.","value":"gfj9Xq"}`
	unmapped := `{"fqdn":"_acme-challenge.nobody.example.org.","value":"gfj9Xq"}`

	none := answer("no credentials, mapped name", mapped, nil)
	noneUnmapped := answer("no credentials, unmapped name", unmapped, nil)
	if none != noneUnmapped {
		t.Errorf("mapped and unmapped names answered differently:\n%q\n%q", none, noneUnmapped)
	}

	wrong := answer("wrong secret", mapped, func(r *http.Request) { r.SetBasicAuth(k.Identifier, "nope") })
	if wrong != none {
		t.Errorf("wrong secret and no credentials answered differently:\n%q\n%q", wrong, none)
	}
	unknown := answer("unknown identifier", mapped, func(r *http.Request) { r.SetBasicAuth("k_00000000", "nope") })
	if unknown != none {
		t.Errorf("unknown identifier and no credentials answered differently:\n%q\n%q", unknown, none)
	}

	// A malformed body must not be diagnosed before the caller is authenticated
	// either — 401 wins over 400.
	if rec := post(e, "/present", `{`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("malformed body without credentials: status %d, want 401", rec.Code)
	}
}

func TestRegisterChallengeRejectsIncompleteOptions(t *testing.T) {
	full := ChallengeOptions{
		DB:       &gorm.DB{},
		Zone:     testZone,
		View:     zoneview.New(func(context.Context) (map[string][]store.ChallengeValue, error) { return nil, nil }),
		Lookup:   func(context.Context, string) (*model.APIKey, error) { return nil, gorm.ErrRecordNotFound },
		Lifetime: time.Hour,

		TrustedProxyCIDRs: []string{"192.0.2.1/32"},
	}
	cases := map[string]func(*ChallengeOptions){
		"no database":    func(o *ChallengeOptions) { o.DB = nil },
		"no zone view":   func(o *ChallengeOptions) { o.View = nil },
		"no lookup":      func(o *ChallengeOptions) { o.Lookup = nil },
		"zero lifetime":  func(o *ChallengeOptions) { o.Lifetime = 0 },
		"no zone":        func(o *ChallengeOptions) { o.Zone = "" },
		"bad zone":       func(o *ChallengeOptions) { o.Zone = "not a zone" },
		"bad proxy cidr": func(o *ChallengeOptions) { o.TrustedProxyCIDRs = []string{"not-a-cidr"} },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			o := full
			break_(&o)
			if err := RegisterChallenge(context.Background(), echo.New(), o); err == nil {
				t.Fatal("want an error, got nil")
			}
		})
	}
}

// --- database-backed behaviour ---------------------------------------------

// migratedDB creates a uniquely named schema in TEST_DATABASE_URL, migrates it
// and returns a connected handle. It duplicates internal/store's unexported
// helper of the same shape, as internal/sweeper does, rather than exporting a
// test helper from a non-test package.
func migratedDB(t *testing.T) *gorm.DB {
	t.Helper()

	base := os.Getenv(testDatabaseURLEnv)
	if base == "" {
		t.Skipf("skipping: %s is not set — this test needs a live PostgreSQL, e.g. "+
			`%s="postgres://user:pass@localhost:5432/acmeservice_test?sslmode=disable"`,
			testDatabaseURLEnv, testDatabaseURLEnv)
	}

	var buf [6]byte
	rand.Read(buf[:])
	name := "test_" + hex.EncodeToString(buf[:])

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open %s: %v", testDatabaseURLEnv, err)
	}
	defer admin.Close()
	if _, err := admin.Exec("create schema " + name); err != nil {
		t.Fatalf("create schema %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", base)
		if err != nil {
			t.Logf("drop schema %s: %v", name, err)
			return
		}
		defer cleanup.Close()
		if _, err := cleanup.Exec("drop schema " + name + " cascade"); err != nil {
			t.Logf("drop schema %s: %v", name, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", testDatabaseURLEnv, err)
	}
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	dsn := u.String()

	if err := store.MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := store.Open(store.Options{DatabaseURL: dsn, Logger: discard()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// env is a migrated database holding one project, two load balancers, one alias owned
// by lb "a", and one API key per load balancer — plus the routes wired over them.
type env struct {
	t       *testing.T
	db      *gorm.DB
	view    *zoneview.View
	e       *echo.Echo
	alias   *model.Alias
	domain  *model.Domain
	metrics *obs.Metrics

	ours, theirs func(*http.Request) // basic-auth setters
}

// challengeAccount creates the account these fixtures act in.
//
// It used to be store.ScopeForBootstrap over the account the migration seeded;
// design C10 removed both, so the fixture makes its own. The Valid check is
// there because row-level security is on and a zero Scope reads no rows and
// raises no error — a fixture that produced one would leave every assertion in
// this file passing against nothing.
func challengeAccount(t *testing.T, db *gorm.DB) store.Scope {
	t.Helper()
	ctx := t.Context()
	owner, err := store.FindOrCreateUser(ctx, db, "challenge-fixture-owner", "Challenge Fixture", "")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, "challenge-fixture", "Challenge Fixture", owner.ID, 25)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	sc, err := store.ScopeForSelection(ctx, db, acc.ID)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if !sc.Valid() {
		t.Fatal("fixture scope is the zero Scope; every read below would silently match nothing")
	}
	return sc
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := migratedDB(t)
	ctx := t.Context()
	sc := challengeAccount(t, db)

	p := &model.Project{Slug: "acme", Name: "Acme"}
	if err := store.CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb := func(slug string) *model.LoadBalancer {
		lb := &model.LoadBalancer{ProjectID: p.ID, Slug: slug, Name: slug}
		if err := store.CreateLoadBalancer(ctx, db, sc, lb); err != nil {
			t.Fatalf("create load balancer %s: %v", slug, err)
		}
		return lb
	}
	mine, other := lb("a"), lb("b")

	alias := &model.Alias{LoadBalancerID: mine.ID, ChallengeFQDN: testFQDN, Label: testLabel}
	if err := store.CreateAlias(ctx, db, sc, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	// The alias's domain, ACTIVATED — the state every domain in a working
	// deployment is in (domain-activation A1). Without it every /present below
	// is refused 409, which is correct behaviour and not what those tests are
	// about; the fixture used to hold an alias with no domain at all, which is
	// a shape the inventory service never creates.
	domain := &model.Domain{LoadBalancerID: mine.ID, AliasID: alias.ID, Name: testDomain}
	if err := store.CreateDomain(ctx, db, sc, domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	if _, err := store.RecordDelegationSeen(ctx, db, alias.ID, time.Now().UTC()); err != nil {
		t.Fatalf("activate domain: %v", err)
	}

	auth := func(loadBalancerID model.ID) func(*http.Request) {
		k, secret, err := apikey.New()
		if err != nil {
			t.Fatalf("new api key: %v", err)
		}
		k.Grants = []model.APIKeyGrant{{ObjectType: "load_balancer", ObjectID: &loadBalancerID, Action: "challenge.publish", Inheritable: true}, {ObjectType: "load_balancer", ObjectID: &loadBalancerID, Action: "challenge.withdraw", Inheritable: true}}
		if err := store.CreateAPIKey(ctx, db, sc, k, store.AllowAnyGrant); err != nil {
			t.Fatalf("create api key: %v", err)
		}
		return func(r *http.Request) { r.SetBasicAuth(k.Identifier, secret) }
	}

	view := zoneview.New(zoneview.FromDB(db))
	metrics := obs.New()
	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	err := RegisterChallenge(ctx, e, ChallengeOptions{
		DB:       db,
		Zone:     testZone,
		View:     view,
		Lookup:   dbLookup(db),
		Lifetime: time.Hour,
		Metrics:  metrics,
		Log:      discard(),
	})
	if err != nil {
		t.Fatalf("RegisterChallenge: %v", err)
	}
	return &env{t: t, db: db, view: view, e: e, alias: alias, domain: domain, metrics: metrics,
		ours: auth(mine.ID), theirs: auth(other.ID)}
}

// call posts fqdn/value to path as the load balancer-"a" key and asserts the status.
func (v *env) call(path, fqdn, value string, want int) {
	v.t.Helper()
	v.callAs(v.ours, path, fqdn, value, want)
}

func (v *env) callAs(auth func(*http.Request), path, fqdn, value string, want int) {
	v.t.Helper()
	body := `{"fqdn":` + quote(fqdn) + `,"value":` + quote(value) + `}`
	rec := post(v.e, path, body, auth)
	if rec.Code != want {
		v.t.Fatalf("POST %s %s: status %d, want %d (body %s)", path, body, rec.Code, want, rec.Body.String())
	}
}

func quote(s string) string { return `"` + s + `"` }

// answers is what the zone view — the thing the DNS server reads — currently
// serves at the alias's label, sorted for comparison.
func (v *env) answers() []string {
	v.t.Helper()
	vals, ok := v.view.Lookup(testLabel)
	if !ok {
		v.t.Fatalf("label %q is absent from the zone view; it must exist even with no values", testLabel)
	}
	return vals
}

func (v *env) assertAnswers(want ...string) {
	v.t.Helper()
	got := v.answers()
	if len(got) != len(want) {
		v.t.Fatalf("zone view answers %v, want %v", got, want)
	}
	for _, w := range want {
		if !contains(got, w) {
			v.t.Fatalf("zone view answers %v, want it to contain %q", got, w)
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func (v *env) rows() int64 {
	v.t.Helper()
	var n int64
	if err := v.db.Model(&model.Challenge{}).Count(&n).Error; err != nil {
		v.t.Fatalf("count challenges: %v", err)
	}
	return n
}

// The value must be answerable by the time /present returns — not after the next
// timed refresh. Nothing here calls Refresh; the handler is required to have
// done it before writing the response.
func TestPresentIsAnswerableWhenTheResponseArrives(t *testing.T) {
	v := newEnv(t)
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.assertAnswers("gfj9Xq")
}

func TestPresentNormalizesTrailingDotAndCase(t *testing.T) {
	v := newEnv(t)
	for _, spelling := range []string{
		testFQDN,
		testFQDN + ".",
		"_ACME-Challenge.Shop.Example.com",
		"_ACME-CHALLENGE.SHOP.EXAMPLE.COM.",
	} {
		v.call("/present", spelling, "gfj9Xq", http.StatusOK)
	}
	// All four spellings resolved to the same alias and the same value, so the
	// upsert left exactly one row behind.
	if n := v.rows(); n != 1 {
		t.Fatalf("%d challenge rows after four spellings of one name, want 1", n)
	}
	v.assertAnswers("gfj9Xq")

	// And cleanup resolves the same alias from a differently spelled name.
	v.call("/cleanup", "_ACME-Challenge.Shop.Example.com.", "gfj9Xq", http.StatusOK)
	v.assertAnswers()
}

func TestRepeatedPresentIsIdempotent(t *testing.T) {
	v := newEnv(t)
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	if n := v.rows(); n != 1 {
		t.Fatalf("%d challenge rows after presenting the same value twice, want 1", n)
	}
	v.assertAnswers("gfj9Xq")
}

// A certificate covering example.com and *.example.com validates two
// authorizations at ONE challenge name, and the CA expects both TXT values to be
// there at once. Present adds; it never displaces.
func TestTwoValuesCoexistAndCleanupRemovesOnlyOne(t *testing.T) {
	v := newEnv(t)
	v.call("/present", testFQDN+".", "first", http.StatusOK)
	v.call("/present", testFQDN+".", "second", http.StatusOK)
	v.assertAnswers("first", "second")

	v.call("/cleanup", testFQDN+".", "first", http.StatusOK)
	v.assertAnswers("second")

	// Cleaning up a value that is not there is not an error, and touches
	// nothing else.
	v.call("/cleanup", testFQDN+".", "never-published", http.StatusOK)
	v.assertAnswers("second")

	v.call("/cleanup", testFQDN+".", "second", http.StatusOK)
	v.assertAnswers()
	// The label still exists with no values: NOERROR-empty, not NXDOMAIN.
	if !v.view.Has(testLabel) {
		t.Fatal("label vanished from the zone view after its last value was withdrawn")
	}
}

func TestUnmappedAndCrossLoadBalancerNamesAreRefused(t *testing.T) {
	v := newEnv(t)

	// No alias is mapped to this name at all.
	v.call("/present", "_acme-challenge.nobody.example.org.", "gfj9Xq", http.StatusForbidden)
	v.call("/cleanup", "_acme-challenge.nobody.example.org.", "gfj9Xq", http.StatusForbidden)

	// The alias exists, but belongs to the other load balancer.
	v.callAs(v.theirs, "/present", testFQDN+".", "gfj9Xq", http.StatusForbidden)
	v.callAs(v.theirs, "/cleanup", testFQDN+".", "gfj9Xq", http.StatusForbidden)

	if n := v.rows(); n != 0 {
		t.Fatalf("%d challenge rows after refused requests, want 0", n)
	}

	// Both refusals must be indistinguishable to the caller.
	unmapped := post(v.e, "/present", `{"fqdn":"_acme-challenge.nobody.example.org.","value":"x"}`, v.ours)
	crossLoadBalancer := post(v.e, "/present", `{"fqdn":"`+testFQDN+`.","value":"x"}`, v.theirs)
	if unmapped.Body.String() != crossLoadBalancer.Body.String() {
		t.Errorf("unmapped and cross-lb refusals differ:\n%q\n%q", unmapped.Body, crossLoadBalancer.Body)
	}
}

// A malformed or unusable name from an authenticated caller is a 400, and stores
// nothing. Both endpoints validate identically.
func TestBadBodiesAreRejectedAfterAuthentication(t *testing.T) {
	v := newEnv(t)
	bodies := []string{
		`{`,
		``,
		`{"fqdn":"","value":"v"}`,
		`{"fqdn":"` + testFQDN + `","value":""}`,
		`{"domain":"shop.example.com","token":"t","keyAuth":"k"}`,
		`{"fqdn":"not a dns name","value":"v"}`,
		`{"fqdn":".","value":"v"}`,
	}
	for _, path := range []string{"/present", "/cleanup"} {
		for _, body := range bodies {
			rec := post(v.e, path, body, v.ours)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s %s: status %d, want 400", path, body, rec.Code)
			}
		}
	}
	if n := v.rows(); n != 0 {
		t.Fatalf("%d challenge rows after rejected bodies, want 0", n)
	}
}

// A write that did not happen must never be answered 200: any non-200 makes lego
// retry, which is the behaviour we want.
func TestStorageFailureIsReportedAsFailure(t *testing.T) {
	v := newEnv(t)
	// Break the write, not the connection: authentication and the alias lookup
	// still succeed, so the request reaches the point of storing something and
	// fails there — which is the path that must never answer 200.
	if err := v.db.Exec("drop table challenges").Error; err != nil {
		t.Fatalf("drop challenges: %v", err)
	}

	for _, path := range []string{"/present", "/cleanup"} {
		rec := post(v.e, path, `{"fqdn":"`+testFQDN+`.","value":"gfj9Xq"}`, v.ours)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("POST %s with a dead pool: status %d, want 500", path, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "challenges") || strings.Contains(body, "sql") {
			t.Errorf("500 body leaks datastore detail: %s", body)
		}
	}
	if vals, ok := v.view.Lookup(testLabel); ok && len(vals) != 0 {
		t.Fatalf("zone view answers %v after a failed write, want nothing", vals)
	}
}

// The fail-closed startup guard: a key pinned to source addresses while no proxy
// is trusted would be judged on a forgeable header. RegisterChallenge must refuse
// to wire the routes at all, so a caller assembling the listener cannot skip it.
func TestRegisterChallengeRefusesUnprotectedCIDRRestriction(t *testing.T) {
	db := migratedDB(t)
	ctx := t.Context()
	sc := challengeAccount(t, db)

	p := &model.Project{Slug: "acme", Name: "Acme"}
	if err := store.CreateProject(ctx, db, sc, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	lb := &model.LoadBalancer{ProjectID: p.ID, Slug: "a", Name: "a"}
	if err := store.CreateLoadBalancer(ctx, db, sc, lb); err != nil {
		t.Fatalf("create load balancer: %v", err)
	}
	k, _, err := apikey.New()
	if err != nil {
		t.Fatal(err)
	}
	k.Grants = []model.APIKeyGrant{{ObjectType: "load_balancer", ObjectID: &lb.ID, Action: "challenge.publish", Inheritable: true}, {ObjectType: "load_balancer", ObjectID: &lb.ID, Action: "challenge.withdraw", Inheritable: true}}
	k.Networks = []model.APIKeyNetwork{{CIDR: "203.0.113.0/24"}}
	if err := store.CreateAPIKey(ctx, db, sc, k, store.AllowAnyGrant); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	opts := ChallengeOptions{
		DB:       db,
		Zone:     testZone,
		View:     zoneview.New(zoneview.FromDB(db)),
		Lookup:   dbLookup(db),
		Lifetime: time.Hour,
		Log:      discard(),
	}
	if err := RegisterChallenge(ctx, echo.New(), opts); err == nil {
		t.Fatal("routes were wired with a CIDR-restricted key and no trusted proxies")
	}

	opts.TrustedProxyCIDRs = []string{"192.0.2.1/32"}
	if err := RegisterChallenge(ctx, echo.New(), opts); err != nil {
		t.Fatalf("with a trusted proxy configured: %v", err)
	}
}

// --- both forms of the submitted name ---------------------------------------

// The name lego sends is OUR alias name whenever it followed the customer's
// CNAME, which is the normal production case and was missing from the original
// premise. The customer's own challenge name arrives from clients that leave the
// aliasing to us. Both must resolve the same alias, over both endpoints.
func TestBothNameFormsResolveTheSameAlias(t *testing.T) {
	v := newEnv(t)

	// Presented as the alias name, withdrawn as the challenge name.
	v.call("/present", testAliasName+".", "gfj9Xq", http.StatusOK)
	v.assertAnswers("gfj9Xq")
	v.call("/cleanup", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.assertAnswers()

	// And the other way round.
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.assertAnswers("gfj9Xq")
	v.call("/cleanup", testAliasName+".", "gfj9Xq", http.StatusOK)
	v.assertAnswers()
}

// Case and trailing dot are stripped from the alias form exactly as from the
// challenge form, and every spelling upserts onto the one row.
func TestAliasFormNormalizesIdentically(t *testing.T) {
	v := newEnv(t)
	for _, spelling := range []string{
		testAliasName,
		testAliasName + ".",
		strings.ToUpper(testAliasName),
		strings.ToUpper(testAliasName) + ".",
		testFQDN + ".", // the other form, same alias
	} {
		v.call("/present", spelling, "gfj9Xq", http.StatusOK)
	}
	if n := v.rows(); n != 1 {
		t.Fatalf("%d challenge rows after five spellings across both forms, want 1", n)
	}
	v.assertAnswers("gfj9Xq")
}

// Authorization is the same check on both paths: an alias reached by its own
// name is still not writable by a key scoped to another load balancer.
func TestAliasFormIsAuthorizedTheSameWay(t *testing.T) {
	v := newEnv(t)
	v.callAs(v.theirs, "/present", testAliasName+".", "gfj9Xq", http.StatusForbidden)
	v.callAs(v.theirs, "/cleanup", testAliasName+".", "gfj9Xq", http.StatusForbidden)

	// An in-zone name that is not an alias label we issued, and an in-zone name
	// with a label too many, are both simply unmapped.
	v.call("/present", "no-such-label."+testZone+".", "gfj9Xq", http.StatusForbidden)
	v.call("/present", "deeper."+testAliasName+".", "gfj9Xq", http.StatusForbidden)
	// The zone apex itself is not an alias either.
	v.call("/present", testZone+".", "gfj9Xq", http.StatusForbidden)

	if n := v.rows(); n != 0 {
		t.Fatalf("%d challenge rows after refused requests, want 0", n)
	}
}

// Which form arrived is recorded, because it says which convention a load balancer's
// client follows. It is NOT a fault signal — neither form logs or answers
// differently — so the record is a plain counter with two fixed label values.
func TestSubmittedNameFormIsCounted(t *testing.T) {
	v := newEnv(t)
	v.call("/present", testAliasName+".", "gfj9Xq", http.StatusOK)
	v.call("/present", testFQDN+".", "second", http.StatusOK)
	v.call("/present", testFQDN+".", "third", http.StatusOK)

	if got := v.formCount("alias"); got != 1 {
		t.Errorf(`fqdn_form="alias" counted %v, want 1`, got)
	}
	if got := v.formCount("challenge"); got != 2 {
		t.Errorf(`fqdn_form="challenge" counted %v, want 2`, got)
	}

	// A refused request is counted too: the form is a property of the request,
	// not of its outcome.
	v.call("/present", "no-such-label."+testZone+".", "gfj9Xq", http.StatusForbidden)
	if got := v.formCount("alias"); got != 2 {
		t.Errorf(`fqdn_form="alias" counted %v after a refusal, want 2`, got)
	}
}

// formCount reads one series of the fqdn_form counter off the registry.
func (v *env) formCount(form string) float64 {
	v.t.Helper()
	families, err := v.metrics.Registry.Gather()
	if err != nil {
		v.t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "acmeservice_challenge_name_forms_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "fqdn_form" && l.GetValue() == form {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// --- the interaction history, as the challenge path writes it ---------------

// interactions returns the history rows, oldest first.
func (v *env) interactions() []model.ChallengeInteraction {
	v.t.Helper()
	var out []model.ChallengeInteraction
	if err := v.db.Order("id").Find(&out).Error; err != nil {
		v.t.Fatalf("read interactions: %v", err)
	}
	return out
}

// lego retries a /present, and a retry of a value that is still live is the
// same interaction continuing — not a second one. The publication time must not
// move either: the interaction began when the CA first asked for it, and a
// published_at that follows the last retry makes every duration on the console
// screen wrong.
func TestRetriedPresentDoesNotOpenASecondInteraction(t *testing.T) {
	v := newEnv(t)

	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	first := v.interactions()
	if len(first) != 1 {
		t.Fatalf("%d interactions after one /present, want 1", len(first))
	}

	// Both spellings of the name, so the reuse is keyed on the resolved alias
	// rather than on the string the client happened to send.
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.call("/present", testAliasName+".", "gfj9Xq", http.StatusOK)

	got := v.interactions()
	if len(got) != 1 {
		t.Fatalf("%d interactions after two retries of a live value, want 1", len(got))
	}
	if !got[0].PublishedAt.Equal(first[0].PublishedAt) {
		t.Errorf("publishedAt moved from %s to %s on a retry", first[0].PublishedAt, got[0].PublishedAt)
	}
	if got[0].WithdrawnAt != nil {
		t.Errorf("the interaction is closed while the value is still published: %s", got[0].WithdrawnAt)
	}

	// A second, different value at the same name IS a second authorization —
	// the apex-and-wildcard pair — and does open its own row.
	v.call("/present", testFQDN+".", "second", http.StatusOK)
	if got := v.interactions(); len(got) != 2 {
		t.Fatalf("%d interactions after a second distinct value, want 2", len(got))
	}
}

// A /cleanup for a value with no interaction behind it — a retried withdrawal,
// or one whose history row was never written because the history is best effort
// — succeeds, and invents nothing. A cleanup that answered anything but 200
// would leave a client retrying a withdrawal that already happened.
func TestCleanupWithoutAMatchingInteractionSucceedsAndCreatesNothing(t *testing.T) {
	v := newEnv(t)

	v.call("/cleanup", testFQDN+".", "never-published", http.StatusOK)
	if got := v.interactions(); len(got) != 0 {
		t.Fatalf("a cleanup with nothing to close created %d interactions: %+v", len(got), got)
	}

	// And a repeated cleanup of a value that WAS published closes the row once
	// and then does nothing further.
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.call("/cleanup", testFQDN+".", "gfj9Xq", http.StatusOK)
	closed := v.interactions()
	if len(closed) != 1 || closed[0].WithdrawnAt == nil {
		t.Fatalf("after present+cleanup: %+v, want one closed interaction", closed)
	}
	withdrawn := *closed[0].WithdrawnAt

	v.call("/cleanup", testFQDN+".", "gfj9Xq", http.StatusOK)
	again := v.interactions()
	if len(again) != 1 {
		t.Fatalf("%d interactions after a repeated cleanup, want 1", len(again))
	}
	if !again[0].WithdrawnAt.Equal(withdrawn) {
		t.Errorf("withdrawnAt moved from %s to %s on a repeated cleanup", withdrawn, again[0].WithdrawnAt)
	}
}

// The deliberate asymmetry: the certificate is what these endpoints are for,
// the record of it is not. A history write that fails must therefore fail
// NEITHER /present nor /cleanup — a 500 here would make lego retry a challenge
// that is already published and answerable.
//
// The failure is injected rather than described: the history table is dropped,
// so the real error path a full disk or a revoked grant would take is the one
// exercised. Compare TestStorageFailureIsReportedAsFailure, where dropping the
// table the CHALLENGE lives in must produce a 500 — that is the same injection
// on the write that does matter.
func TestHistoryWriteFailureFailsNeitherPresentNorCleanup(t *testing.T) {
	v := newEnv(t)
	if err := v.db.Exec("drop table challenge_interactions").Error; err != nil {
		t.Fatalf("drop challenge_interactions: %v", err)
	}

	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)
	// The part that matters still happened: the value is answerable.
	v.assertAnswers("gfj9Xq")

	v.call("/cleanup", testFQDN+".", "gfj9Xq", http.StatusOK)
	v.assertAnswers()
	if n := v.rows(); n != 0 {
		t.Fatalf("%d challenge rows after the withdrawal, want 0", n)
	}
}

// deactivate returns the fixture's domain to the state a domain is in the
// moment it is added: never checked, never seen.
//
// It clears last_success_at directly, which is deliberately something no
// production code path does — activation is one-way (domain-activation A3) and
// the only way back to offline is a row that was never activated. A helper is
// the honest way to reach that state, and a store function for it would be a
// loaded gun left lying in the package.
func (v *env) deactivate() {
	v.t.Helper()
	err := store.CrossAccount(v.t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Exec(`update domains set last_checked_at = null, last_success_at = null where id = ?`,
			v.domain.ID).Error
	})
	if err != nil {
		v.t.Fatalf("deactivate domain: %v", err)
	}
}

// The gate (domain-activation tasks 3.1–3.4).
//
// Four things at once, because they are one story and separating them would
// invite each half to be "fixed" without the other: the refusal happens, it
// publishes nothing, it says which record to publish, and once the delegation
// has been seen no number of failed re-checks takes the domain offline again.
func TestIntegrationChallengeIsRefusedUntilTheDelegationHasBeenSeen(t *testing.T) {
	v := newEnv(t)
	v.deactivate()

	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusConflict)
	if n := v.rows(); n != 0 {
		t.Fatalf("%d challenge rows after a refused publish, want 0 — the gate must decide before anything is written", n)
	}

	// The message lands in the log of the machine running lego, whose operator
	// is the one person who can fix this. It has to carry both halves of the
	// record: the name to publish it under and the target it points at.
	rec := post(v.e, "/present", `{"fqdn":"`+testFQDN+`.","value":"gfj9Xq"}`, v.ours)
	for _, want := range []string{testFQDN, testAliasName} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the 409 does not name %q: %s", want, rec.Body.String())
		}
	}

	// A withdrawal is not gated. Refusing it would leave a value we are already
	// holding answerable until it expired — the gate making the storage it
	// exists to bound last longer.
	v.call("/cleanup", testFQDN+".", "gfj9Xq", http.StatusOK)

	// Seen once: answerable.
	if _, err := store.RecordDelegationSeen(t.Context(), v.db, v.alias.ID, time.Now().UTC()); err != nil {
		t.Fatalf("activate: %v", err)
	}
	v.call("/present", testFQDN+".", "gfj9Xq", http.StatusOK)

	// And it STAYS answerable through a delegation that has stopped resolving,
	// however long for (A3). A customer migrating DNS providers must not have
	// their renewals stopped by a blip nobody would notice for weeks.
	for i := range 3 {
		at := time.Now().UTC().Add(time.Duration(i) * time.Hour)
		if _, err := store.RecordDelegationMissing(t.Context(), v.db, v.alias.ID, at); err != nil {
			t.Fatalf("record missing: %v", err)
		}
		v.call("/present", testFQDN+".", "still-answerable", http.StatusOK)
	}
}
