package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/apikey"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsname"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/inventory"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/oidcauth"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// The role tests below call every route directly, without the console. That is
// the point of them: the Angular guard hides controls, it does not enforce
// anything, so what is asserted here is that curl gets the same answer.

// --- test doubles ----------------------------------------------------------

// testAuth stands in for *oidcauth.Service on the guard. It resolves a real
// session row named by a test header, applying the same revoked/expired rules
// oidcauth.Authenticate does — which is what makes the revocation test
// meaningful rather than a test of a map.
type testAuth struct{ db *gorm.DB }

func (t *testAuth) Authenticate(ctx context.Context, r *http.Request) (*model.Session, error) {
	id := r.Header.Get("X-Test-Session")
	if id == "" {
		return nil, oidcauth.ErrNoSession
	}
	s, err := store.FindSession(ctx, t.db, id)
	if err != nil {
		return nil, oidcauth.ErrSessionInvalid
	}
	if s.RevokedAt != nil || !time.Now().Before(s.ExpiresAt) {
		return nil, oidcauth.ErrSessionInvalid
	}
	return s, nil
}

// testConsoleAuth supplies the sign-in routes and the CSRF middleware. The
// CSRF middleware is the real one, configured with oidcauth's names, so the
// mount point is exercised rather than stubbed out.
type testConsoleAuth struct{}

func (testConsoleAuth) Login(w http.ResponseWriter, r *http.Request) error {
	http.Redirect(w, r, "https://idp.example/authorize", http.StatusFound)
	return nil
}
func (testConsoleAuth) Callback(http.ResponseWriter, *http.Request) error { return nil }
func (testConsoleAuth) Logout(context.Context, http.ResponseWriter, *http.Request) (string, error) {
	return "", nil
}
func (testConsoleAuth) CSRF() echo.MiddlewareFunc {
	return middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:  "header:" + oidcauth.CSRFHeaderName,
		CookieName:   oidcauth.CSRFCookieName,
		CookiePath:   "/",
		CookieSecure: false,
	})
}

// --- fixture ---------------------------------------------------------------

// The four roles a new team account is seeded with (store.DefaultRoles). They
// used to be reached through four ACME_*_GROUP variables and a group claim;
// they are membership rows now, which is what adminEnv.session writes.
const (
	roleAdmin  = "ADMIN"
	roleConfig = "CONFIG_ADMIN"
	roleKey    = "KEY_ROTATION"
	roleRead   = "READ_ONLY"
)

type adminEnv struct {
	t      *testing.T
	db     *gorm.DB
	e      *echo.Echo
	projID model.ID
	roleID model.ID
	sc     store.Scope
	opts   AdminOptions
	// mail is set only by newAdminEnvWithMail. The invitation tests read the
	// token out of the MESSAGE rather than out of the table, which is the only
	// way to assert that what goes on the wire actually works — an invitation
	// that is perfectly correct in the database and unusable in the inbox is a
	// failure nothing else here would catch.
	mail *captureMail
}

// newAdminEnvWithMail is newAdminEnv with a sender that keeps what it was given.
func newAdminEnvWithMail(t *testing.T) *adminEnv {
	t.Helper()
	v := newAdminEnv(t)
	v.mail = &captureMail{}
	v.opts.Mail = v.mail
	// The console origin the invitation link is built from. Derived from the
	// OIDC redirect URL in production, because that is where the provider sends
	// a browser after sign-in and its origin therefore IS the console.
	v.opts.Config.OIDCRedirectURL = "https://console.example/api/v1/auth/callback"
	v.reregister(t)
	return v
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	db := migratedDB(t)
	ctx := t.Context()
	// The account is created rather than found. store.ScopeForBootstrap used to
	// hand back the one the migration seeded; design C10 removed both, so the
	// fixture goes through the production path — which also seeds the four
	// default roles the sessions below are given membership of.
	owner, err := store.FindOrCreateUser(ctx, db, "fixture-owner", "Fixture Owner", "")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	acc, err := store.CreateAccountWithOwner(ctx, db, "acme-fixture", "Acme Fixture", owner.ID, 25)
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

	p, err := inventory.CreateProject(ctx, db, sc, authz.CLISubject(sc.AccountID()), inventory.Limits{}, "acme", "Acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := inventory.AddNode(ctx, db, sc, authz.CLISubject(sc.AccountID()), p.ID, "a", "203.0.113.10", "2001:db8:1::10"); err != nil {
		t.Fatalf("add node: %v", err)
	}

	cfg := config.Config{
		Zone:               "acmeservice.example.",
		CAAIssuers:         []string{"letsencrypt.org"},
		SessionIdleTimeout: time.Hour,
		SessionMaxLifetime: 8 * time.Hour,
		OIDCClientSecret:   "super-secret-value",
		DatabaseURL:        "postgres://user:hunter2@db:5432/acme",
	}

	opts := AdminOptions{
		DB:     db,
		Guard:  &authz.Guard{Auth: &testAuth{db: db}, Log: discard()},
		Auth:   testConsoleAuth{},
		Config: cfg,
		Log:    discard(),
	}
	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	if err := RegisterAdmin(e, opts); err != nil {
		t.Fatalf("RegisterAdmin: %v", err)
	}
	// The console's history fallback, as group 16 mounts it. Registering it
	// here is what makes the "unknown API paths stay JSON" test a test of the
	// real arrangement rather than of an empty router.
	e.GET("/*", func(c echo.Context) error { return c.HTML(http.StatusOK, "<html>console</html>") })

	// A spare role for the membership rows of the route table: adding a member
	// needs one to add them to, and the unrestricted role is the wrong choice
	// because its last-member guard would make the row's outcome depend on how
	// many other rows ran first.
	spare := &model.Role{Name: "route-table-spare"}
	if err := store.CreateRole(t.Context(), db, sc, spare, store.AllowAnyGrant); err != nil {
		t.Fatalf("create spare role: %v", err)
	}
	return &adminEnv{t: t, db: db, sc: sc, e: e, projID: p.ID, roleID: spare.ID, opts: opts}
}

// Seeders for the destructive rows of TestEveryRouteEnforcesItsRole. Each
// creates one throwaway object and returns the path addressing it, so every
// role's attempt acts on something that exists and belongs to the account —
// which is what makes a refusal mean "you may not" rather than "there is no
// such thing".
//
// They go through the service layer with an unrestricted subject, which is the
// same authority the CLI has: seeding is setup, not part of what is under test.

func seedProject(t *testing.T, v *adminEnv, suffix string) string {
	t.Helper()
	p, err := inventory.CreateProject(t.Context(), v.db, v.sc, authz.CLISubject(v.sc.AccountID()), inventory.Limits{},
		fmt.Sprintf("seed-p%d", seedCounter.Add(1)), "Seed "+suffix)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + p.ID.String()
}

// seedCounter keeps seeded labels and slugs distinct. A node label is unique
// per project, so deriving one from the role name collides the moment two
// rows seed for the same role.
var seedCounter atomic.Int64

func seedNode(t *testing.T, v *adminEnv, suffix string) string {
	t.Helper()
	subj := authz.CLISubject(v.sc.AccountID())
	n, err := inventory.AddNode(t.Context(), v.db, v.sc, subj, v.projID,
		fmt.Sprintf("n%d", seedCounter.Add(1)), "203.0.113.200", "")
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}
	return "/api/v1/accounts/" + v.sc.AccountID().String() + "/nodes/" + n.ID.String()
}

func seedDomain(t *testing.T, v *adminEnv, suffix string) string {
	t.Helper()
	subj := authz.CLISubject(v.sc.AccountID())
	res, err := inventory.CreateDomain(t.Context(), v.db, v.sc, subj, v.projID,
		fmt.Sprintf("seed-d%d.example.com", seedCounter.Add(1)), false, false)
	if err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	return "/api/v1/accounts/" + v.sc.AccountID().String() + "/domains/" + res.Domains[0].ID.String()
}

// seedDomainPath is seedDomain plus a tail, for the routes that hang off a
// domain rather than addressing it.
func seedDomainPath(tail string) func(*testing.T, *adminEnv, string) string {
	return func(t *testing.T, v *adminEnv, suffix string) string {
		t.Helper()
		return seedDomain(t, v, suffix) + tail
	}
}

// seedKeyPath returns a seeder for a key route, since the key rows differ only
// by what follows the identifier.
func seedKeyPath(tail string) func(*testing.T, *adminEnv, string) string {
	return func(t *testing.T, v *adminEnv, suffix string) string {
		t.Helper()
		k, _, err := apikey.New()
		if err != nil {
			t.Fatalf("seed key: %v", err)
		}
		k.Kind = model.APIKeyKindProject
		k.ProjectID = &v.projID
		if err := store.CreateAPIKey(t.Context(), v.db, v.sc, k, store.AllowAnyGrant); err != nil {
			t.Fatalf("seed key: %v", err)
		}
		return "/api/v1/accounts/" + v.sc.AccountID().String() + "/keys/" + k.ID.String() + tail
	}
}

func seedRole(t *testing.T, v *adminEnv, suffix string) string {
	t.Helper()
	r := &model.Role{Name: fmt.Sprintf("seed-role%d", seedCounter.Add(1))}
	if err := store.CreateRole(t.Context(), v.db, v.sc, r, store.AllowAnyGrant); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	return "/api/v1/accounts/" + v.sc.AccountID().String() + "/roles/" + r.ID.String()
}

func seedMember(t *testing.T, v *adminEnv, suffix string) string {
	t.Helper()
	r := &model.Role{Name: fmt.Sprintf("seed-mrole%d", seedCounter.Add(1))}
	if err := store.CreateRole(t.Context(), v.db, v.sc, r, store.AllowAnyGrant); err != nil {
		t.Fatalf("seed role for member: %v", err)
	}
	u, err := store.FindOrCreateUser(t.Context(), v.db, fmt.Sprintf("seed-user%d", seedCounter.Add(1)), "Seed", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := store.AddMember(t.Context(), v.db, v.sc, u.ID, r.ID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	ms, err := store.ListMembers(t.Context(), v.db, v.sc)
	if err != nil {
		t.Fatalf("read members: %v", err)
	}
	for _, m := range ms {
		if m.UserID == u.ID {
			return "/api/v1/accounts/" + v.sc.AccountID().String() + "/members/" + m.ID.String()
		}
	}
	t.Fatal("seeded member not found")
	return ""
}

// seedMemberRolePath seeds a member and returns the path that would assign them
// a role.
func seedMemberRolePath(t *testing.T, v *adminEnv, suffix string) string {
	t.Helper()
	return seedMember(t, v, suffix) + "/role"
}

// seedInvitation returns a seeder for an invitation route, since the rows differ
// only by what follows the identifier.
//
// Each call issues a fresh invitation to a fresh address, because at most one
// may be outstanding per address per account — a seeder that reused one would
// make the second role's attempt fail on the pending index rather than on
// permission, and pass for the wrong reason.
func seedInvitation(tail string) func(*testing.T, *adminEnv, string) string {
	return func(t *testing.T, v *adminEnv, suffix string) string {
		t.Helper()
		email := fmt.Sprintf("seed-invite%d@corp.example", seedCounter.Add(1))
		inv, _, err := store.IssueInvitation(t.Context(), v.db, v.sc, email,
			accountOwnerID(t, v), time.Now().UTC())
		if err != nil {
			t.Fatalf("seed invitation: %v", err)
		}
		return "/api/v1/accounts/" + v.sc.AccountID().String() + "/invitations/" + inv.ID.String() + tail
	}
}

func accountOwnerID(t *testing.T, v *adminEnv) model.ID {
	t.Helper()
	acc, err := store.GetAccount(t.Context(), v.db, v.sc)
	if err != nil {
		t.Fatalf("read account: %v", err)
	}
	return acc.OwnerUserID
}

// openAccountCreation re-registers the API with the switch on, for the tests
// that exercise the open half of U9.
func (v *adminEnv) openAccountCreation(t *testing.T) {
	t.Helper()
	v.opts.Config.AllowAccountCreation = true
	v.reregister(t)
}

// reregister rebuilds the router around whatever v.opts now says. It is how a
// test changes a SETTING rather than a fixture: the effective configuration is
// captured at registration, so a test that edited v.opts without this would be
// asserting against the old value.
func (v *adminEnv) reregister(t *testing.T) {
	t.Helper()
	e := echo.New()
	e.Logger.SetOutput(io.Discard)
	if err := RegisterAdmin(e, v.opts); err != nil {
		t.Fatalf("re-register admin API: %v", err)
	}
	v.e = e
}

// listAccountsFor reads the accounts the deployment holds, for the tests that
// assert one was or was not created.
func listAccountsFor(t *testing.T, v *adminEnv) ([]model.Account, error) {
	t.Helper()
	var accs []model.Account
	err := store.CrossAccount(t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Find(&accs).Error
	})
	return accs, err
}

// session inserts a session row holding the named roles IN THE FIXTURE ACCOUNT,
// and returns a client authenticated as it.
//
// This used to take identity-provider GROUPS, and the roles followed from the
// four ACME_*_GROUP settings. Design C10 removed that derivation: a role is a
// membership row now, so the fixture writes one per role name. The tests below
// are unchanged in what they assert — a caller holding CONFIG_ADMIN and a route
// that CONFIG_ADMIN may not reach — and changed in how that authority arrives,
// which is the whole of the change.
//
// Groups are still stamped on the session because the session still records
// them and the audit trail still reports them; nothing grants on them.
func (v *adminEnv) session(subject string, roleNames ...string) *client {
	v.t.Helper()
	ctx := v.t.Context()
	now := time.Now().UTC()

	user, err := store.FindOrCreateUser(ctx, v.db, subject, strings.ToUpper(subject), "")
	if err != nil {
		v.t.Fatalf("create user %s: %v", subject, err)
	}
	if len(roleNames) > 0 {
		roles, err := store.RolesByName(ctx, v.db, v.sc, roleNames...)
		if err != nil {
			v.t.Fatalf("read roles %v: %v", roleNames, err)
		}
		if len(roles) != len(roleNames) {
			v.t.Fatalf("found %d of %v in the account; a new account is seeded with all four", len(roles), roleNames)
		}
		// Idempotent: a person with two sessions is one person with one
		// membership, and the unique constraint on (account, user, role) says
		// so.
		existing, err := store.ListMembers(ctx, v.db, v.sc)
		if err != nil {
			v.t.Fatalf("read members: %v", err)
		}
		held := map[model.ID]bool{}
		for _, m := range existing {
			if m.UserID == user.ID && m.RoleID != nil {
				held[*m.RoleID] = true
			}
		}
		for _, r := range roles {
			if held[r.ID] {
				continue
			}
			if err := store.AddMember(ctx, v.db, v.sc, user.ID, r.ID); err != nil {
				v.t.Fatalf("add %s to %s: %v", subject, r.Name, err)
			}
		}
	}

	// The session selects the account it will act in. Sign-in no longer does
	// that (task 2.2) — a session begins with nothing selected — so a test that
	// wants an account-scoped route says so here, which is what a person does
	// through the picker.
	account := v.sc.AccountID()
	s := &model.Session{
		ID: subject + "-session-" + hexish(), Subject: subject, DisplayName: strings.ToUpper(subject),
		Groups: model.StringArray(roleNames), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
		SelectedAccountID: &account,
	}
	if err := store.CreateSession(ctx, v.db, s); err != nil {
		v.t.Fatalf("create session: %v", err)
	}
	return &client{t: v.t, e: v.e, sessionID: s.ID}
}

// isHTTPMethod tells a real route from echo's internal placeholders, which it
// registers under names that are not HTTP methods.
func isHTTPMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

var hexCounter int

func hexish() string {
	hexCounter++
	return time.Now().Format("150405.000000000") + "-" + string(rune('a'+hexCounter%26))
}

type client struct {
	t         *testing.T
	e         *echo.Echo
	sessionID string
	csrf      string
}

func (c *client) do(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	if c.csrf == "" && method != http.MethodGet {
		c.token()
	}
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if c.sessionID != "" {
		req.Header.Set("X-Test-Session", c.sessionID)
	}
	if c.csrf != "" {
		req.Header.Set(oidcauth.CSRFHeaderName, c.csrf)
		req.AddCookie(&http.Cookie{Name: oidcauth.CSRFCookieName, Value: c.csrf})
	}
	rec := httptest.NewRecorder()
	c.e.ServeHTTP(rec, req)
	return rec
}

// token performs one GET so the CSRF middleware issues a cookie, and keeps it.
func (c *client) token() {
	c.t.Helper()
	rec := c.do(http.MethodGet, "/api/v1/me", "")
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == oidcauth.CSRFCookieName {
			c.csrf = ck.Value
			return
		}
	}
	c.t.Fatalf("no %s cookie was issued (status %d)", oidcauth.CSRFCookieName, rec.Code)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

// assertEveryRouteHasARow fails when a registered management route is missing
// from the matrix above. Without it the matrix is a list someone has to
// remember to extend: a new route ships, every existing row still passes, and
// the route nobody added is exactly the one whose permission was never
// checked.
//
// covered holds "METHOD /route/:pattern" keys, resolved from each row's
// concrete path by the same router that serves it — so the rows stay readable
// ("/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/7/usage", not "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/:id/usage").
func assertEveryRouteHasARow(t *testing.T, e *echo.Echo, covered map[string]bool) {
	t.Helper()
	for _, r := range e.Routes() {
		// Sign-in is deliberately outside the guard, the catch-alls match no
		// endpoint of their own, and echo registers a placeholder under a
		// pseudo-method for a group prefix no handler serves.
		if !strings.HasPrefix(r.Path, "/api/v1/") || strings.HasPrefix(r.Path, "/api/v1/auth/") ||
			strings.HasSuffix(r.Path, "*") || !isHTTPMethod(r.Method) {
			continue
		}
		if !covered[r.Method+" "+r.Path] {
			t.Errorf("%s %s is registered but has no row in the role matrix — add one saying which roles may reach it",
				r.Method, r.Path)
		}
	}
}

// --- the role matrix -------------------------------------------------------

// TestEveryRouteEnforcesItsRole calls every route as each of the four roles.
//
// Two rows carry the weight: KEY_ROTATION creating a key but not a domain is
// the only lb that distinguishes the CONFIG_ADMIN/KEY_ROTATION boundary from
// a naive "any role may write", and the session routes are the only ones
// ADMIN alone may reach. The rest would pass under a much weaker
// implementation, and are here so a future edit cannot quietly widen one.
//
// The destructive rows address REAL objects, seeded fresh for each role, and
// that matters more since the permission check moved into the service layer
// (design P7). A check that names its object cannot run before the object is
// loaded, so a request for an identifier nothing holds is refused as not-found
// — correctly, since absence and denial must be indistinguishable. Pointed at a
// made-up identifier, every one of these rows would pass against a route with no
// check at all.
func TestEveryRouteEnforcesItsRole(t *testing.T) {
	v := newAdminEnv(t)

	clients := map[string]*client{
		"ADMIN":        v.session("admin", roleAdmin),
		"CONFIG_ADMIN": v.session("config", roleConfig),
		"KEY_ROTATION": v.session("keys", roleKey),
		"READ_ONLY":    v.session("reader", roleRead),
	}

	acct := "/api/v1/accounts/" + v.sc.AccountID().String()
	// One path where there were two: `lb` and `proj` used to address different
	// objects, and the whole of this change is that they no longer do.
	proj := acct + "/projects/" + itoa(v.projID)

	// A refusal is 403 everywhere except here, and the asymmetry with transfer
	// is the decision rather than an inconsistency. Transfer is inside the
	// account-scoped group, so the caller has already been established as acting
	// in this account and "you are not its owner" tells them nothing they could
	// not work out. Checkout is deliberately OUTSIDE that group — it is the one
	// route the owner of a suspended account has to be able to reach — so its
	// account comes from a path parameter with no selection to check it against,
	// and a 403 would confirm that a guessed identifier names a real account. It
	// answers the same not-found whether the account exists or not.
	refusedWithNotFound := map[string]bool{
		"POST /api/v1/accounts/" + v.sc.AccountID().String() + "/checkout": true,
	}

	cases := []struct {
		method, path, body string
		allowed            []string
		// seed, when set, creates a disposable object for THIS role's attempt
		// and returns the path addressing it. Rows that mutate use it, so a
		// refusal is a refusal about permission rather than about a missing row.
		seed func(t *testing.T, v *adminEnv, suffix string) string
	}{
		// Reads: every role.
		{"GET", "/api/v1/me", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj + "/domains", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj + "/keys", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", "/api/v1/accounts/" + v.sc.AccountID().String() + "/challenges", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", "/api/v1/accounts/" + v.sc.AccountID().String() + "/audit", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", "/api/v1/accounts/" + v.sc.AccountID().String() + "/config", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj, "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj + "/usage", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj + "/inventory-history", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj + "/interactions", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"GET", proj + "/verification", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		// "Check now" writes activation state, but the authority it needs is the
		// authority to run the check — dns.plan, exactly as the GET — because it
		// asks the same questions of the same names. A read-only member watching
		// a domain they have just delegated is the person this control exists
		// for, and requiring inventory.write of them would put it out of reach
		// of everyone except the account's administrators.
		{"POST", proj + "/verification", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},

		// Projects: ADMIN only.
		{"POST", "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects", `{"slug":"other","name":"Other"}`, []string{"ADMIN"}, nil},
		{"PATCH", proj, `{"name":"Renamed"}`, []string{"ADMIN"}, nil},
		{"DELETE", "", "", []string{"ADMIN"}, seedProject},

		// Inventory: ADMIN and CONFIG_ADMIN. KEY_ROTATION must not pass here.
		{"POST", proj + "/nodes", `{"label":"z","ipv4":"203.0.113.99"}`, []string{"ADMIN", "CONFIG_ADMIN"}, nil},
		{"PATCH", "", `{"ipv4":"203.0.113.1"}`, []string{"ADMIN", "CONFIG_ADMIN"}, seedNode},
		{"DELETE", "", "", []string{"ADMIN", "CONFIG_ADMIN"}, seedNode},
		{"POST", proj + "/domains", `{"name":"shop.example.com"}`, []string{"ADMIN", "CONFIG_ADMIN"}, nil},
		{"DELETE", "", "", []string{"ADMIN", "CONFIG_ADMIN"}, seedDomain},
		// Observation is a PROPERTY of a domain, so it is governed by the
		// permission that already writes one and not by a new action.
		{"PATCH", "", `{"observeTls":true}`, []string{"ADMIN", "CONFIG_ADMIN"}, seedDomain},
		// Reading back what was observed is reading the inventory: every role,
		// like the domain listing itself. It is what colours a dot, and a dot
		// nobody but an administrator can see is a dot nobody looks at.
		{"GET", "", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, seedDomainPath("/certificate")},
		// The history is the same read gated the same way. What keeps it off a
		// personal account is the account's KIND and not a role, so it is not a
		// distinction this table can express — 418 is asserted where the account
		// kind is (personal_account_test.go).
		{"GET", "", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, seedDomainPath("/certificate/history")},
		// The acme-dns credentials file. It MINTS A KEY, so it is governed like
		// key creation and not like a domain read: KEY_ROTATION reaches it and
		// READ_ONLY does not. A row that let every member produce one would be a
		// key screen with a friendlier name.
		{"POST", "", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}, seedDomainPath("/acme-dns")},

		// Keys: ADMIN, CONFIG_ADMIN and KEY_ROTATION.
		{"POST", proj + "/keys", `{"note":"traefik"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}, nil},
		{"PUT", "", `{"networks":[]}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}, seedKeyPath("/networks")},
		{"PUT", "", `{"expiresIn":"720h"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}, seedKeyPath("/expiry")},
		{"POST", "", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}, seedKeyPath("/rotate")},
		{"DELETE", "", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION"}, seedKeyPath("")},

		// System keys, which are governed like MEMBERSHIP and not like keys, and
		// that is the row worth reading twice. A system key is a member of the
		// account that happens not to be a person (design K6, K8): it holds a
		// role, so minting one is handing out a role, and handing out a role is
		// the unrestricted member's act everywhere else in this table. Letting
		// KEY_ROTATION mint one would let the role whose whole point is rotating
		// secrets create a credential holding ADMIN's authority.
		//
		// Reading them is any member's, for the reason reading the roster is: a
		// credential in the account that nobody can see is how an account
		// acquires an automation nobody remembers creating.
		{"GET", acct + "/keys", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", acct + "/keys", `{"note":"ci","roleId":"` + v.roleID.String() + `"}`, []string{"ADMIN"}, nil},

		// The published contract, reachable by anyone — including, deliberately,
		// callers with no session at all. A contract nobody can read without
		// credentials is not a published contract, and it describes only what
		// the API already answers.
		{"GET", "/api/v1/openapi.yaml", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},

		// Accounts and their administration.
		//
		// Listing accounts and creating one are reachable with NO account
		// selected (design P19), so every role reaches them — they are about
		// which account you are in, not about anything inside one.
		{"GET", "/api/v1/accounts", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		// Nobody: account creation is closed unless a deployment opens it, and
		// the refusal is the same for every role because it is not about what
		// they may do — it is about what this deployment does at all.
		{"POST", "/api/v1/accounts", `{"slug":"fresh","name":"Fresh","amount":25}`, nil, nil},
		{"POST", "/api/v1/accounts/" + v.sc.AccountID().String() + "/select", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},

		// The price list is not a secret: it is what the buy screen renders,
		// and it is readable with no account selected because that screen is
		// reachable before there is an account to select.
		{"GET", "/api/v1/checkout/amounts", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},

		// Nobody, and the reason is the interesting one: these two are gated on
		// OWNERSHIP rather than on a role, and the fixture's owner is
		// "fixture-owner" — none of the four sessions below. An unrestricted
		// member can administer everything inside the account and is still not
		// the person whose account it is, which is exactly what these rows pin.
		//
		// A row that listed ADMIN here would be asserting the opposite: that
		// holding the unrestricted role is enough to spend the owner's money and
		// to give their account away.
		//
		// Checkout answers 404 rather than 403, and that asymmetry with transfer
		// is deliberate. Transfer is inside the account-scoped group, so the
		// caller has already been established as acting in this account and
		// "you are not its owner" tells them nothing they could not work out.
		// Checkout is outside it and takes its account from a path parameter, so
		// a 403 there would confirm that the guessed identifier names a real
		// account — an existence oracle for anybody with a session. It answers
		// the same not-found whether the account exists or not.
		{"POST", "/api/v1/accounts/" + v.sc.AccountID().String() + "/checkout", `{"amount":25}`, nil, nil},
		{"POST", "/api/v1/accounts/" + v.sc.AccountID().String() + "/transfer", `{"subject":"someone-else"}`, nil, nil},

		// Roles and membership change what people can do, so they are governed
		// by membership of the unrestricted role (design P14) — reading the
		// roster is not, because knowing who else is in the room is not a
		// privilege.
		{"GET", acct + "/roles", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", acct + "/roles", `{"name":"ops","grants":[]}`, []string{"ADMIN"}, nil},
		{"PUT", "", `{"name":"ops2","grants":[]}`, []string{"ADMIN"}, seedRole},
		{"DELETE", "", "", []string{"ADMIN"}, seedRole},
		{"GET", acct + "/members", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", acct + "/members", `{"subject":"someone","roleId":"` + v.roleID.String() + `"}`, []string{"ADMIN"}, nil},
		{"DELETE", "", "", []string{"ADMIN"}, seedMember},
		// Giving a role to somebody who accepted an invitation and holds none.
		// ADMIN only, like every other membership change.
		//
		// ADMIN's attempt answers 409, not 204, because the seeded member
		// already holds a role — assigning is only ever the completion of an
		// invitation, never a way to re-point an existing membership. That is
		// the right refusal and is not what this row pins: what it pins is that
		// the other three never reach that question, being refused with 403
		// first.
		{"PUT", "", `{"roleId":"` + v.roleID.String() + `"}`, []string{"ADMIN"}, seedMemberRolePath},

		// Invitations, the account's half. Reading is any member's, for the
		// same reason reading the roster is: a pending invitation nobody can see
		// is how an account acquires a member nobody remembers inviting.
		// Issuing, revoking and re-sending are the unrestricted role's — the
		// owner and the administrators, who are the same people (design V5).
		{"GET", acct + "/invitations", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", acct + "/invitations", `{"email":"matrix@corp.example"}`, []string{"ADMIN"}, nil},
		{"DELETE", "", "", []string{"ADMIN"}, seedInvitation("")},
		{"POST", "", "", []string{"ADMIN"}, seedInvitation("/resend")},

		// Invitations, the invitee's half. EVERY session reaches these, and so
		// would one belonging to nothing at all — which is the property that
		// makes the flow terminate rather than a permissiveness (design V5).
		//
		// Reaching them is not accepting anything. Each row sends a token
		// nothing resolves to and is answered 404, which is what these rows
		// assert: not 403. A 403 here would mean the ROLE was the obstacle,
		// and there is no role in this account or any other that decides whether
		// somebody may answer an invitation addressed to their own mailbox. What
		// decides that is the token and the address, and both are checked
		// inside — see TestInvitationIsBoundToItsAddress.
		{"GET", "/api/v1/invitations", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", "/api/v1/invitations/resolve", `{"token":"nothing-resolves-to-this"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", "/api/v1/invitations/accept", `{"token":"nothing-resolves-to-this"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		{"POST", "/api/v1/invitations/decline", `{"token":"nothing-resolves-to-this"}`, []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},

		// Sessions belong to the person holding them, so every role reaches
		// them and reaches only their own (design C10; see
		// TestSessionsAreTheCallersOwn). They used to be ADMIN-only, on the
		// global role that no longer exists.
		{"GET", "/api/v1/sessions", "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
		// Naming somebody else is refused for every role, which is the point:
		// there is no role that ends another person's session. Named here rather
		// than bare so this row cannot sign the table's own client out halfway
		// through.
		{"DELETE", "/api/v1/sessions?subject=nobody", "", nil, nil},
		{"DELETE", "/api/v1/sessions/" + strings.Repeat("0", 64), "", []string{"ADMIN", "CONFIG_ADMIN", "KEY_ROTATION", "READ_ONLY"}, nil},
	}

	covered := map[string]bool{}
	for _, tc := range cases {
		path := tc.path
		if tc.seed != nil {
			path = tc.seed(t, v, "coverage")
		}
		req := httptest.NewRequest(tc.method, path, nil)
		c := v.e.NewContext(req, httptest.NewRecorder())
		v.e.Router().Find(tc.method, req.URL.Path, c)
		covered[tc.method+" "+c.Path()] = true
	}
	assertEveryRouteHasARow(t, v.e, covered)

	for _, tc := range cases {
		for role, cl := range clients {
			t.Run(tc.method+" "+tc.path+" as "+role, func(t *testing.T) {
				path := tc.path
				if tc.seed != nil {
					path = tc.seed(t, v, role)
				}
				// Re-select the account under test before each attempt. One of
				// the rows CREATES an account and selects it, which is the right
				// product behaviour — someone who just made an account meant to
				// act in it — and would otherwise leave every later row in this
				// loop addressing an account the session is no longer in.
				account := v.sc.AccountID()
				if err := store.SelectAccount(t.Context(), v.db, cl.sessionID, &account); err != nil {
					t.Fatalf("re-select account: %v", err)
				}
				rec := cl.do(tc.method, path, tc.body)
				if contains(tc.allowed, role) {
					if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
						t.Fatalf("%s %s as %s: %d %s, want the role to be accepted",
							tc.method, path, role, rec.Code, rec.Body.String())
					}
					return
				}
				want := http.StatusForbidden
				if refusedWithNotFound[tc.method+" "+tc.path] {
					want = http.StatusNotFound
				}
				if rec.Code != want {
					t.Fatalf("%s %s as %s: %d %s, want %d", tc.method, path, role, rec.Code, rec.Body.String(), want)
				}
			})
		}
	}
}

// A refusal must never depend on a role the caller happens to also hold: a
// user in the key-rotation group and nothing else may mint a key and must not
// touch the inventory. This is the load balancer that pins the boundary, so it is
// asserted end to end rather than only through the matrix above.
func TestKeyRotationMayMintAKeyButNotADomain(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("keys", roleKey)
	proj := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	rec := cl.do(http.MethodPost, proj+"/keys", `{"note":"traefik"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s, want 201", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if created.Secret == "" {
		t.Fatal("key creation returned no secret; the one time it is ever available is now")
	}

	rec = cl.do(http.MethodPost, proj+"/domains", `{"name":"shop.example.com"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create domain as KEY_ROTATION: %d %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestUnauthenticatedManagementRequestIs401(t *testing.T) {
	v := newAdminEnv(t)
	cl := &client{t: t, e: v.e}
	rec := cl.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	var body ErrorBody
	decode(t, rec, &body)
	if body.Message == "" {
		t.Errorf("401 body %s carries no message", rec.Body.String())
	}
}

// A person who holds no role in the account is admitted and permitted nothing,
// rather than refused at the door.
//
// This replaces TestAuthenticatedWithNoMappedGroupIsRefusedAndTold, which
// asserted a 403 naming the groups the request had been seen in — the diagnostic
// for a typo in ACME_ADMIN_GROUP, or for Keycloak's "Full group path" being left
// on. Neither the setting nor the failure mode exists any more (design C10).
//
// The distinction it is worth keeping is the one this asserts: /me answers,
// because it is about the person rather than about the account, and it answers
// with an empty authority — which is how the console knows to render nothing
// rather than to render everything greyed out.
func TestMemberOfNothingIsAdmittedAndPermittedNothing(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("stranger")
	rec := cl.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var me meView
	decode(t, rec, &me)
	if len(me.Roles) != 0 || len(me.Permissions) != 0 {
		t.Errorf("roles %v / permissions %v, want neither — this person is in no role here", me.Roles, me.Permissions)
	}

	// And they cannot select the account, which is where membership is checked
	// (design P19): the fixture put the selection on the session row directly,
	// which is not a door a request can walk through.
	//
	// Not-found rather than forbidden, deliberately: whether an account exists
	// is not something a non-member should learn by asking.
	rec = cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/select", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("select as a non-member: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

// --- /me and the error shape -----------------------------------------------

// The roles /me reports are the ones held IN THE SELECTED ACCOUNT, and they are
// additive: holding two is normal.
func TestMeCarriesDisplayNameGroupsAndRoles(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin, roleRead)
	rec := cl.do(http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var me meView
	decode(t, rec, &me)
	if me.DisplayName != "ADMIN" {
		t.Errorf("displayName %q", me.DisplayName)
	}
	if !contains(me.Groups, roleAdmin) || !contains(me.Groups, roleRead) {
		t.Errorf("groups %v do not carry the session's claim", me.Groups)
	}
	if !contains(me.Roles, "ADMIN") || !contains(me.Roles, "READ_ONLY") {
		t.Errorf("roles %v: roles are additive, both memberships were written", me.Roles)
	}
}

func TestDomainErrorsMapToStatusesAndOneJSONShape(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	proj := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	if rec := cl.do(http.MethodPost, proj+"/domains", `{"name":"shop.example.com"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed domain: %d %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"invalid wildcard", "POST", proj + "/domains", `{"name":"*.*.example.com"}`, http.StatusBadRequest},
		{"duplicate domain", "POST", proj + "/domains", `{"name":"shop.example.com"}`, http.StatusConflict},
		{"unknown project", "GET", "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/00000000-0000-7000-8000-000000000999", "", http.StatusNotFound},
		{"non-empty project", "DELETE", "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID), "", http.StatusConflict},
		{"non-numeric id", "GET", "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/banana", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := cl.do(tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if ct := rec.Header().Get(echo.HeaderContentType); !strings.Contains(ct, echo.MIMEApplicationJSON) {
				t.Fatalf("content type %q, want JSON", ct)
			}
			var body ErrorBody
			decode(t, rec, &body)
			if body.Message == "" {
				t.Fatalf("error body %s carries no message", rec.Body.String())
			}
		})
	}
}

// An unknown path under the API prefix must stay JSON even though the console
// claims everything else: a typo'd endpoint that answers with index.html turns
// into a bug report about the console.
func TestUnknownAPIPathIsJSONNotTheConsole(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)

	for _, path := range []string{"/api/v1/nope", "/api/v2/projects", "/api/"} {
		rec := cl.do(http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "<html>") {
			t.Errorf("%s: answered with the console: %s", path, rec.Body.String())
		}
		var body ErrorBody
		decode(t, rec, &body)
		if body.Message == "" {
			t.Errorf("%s: 404 body %s carries no message", path, rec.Body.String())
		}
	}

	// The fallback still serves everything that is not the API.
	if rec := cl.do(http.MethodGet, "/projects/1", ""); !strings.Contains(rec.Body.String(), "<html>") {
		t.Errorf("the console fallback stopped working: %d %s", rec.Code, rec.Body.String())
	}
}

// --- keys ------------------------------------------------------------------

func TestKeySecretIsReturnedOnceAndNeverRead(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	proj := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	rec := cl.do(http.MethodPost, proj+"/keys", `{"note":"traefik","networks":["203.0.113.5/24"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var created keyCreated
	decode(t, rec, &created)
	if created.Secret == "" {
		t.Fatal("no secret in the creation response")
	}
	// The CIDR is stored masked: PostgreSQL's cidr type refuses host bits.
	if len(created.Key.Networks) != 1 || created.Key.Networks[0] != "203.0.113.0/24" {
		t.Fatalf("networks %v, want the masked prefix", created.Key.Networks)
	}

	rec = cl.do(http.MethodGet, proj+"/keys", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), created.Secret) {
		t.Fatal("the key listing returned the secret")
	}
	var listed []map[string]any
	decode(t, rec, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed %d keys, want 1", len(listed))
	}
	for _, forbidden := range []string{"secret", "secretHash", "SecretHash"} {
		if _, ok := listed[0][forbidden]; ok {
			t.Errorf("the key listing carries a %q field", forbidden)
		}
	}

	// Rotation mints a successor and leaves the old key working, which is what
	// makes a zero-downtime swap possible.
	rec = cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/keys/"+itoa(created.Key.ID)+"/rotate", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rotated keyCreated
	decode(t, rec, &rotated)
	if rotated.Secret == created.Secret || rotated.Key.ID == created.Key.ID {
		t.Fatal("rotation returned the same key")
	}
	if len(rotated.Key.Networks) != 1 {
		t.Errorf("rotation dropped the address restrictions: %v", rotated.Key.Networks)
	}

	rec = cl.do(http.MethodDelete, "/api/v1/accounts/"+v.sc.AccountID().String()+"/keys/"+itoa(created.Key.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	var revoked keyView
	decode(t, rec, &revoked)
	if revoked.Active || revoked.DisabledAt == nil {
		t.Errorf("revoked key still reads as active: %+v", revoked)
	}
}

func TestKeyRestrictionRejectsGarbageCIDR(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects/"+itoa(v.projID)+"/keys", `{"networks":["not-a-cidr"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// --- sessions --------------------------------------------------------------

// Sessions are the caller's own, and revoking one is immediate.
//
// This used to be an ADMIN listing every session in the deployment and revoking
// somebody else's — the operator half of "remove them from the group AND revoke
// their sessions". Design C10 removed the global ADMIN role, and nothing
// per-account could replace it: a session belongs to a person rather than to an
// account, and the listing would have disclosed every other tenant's subjects to
// any account's administrator. Ending somebody else's session is a staff power
// now, and the staff plane does not exist yet.
//
// What survives is what a person can do about their own: see them, and end them.
func TestSessionsAreTheCallersOwn(t *testing.T) {
	v := newAdminEnv(t)
	admin := v.session("admin", roleAdmin)
	aliceLaptop := v.session("alice", roleRead)
	alicePhone := v.session("alice", roleRead)
	bob := v.session("bob", roleRead)

	rec := aliceLaptop.do(http.MethodGet, "/api/v1/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions: %d %s", rec.Code, rec.Body.String())
	}
	var listed []sessionView
	decode(t, rec, &listed)
	if len(listed) != 2 {
		t.Fatalf("alice sees %d sessions, want her two: %+v", len(listed), listed)
	}
	for _, s := range listed {
		if s.Subject != "alice" {
			t.Fatalf("alice sees a session belonging to %q", s.Subject)
		}
		if s.CreatedAt.IsZero() || s.ExpiresAt.IsZero() {
			t.Fatalf("session row is missing start/expiry: %+v", s)
		}
	}
	// The listing must not carry anything usable as a credential.
	for _, id := range []string{aliceLaptop.sessionID, alicePhone.sessionID, bob.sessionID} {
		if strings.Contains(rec.Body.String(), id) {
			t.Fatal("the sessions listing contains a raw session id")
		}
	}

	// A handle belonging to somebody else is not merely refused — it is not in
	// the set this caller can name at all, so it reads as not-found.
	rec = bob.do(http.MethodGet, "/api/v1/sessions", "")
	var bobsOwn []sessionView
	decode(t, rec, &bobsOwn)
	if len(bobsOwn) != 1 {
		t.Fatalf("bob sees %d sessions, want his one", len(bobsOwn))
	}
	if rec := aliceLaptop.do(http.MethodDelete, "/api/v1/sessions/"+bobsOwn[0].Handle, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("alice revoking bob's session: %d %s, want 404", rec.Code, rec.Body.String())
	}
	if rec := bob.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("bob was signed out by alice: %d", rec.Code)
	}

	// Her own phone, though, she may end — and it takes effect at the next
	// request rather than at the next sign-in.
	var phoneHandle string
	for _, s := range listed {
		if s.Handle != sessionHandle(aliceLaptop.sessionID) {
			phoneHandle = s.Handle
		}
	}
	if phoneHandle == "" {
		t.Fatal("alice's other session was not listed")
	}
	if rec := aliceLaptop.do(http.MethodDelete, "/api/v1/sessions/"+phoneHandle, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := alicePhone.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the revoked session still answers: %d, want 401", rec.Code)
	}
	if rec := aliceLaptop.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("the session that did the revoking was ended too: %d", rec.Code)
	}

	// The revocation is attributable, and it belongs to NO account.
	//
	// It is read straight out of the table rather than through
	// /accounts/:id/audit, because there is no account trail it could appear in
	// — which is the assertion. A session belongs to a person, not to a tenant:
	// filing the entry into whichever account happened to be selected would put
	// one person's security event into somebody else's trail, and since task 2.2
	// the usual selection is none at all. For the same reason it records no
	// roles: a role is authority IN an account, and claiming the one the actor
	// held somewhere else would say the revocation was performed with an
	// authority that has nothing to do with it.
	_ = admin
	var entries []model.AuditEntry
	if err := store.CrossAccount(t.Context(), v.db, func(tx *gorm.DB) error {
		return tx.Where("object_type = ?", authz.ObjectSession).Order("occurred_at").Find(&entries).Error
	}); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no audit entry for the revocation")
	}
	e := entries[0]
	if e.ActorSubject == nil || *e.ActorSubject != "alice" || e.ObjectLabel != "alice" {
		t.Fatalf("audit entry does not name who did it and whose access ended: %+v", e)
	}
	if e.AccountID != nil {
		t.Fatalf("the revocation was filed into account %s; a session belongs to a person", *e.AccountID)
	}
	if len(e.ActorRoles) != 0 {
		t.Fatalf("the revocation claims roles %v; it was not performed inside an account", e.ActorRoles)
	}
}

// "Sign me out everywhere", after a laptop goes missing — and only the caller's
// own everywhere.
func TestRevokingEverySessionOfTheCaller(t *testing.T) {
	v := newAdminEnv(t)
	first := v.session("carol", roleRead)
	second := v.session("carol", roleRead)
	other := v.session("dave", roleRead)

	if rec := first.do(http.MethodDelete, "/api/v1/sessions", ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke my sessions: %d %s", rec.Code, rec.Body.String())
	}
	for _, cl := range []*client{first, second} {
		if rec := cl.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("a session of carol survived: %d", rec.Code)
		}
	}
	if rec := other.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusOK {
		t.Errorf("dave was signed out too: %d %s", rec.Code, rec.Body.String())
	}
}

// The ?subject= parameter is kept as a CHECK rather than a target. Dropping it
// would have made a call naming somebody else silently sign the CALLER out,
// which is equally destructive and harder to notice.
func TestRevokingAnotherSubjectsSessionsIsRefused(t *testing.T) {
	v := newAdminEnv(t)
	admin := v.session("admin", roleAdmin)
	victim := v.session("erin", roleRead)

	rec := admin.do(http.MethodDelete, "/api/v1/sessions?subject=erin", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("revoking erin's sessions: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if rec := victim.do(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("erin was signed out anyway: %d", rec.Code)
	}
	// Naming yourself is fine: it is what the console sends.
	if rec := admin.do(http.MethodDelete, "/api/v1/sessions?subject=admin", ""); rec.Code != http.StatusOK {
		t.Fatalf("naming my own subject: %d %s, want 200", rec.Code, rec.Body.String())
	}
}

// --- CSRF, audit, plan -----------------------------------------------------

func TestStateChangingRequestWithoutTheTokenIsRejected(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	// A client that never fetches a token: a valid session cookie alone must
	// not be enough.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", strings.NewReader(`{"slug":"x","name":"X"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Test-Session", cl.sessionID)
	rec := httptest.NewRecorder()
	v.e.ServeHTTP(rec, req)

	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("status %d, want a 4xx: %s", rec.Code, rec.Body.String())
	}
	var count int64
	v.db.Model(&model.Project{}).Where("slug = ?", "x").Count(&count)
	if count != 0 {
		t.Fatal("the project was created despite the missing CSRF token")
	}
}

// wantEntry is one expected audit entry: the action, the object label it must
// carry after the object itself is gone, and the project it must be filterable
// by.
type wantEntry struct {
	action    string
	label     string
	projectID *model.ID
}

// anything stands in for a value the test cannot know before the request:
// a key identifier is minted during it, and a project id does not exist until
// the project has been inserted. Both are asserted separately, right after.
const anything = "\x00any"

// anyProject is a sentinel pointer, never a real identifier: the assertion
// compares against the pointer itself, so the value behind it is irrelevant and
// deliberately not a value any row could hold.
var anyProject = &[]model.ID{model.NewID()}[0]

// auditRecorder drives one mutating route at a time and asserts exactly which
// entries it produced. It reads through GET /api/v1/audit — the surface an
// operator actually has — rather than the table, so a mutation that writes a
// row nobody can find still fails here.
type auditRecorder struct {
	cl      *client
	auditAt string
	// seen is every entry this recorder has already accounted for. Identifiers
	// are UUIDs now, so "newer than the last one" is no longer a comparison —
	// v7 values do sort by time, but as a [16]byte they are not ordered in Go,
	// and a set says what is meant without relying on that.
	seen map[model.ID]bool
}

// step issues one mutating request and asserts that it produced exactly want,
// in the order written. Passing no wantEntry asserts the route wrote nothing.
func (r *auditRecorder) step(t *testing.T, method, path, body string, wantStatus int, want ...wantEntry) *httptest.ResponseRecorder {
	t.Helper()
	rec := r.cl.do(method, path, body)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: %d %s, want %d", method, path, rec.Code, rec.Body.String(), wantStatus)
	}

	listing := r.cl.do(http.MethodGet, r.auditAt+"?limit=1000", "")
	var all []auditView
	decode(t, listing, &all)

	// Newest first, so walk backwards to get them in the order they were
	// written, and keep only what this request added.
	var got []auditView
	for i := len(all) - 1; i >= 0; i-- {
		if !r.seen[all[i].ID] {
			got = append(got, all[i])
		}
	}
	if r.seen == nil {
		r.seen = map[model.ID]bool{}
	}
	for _, e := range got {
		r.seen[e.ID] = true
	}

	if len(got) != len(want) {
		t.Fatalf("%s %s wrote %d audit entries, want %d: %+v", method, path, len(got), len(want), got)
	}
	for i, w := range want {
		e := got[i]
		if e.Action != w.action {
			t.Errorf("%s %s entry %d: action %q, want %q", method, path, i, e.Action, w.action)
		}
		if w.label != anything && e.ObjectLabel != w.label {
			t.Errorf("%s %s entry %d: objectLabel %q, want %q", method, path, i, e.ObjectLabel, w.label)
		}
		switch {
		case w.projectID == anyProject:
		case w.projectID == nil && e.ProjectID != nil:
			t.Errorf("%s %s entry %d: projectId %d, want none", method, path, i, *e.ProjectID)
		case w.projectID != nil && (e.ProjectID == nil || *e.ProjectID != *w.projectID):
			t.Errorf("%s %s entry %d: projectId %v, want %d", method, path, i, e.ProjectID, *w.projectID)
		}
		if e.ActorSource != authz.SourceConsole || !contains(e.ActorRoles, "ADMIN") {
			t.Errorf("%s %s entry %d: actor %q roles %v, want a console ADMIN",
				method, path, i, e.ActorSource, e.ActorRoles)
		}
		if e.ObjectType == "" {
			t.Errorf("%s %s entry %d: no objectType", method, path, i)
		}
	}
	return rec
}

// TestMutationsAreAudited walks every mutating route on the management API and
// asserts that each writes exactly the entries it should — no route silently
// skipping the trail, and no route writing two rows for one change.
//
// It is one sequential scenario rather than a table because most of these
// routes need an object that an earlier one created, and because "exactly
// these entries and no others" is only meaningful if nothing else is running.
func TestMutationsAreAudited(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	r := &auditRecorder{cl: cl, auditAt: "/api/v1/accounts/" + v.sc.AccountID().String() + "/audit"}

	// --- projects ---
	rec := r.step(t, http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", `{"slug":"beta","name":"Beta"}`,
		http.StatusCreated, wantEntry{authz.ActionProjectCreate, "beta", anyProject})
	var proj projectView
	decode(t, rec, &proj)
	// Every entry below belongs to this project, and stays filterable by it.
	pid := &proj.ID
	// The create entry could not know its own project id before the insert, so
	// it is checked here instead of in the step above.
	assertProjectID(t, cl, r.auditAt, authz.ActionProjectCreate, proj.ID)

	projPath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(proj.ID)
	r.step(t, http.MethodPatch, projPath, `{"name":"Beta Renamed"}`,
		http.StatusOK, wantEntry{authz.ActionProjectUpdate, "beta", pid})

	// --- nodes ---
	rec = r.step(t, http.MethodPost, projPath+"/nodes", `{"label":"n1","ipv4":"203.0.113.20"}`,
		http.StatusCreated, wantEntry{authz.ActionNodeCreate, "n1", pid})
	var n nodeView
	decode(t, rec, &n)
	nodePath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/nodes/" + itoa(n.ID)

	r.step(t, http.MethodPatch, nodePath, `{"ipv4":"203.0.113.21"}`,
		http.StatusOK, wantEntry{authz.ActionNodeUpdate, "n1", pid})

	// --- domains, and the alias that appears and disappears with them ---
	// The alias entry's label is whatever the service generated — the request
	// cannot ask for one (design A5), so the test cannot know it either.
	rec = r.step(t, http.MethodPost, projPath+"/domains", `{"name":"shop.example.com"}`,
		http.StatusCreated,
		wantEntry{authz.ActionDomainCreate, "shop.example.com", pid},
		wantEntry{authz.ActionAliasCreate, anything, pid})
	var dom domainCreated
	decode(t, rec, &dom)

	// A second domain on the same challenge name reuses the alias, so no
	// alias entry this time — the wildcard-and-apex case.
	rec = r.step(t, http.MethodPost, projPath+"/domains", `{"name":"*.shop.example.com"}`,
		http.StatusCreated, wantEntry{authz.ActionDomainCreate, "*.shop.example.com", pid})
	var wildcard domainCreated
	decode(t, rec, &wildcard)

	r.step(t, http.MethodDelete, "/api/v1/accounts/"+v.sc.AccountID().String()+"/domains/"+itoa(wildcard.ID), "",
		http.StatusOK, wantEntry{authz.ActionDomainDelete, "*.shop.example.com", pid})
	// Now the last domain goes, and the alias goes with it: the cascade is its
	// own entry, because losing the alias is what stops the customer's
	// published CNAME from validating.
	r.step(t, http.MethodDelete, "/api/v1/accounts/"+v.sc.AccountID().String()+"/domains/"+itoa(dom.ID), "",
		http.StatusOK,
		wantEntry{authz.ActionDomainDelete, "shop.example.com", pid},
		wantEntry{authz.ActionAliasDelete, anything, pid})

	// --- api keys ---
	//
	// Key entries carry no project. A key is anchored to the account and reaches
	// whatever its grants name, which may be several projects or none, so there
	// is no single project the entry belongs to — and inventing one would make
	// the trail's project filter lie.
	rec = r.step(t, http.MethodPost, projPath+"/keys", `{"note":"traefik","networks":["203.0.113.0/24"]}`,
		http.StatusCreated, wantEntry{authz.ActionKeyCreate, anything, nil})
	var created keyCreated
	decode(t, rec, &created)
	keyPath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/keys/" + itoa(created.Key.ID)
	assertLabel(t, cl, r.auditAt, authz.ActionKeyCreate, created.Key.Identifier)

	r.step(t, http.MethodPut, keyPath+"/networks", `{"networks":["198.51.100.0/24"]}`,
		http.StatusOK, wantEntry{authz.ActionKeyRestrict, created.Key.Identifier, nil})
	r.step(t, http.MethodPut, keyPath+"/expiry", `{"expiresIn":"720h"}`,
		http.StatusOK, wantEntry{authz.ActionKeyExpiryChange, created.Key.Identifier, nil})

	rec = r.step(t, http.MethodPost, keyPath+"/rotate", "",
		http.StatusCreated, wantEntry{authz.ActionKeyRotate, anything, nil})
	var rotated keyCreated
	decode(t, rec, &rotated)
	assertLabel(t, cl, r.auditAt, authz.ActionKeyRotate, rotated.Key.Identifier)

	r.step(t, http.MethodDelete, keyPath, "",
		http.StatusOK, wantEntry{authz.ActionKeyRevoke, created.Key.Identifier, nil})

	// The entries outlive what they describe: the project is gone, and its
	// trail is still there and still filterable by it.
	listing := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?projectId="+itoa(proj.ID), "")
	var survivors []auditView
	decode(t, listing, &survivors)
	if len(survivors) < 10 {
		t.Fatalf("%d entries survive the deleted project, want the whole trail", len(survivors))
	}
}

// assertLabel checks the most recent entry for an action carries label. Used
// where the label is only known from the response — a key identifier is minted
// during the request.
func assertLabel(t *testing.T, cl *client, auditAt, action, label string) {
	t.Helper()
	var es []auditView
	decode(t, cl.do(http.MethodGet, auditAt+"?action="+action, ""), &es)
	if len(es) == 0 || es[0].ObjectLabel != label {
		t.Fatalf("%s: objectLabel %+v, want %q", action, es, label)
	}
}

func assertProjectID(t *testing.T, cl *client, auditAt, action string, projectID model.ID) {
	t.Helper()
	var es []auditView
	decode(t, cl.do(http.MethodGet, auditAt+"?action="+action, ""), &es)
	if len(es) == 0 || es[0].ProjectID == nil || *es[0].ProjectID != projectID {
		t.Fatalf("%s: projectId %+v, want %d", action, es, projectID)
	}
}

// TestActionClassFilterNarrowsTheTrail covers the config/credential filter:
// the two questions an audit trail is actually opened for.
func TestActionClassFilterNarrowsTheTrail(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	projPath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	if rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", `{"slug":"gamma","name":"Gamma"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}
	if rec := cl.do(http.MethodPost, projPath+"/keys", `{"note":"k"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct{ class, wantAction string }{
		{authz.ClassConfig, authz.ActionProjectCreate},
		{authz.ClassCredential, authz.ActionKeyCreate},
	} {
		var es []auditView
		decode(t, cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?actionClass="+tc.class, ""), &es)
		if len(es) == 0 {
			t.Fatalf("actionClass=%s returned nothing", tc.class)
		}
		for _, e := range es {
			if authz.ActionClass(e.Action) != tc.class {
				t.Errorf("actionClass=%s returned %q, which is %q", tc.class, e.Action, authz.ActionClass(e.Action))
			}
		}
		if es[0].Action != tc.wantAction {
			t.Errorf("actionClass=%s newest entry is %q, want %q", tc.class, es[0].Action, tc.wantAction)
		}
	}

	// A class nobody defined is a mistake, not an empty trail.
	if rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?actionClass=nonsense", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("actionClass=nonsense: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// TestRolledBackChangeLeavesNoAuditEntry is the first half of "the entry is
// written atomically with the change": the entry is written, the change then
// fails before commit, and nothing at all persists — not the change, and not
// the record of it.
func TestRolledBackChangeLeavesNoAuditEntry(t *testing.T) {
	v := newAdminEnv(t)
	ctx := t.Context()

	boom := errors.New("the change failed after its audit entry was written")
	err := v.sc.Run(ctx, v.db, func(tx *gorm.DB) error {
		p, err := inventory.CreateProject(ctx, tx, v.sc, authz.CLISubject(v.sc.AccountID()), inventory.Limits{}, "doomed", "Doomed")
		if err != nil {
			return err
		}
		err = authz.Record(ctx, tx, nil, authz.SystemActor().InAccount(v.sc.AccountID()), authz.Event{
			Action: authz.ActionProjectCreate, ObjectType: authz.ObjectProject,
			ObjectID: &p.ID, ObjectLabel: p.Slug, ProjectID: &p.ID,
		})
		if err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("transaction error = %v, want %v", err, boom)
	}

	var projects, entries int64
	v.db.Model(&model.Project{}).Where("slug = ?", "doomed").Count(&projects)
	v.db.Model(&model.AuditEntry{}).Where("object_label = ?", "doomed").Count(&entries)
	if projects != 0 {
		t.Error("the rolled-back project persisted")
	}
	if entries != 0 {
		t.Error("an audit entry persisted for a change that was rolled back")
	}
}

// TestFailedAuditWriteRollsBackTheChange is the second half: if the entry
// cannot be written, the change must not be applied. The failure is injected
// in the database rather than faked in Go, so what is under test is the real
// path a full disk or a revoked grant would take.
func TestFailedAuditWriteRollsBackTheChange(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)

	stmts := []string{
		`create function refuse_audit() returns trigger language plpgsql as $$
		 begin raise exception 'audit store unavailable'; end $$`,
		`create trigger refuse_audit before insert on audit_entries
		 for each row execute function refuse_audit()`,
	}
	for _, q := range stmts {
		if err := v.db.Exec(q).Error; err != nil {
			t.Fatalf("install the failing audit writer: %v", err)
		}
	}

	rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", `{"slug":"unrecorded","name":"Unrecorded"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create project with a broken audit store: %d %s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "nothing was applied") {
		t.Errorf("the error does not say the change was not applied: %s", rec.Body.String())
	}

	var projects int64
	v.db.Model(&model.Project{}).Where("slug = ?", "unrecorded").Count(&projects)
	if projects != 0 {
		t.Fatal("the project was created even though its audit entry could not be written")
	}
}

// The plan endpoints are gone; what they carried is a property of the domain
// now (change domain-diagnostics, G2). The value worth defending is the same one
// the plan test defended: a customer is told to publish a CNAME to the name this
// service actually answers for, once per delegation however many domains share
// it.
func TestDomainReadCarriesTheRecordsToPublish(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("reader", roleRead)
	proj := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	admin := v.session("admin", roleAdmin)
	for _, name := range []string{"example.com", "*.example.com"} {
		if rec := admin.do(http.MethodPost, proj+"/domains", `{"name":"`+name+`"}`); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	var got []domainView
	rec := cl.do(http.MethodGet, proj+"/domains", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("domains: %d %s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &got)
	if len(got) != 2 {
		t.Fatalf("%d domains, want 2", len(got))
	}

	zone := strings.TrimSuffix(v.opts.Config.Zone, ".")
	targets := map[string]bool{}
	for _, d := range got {
		if d.CNAMETarget == "" {
			t.Errorf("%s: cnameTarget is empty, so the console has nothing to tell the customer to publish", d.Name)
		}
		if !strings.HasSuffix(d.CNAMETarget, "."+zone) {
			t.Errorf("%s: cnameTarget %q is not in the zone this service serves (%s)", d.Name, d.CNAMETarget, zone)
		}
		targets[d.CNAMETarget] = true
	}
	// The apex and its wildcard share one alias, so one target between them.
	if len(targets) != 1 {
		t.Errorf("%d distinct targets, want 1 shared by the apex and its wildcard: %v", len(targets), targets)
	}
	// One issuer configured, so both tags at the registrable name — rendered
	// rdata, which is what a dialog prints. With no issuer it would be null
	// rather than empty, which is what lets a console hide the CAA dot instead
	// of showing a grey one for a question nobody asked.
	want := []string{`0 issue "letsencrypt.org"`, `0 issuewild "letsencrypt.org"`}
	for _, d := range got {
		if !slices.Equal(d.ExpectedCAA, want) {
			t.Errorf("%s: expectedCaa = %v, want %v at the registrable name", d.Name, d.ExpectedCAA, want)
		}
	}
}

// --- the expectation-to-checks adapter (no database) -----------------------

// A wildcard and its apex share one challenge name and every domain under one
// registrable name shares one CAA set, so both are asked once. Getting this
// wrong quadruples the queries one screen open puts on somebody's recursor, and
// it is the only thing standing between "answer per domain" and doing so.
func TestGroupForChecksAsksEachDistinctNameOnce(t *testing.T) {
	checks := groupForChecks(expectedDomains(), []string{"letsencrypt.org"})
	if len(checks) != 3 {
		t.Fatalf("%d checks, want 3 (two challenge names, one registrable): %+v", len(checks), checks)
	}
	kinds := map[string]int{}
	for _, c := range checks {
		kinds[c.kind]++
	}
	if kinds["CNAME"] != 2 || kinds["CAA"] != 1 {
		t.Errorf("checks by kind = %v, want two CNAME and one CAA", kinds)
	}
	for _, c := range checks {
		if c.kind == "CNAME" && (c.cname == nil || !strings.HasSuffix(c.cname.Expected, ".")) {
			t.Errorf("the CNAME target must be fully qualified before it is compared: %+v", c.cname)
		}
		if c.kind == "CAA" && (c.caa == nil || len(c.caa.Issuers) != 1) {
			// The check compares issuer IDENTIFIERS; the rendered rdata is what
			// the customer publishes, and is carried separately.
			t.Errorf("CAA check: %+v", c.caa)
		}
	}
}

// A domain whose alias has gone missing is asked nothing — there is no target to
// compare against — and must still appear in the answer rather than vanishing
// off the screen.
func TestGroupForChecksSkipsADomainWithNoAlias(t *testing.T) {
	exps := expectedDomains()
	exps[2].Target = ""
	checks := groupForChecks(exps, nil)
	for _, c := range checks {
		if c.name == "_acme-challenge.shop.example.com" {
			t.Fatalf("a domain with no alias was asked for anyway: %+v", c)
		}
	}
	rows := domainChecks(exps, nil)
	if len(rows) != 3 {
		t.Fatalf("%d rows, want all three domains present", len(rows))
	}
	if rows[2].Delegation.Status != statusNotChecked || len(rows[2].Delegation.Expected) != 0 {
		t.Errorf("the aliasless row = %+v, want not-checked with nothing to publish", rows[2].Delegation)
	}
}

func TestVerificationResolverPrefersConfigurationAndAddsAPort(t *testing.T) {
	got := verificationResolver(config.Config{VerificationResolver: "1.1.1.1"}, discard())
	if got != "1.1.1.1:53" {
		t.Errorf("resolver %q, want 1.1.1.1:53", got)
	}
	if got := verificationResolver(config.Config{VerificationResolver: "1.1.1.1:5353"}, discard()); got != "1.1.1.1:5353" {
		t.Errorf("resolver %q, want the configured port kept", got)
	}
}

func TestParseCIDRsMasksHostBits(t *testing.T) {
	got, err := parseCIDRs([]string{"203.0.113.5/24", "2001:db8::1/32"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"203.0.113.0/24", "2001:db8::/32"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %q, want %q", got[i], want[i])
		}
	}
	if _, err := netip.ParsePrefix(got[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCIDRs([]string{"nope"}); err == nil {
		t.Error("a malformed CIDR was accepted")
	}
}

// The effective configuration is a screen an operator reads, so it must not
// carry the secrets.
//
// It used to carry a roleGroups block as well, naming the identity-provider
// group behind each of the four global roles, and this test asserted both. The
// block is gone with the mapping (design C10) and the redaction assertion is
// deliberately NOT gone with it: the two were only ever in one test by accident
// of what the route returned, and deleting the whole test would have quietly
// stopped checking that the client secret and the database password stay off the
// screen.
func TestEffectiveConfigurationIsRedacted(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("reader", roleRead)
	rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{"super-secret-value", "hunter2"} {
		if strings.Contains(body, secret) {
			t.Errorf("the effective configuration leaked %q: %s", secret, body)
		}
	}
	// And it names no group-to-role mapping, because there is none to name.
	if strings.Contains(body, "roleGroups") {
		t.Errorf("the effective configuration still reports a group-to-role mapping: %s", body)
	}
}

func itoa(n model.ID) string { return n.String() }

// --- paging, filtering and the time range ----------------------------------

// seedProjects creates n projects whose slugs sort in creation order, plus
// returns the ids, so a test can talk about "a row on a later page".
func (v *adminEnv) seedProjects(cl *client, slugs ...string) []model.ID {
	v.t.Helper()
	ids := make([]model.ID, 0, len(slugs))
	for _, slug := range slugs {
		rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", `{"slug":"`+slug+`","name":"`+slug+`"}`)
		if rec.Code != http.StatusCreated {
			v.t.Fatalf("seed project %s: %d %s", slug, rec.Code, rec.Body.String())
		}
		var p projectView
		decode(v.t, rec, &p)
		ids = append(ids, p.ID)
	}
	return ids
}

func totalCount(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	v := rec.Header().Get(totalCountHeader)
	if v == "" {
		t.Fatalf("no %s header on %s", totalCountHeader, rec.Body.String())
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s = %q: %v", totalCountHeader, v, err)
	}
	return n
}

// The four properties a paged, filtered list screen is only honest if it has:
// a refused over-large limit (a silent clamp leaves the caller with a short
// list and no way to know), a total counted before paging (a pager cannot say
// "51-100 of 4,213" otherwise), a filter that runs in SQL rather than over the
// page, and a `_` that means an underscore.
func TestListPagingFilteringAndTotals(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	// "acme" already exists from the fixture; these make six.
	v.seedProjects(cl, "aa-first", "bb-second", "cc-third", "dd-fourth", "zz-last")

	// A limit above the maximum is refused, and the refusal names the bound.
	rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?limit=501", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=501: %d %s, want 400", rec.Code, rec.Body.String())
	}
	var body ErrorBody
	decode(t, rec, &body)
	if !strings.Contains(body.Message, "500") {
		t.Errorf("the refusal %q does not name the maximum", body.Message)
	}
	for _, bad := range []string{"limit=0", "limit=-1", "limit=banana", "offset=-1", "offset=x"} {
		if rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?"+bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", bad, rec.Code, rec.Body.String())
		}
	}

	// The total is the count of everything matching the filter, not the page.
	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var page []projectView
	decode(t, rec, &page)
	if len(page) != 2 {
		t.Fatalf("%d rows on a limit=2 page, want 2", len(page))
	}
	if got := totalCount(t, rec); got != 6 {
		t.Errorf("%s = %d on a two-row page, want the pre-paging count 6", totalCountHeader, got)
	}

	// zz-last is ordered by slug and so falls outside the first page, but the
	// filter runs in SQL: it comes back on page one of the FILTERED result.
	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?q=zz-last&limit=2", "")
	decode(t, rec, &page)
	if len(page) != 1 || page[0].Slug != "zz-last" {
		t.Fatalf("q=zz-last on a two-row page returned %+v, want the one matching row", page)
	}
	if got := totalCount(t, rec); got != 1 {
		t.Errorf("%s = %d for a filter matching one project, want 1", totalCountHeader, got)
	}

	// `_` is a LIKE wildcard, and a filter containing one must match it
	// literally — otherwise a search for "shop_eu" quietly returns everything.
	if rec := cl.do(http.MethodPatch, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects/"+itoa(v.projID), `{"name":"Shop_EU"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?q=Shop_EU", "")
	decode(t, rec, &page)
	if len(page) != 1 || page[0].Name != "Shop_EU" {
		t.Fatalf("q=Shop_EU returned %+v, want the one project actually named that", page)
	}
	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?q=Shop_E", "")
	decode(t, rec, &page)
	if len(page) != 1 {
		t.Errorf("q=Shop_E returned %d projects; _ was treated as a wildcard", len(page))
	}
	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?q=ShopXEU", "")
	decode(t, rec, &page)
	if len(page) != 0 {
		t.Errorf("q=ShopXEU returned %+v; _ matched an arbitrary character", page)
	}
}

// The audit range picker submits bare dates, and a bare `to` means the END of
// the day named. Midnight would silently exclude everything recorded during the
// day the operator asked for — the most recent entries, which are the ones they
// opened the screen for.
func TestAuditTimeRangeCoversTheWholeDayNamed(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)

	if rec := cl.do(http.MethodPost, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects", `{"slug":"today","name":"Today"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}
	// The bounds are parsed as UTC, so the day is named in UTC too.
	day := time.Now().UTC().Format("2006-01-02")

	var entries []auditView
	rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?from="+day+"&to="+day, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("range: %d %s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &entries)
	if len(entries) == 0 {
		t.Fatalf("to=%s excluded an entry recorded during that day", day)
	}
	if totalCount(t, rec) != len(entries) {
		t.Errorf("%s = %d for %d entries", totalCountHeader, totalCount(t, rec), len(entries))
	}

	// A day before the entry excludes it, which is what makes the assertion
	// above a test of the bound rather than of an unfiltered listing.
	before := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	decode(t, cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?to="+before, ""), &entries)
	if len(entries) != 0 {
		t.Errorf("to=%s returned %d entries recorded after it", before, len(entries))
	}

	// An inverted range is a mistake, not an empty trail: an audit screen must
	// never answer "nothing happened" by accident.
	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?from="+day+"&to="+before, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("from=%s&to=%s: %d %s, want 400", day, before, rec.Code, rec.Body.String())
	}
	if rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/audit?from=yesterday", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("from=yesterday: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// The console loads a project by id rather than searching the list for it: with
// paging on, the list is one page and the project being opened may be on
// another. This is the regression that read as "project not found" for anyone
// past page one.
func TestProjectOutsideTheFirstPageIsStillReadableByID(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	ids := v.seedProjects(cl, "zz-last")
	last := ids[0]

	var page []projectView
	rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects?limit=1", "")
	decode(t, rec, &page)
	if len(page) != 1 || page[0].ID == last {
		t.Fatalf("first page %+v already contains project %d; the test proves nothing", page, last)
	}

	rec = cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects/"+itoa(last), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get project %d by id: %d %s, want 200", last, rec.Code, rec.Body.String())
	}
	var got projectView
	decode(t, rec, &got)
	if got.ID != last || got.Slug != "zz-last" {
		t.Fatalf("got %+v, want project %d", got, last)
	}
	if rec := cl.do(http.MethodGet, "/api/v1/accounts/"+v.sc.AccountID().String()+"/projects/00000000-0000-7000-8000-000000000999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project by id: %d, want 404", rec.Code)
	}
}

// --- the interaction history endpoint --------------------------------------

// seedInteraction inserts one history row for the fixture's project.
// Written straight to the table rather than through the challenge API, because
// what is under test here is the READING: three of the five outcomes cannot be
// produced through /present and /cleanup at all without waiting for a sweep.
func (v *adminEnv) seedInteraction(label string, withdrawn bool, abandoned bool, queries int64) {
	v.t.Helper()
	in := &model.ChallengeInteraction{
		ProjectID:     &v.projID,
		AliasID:       &v.projID, // no alias is needed: the history keeps no foreign keys
		AliasLabel:    label,
		ChallengeFQDN: "_acme-challenge." + label + ".example.com",
		DomainNames:   model.StringArray{label + ".example.com"},
		FQDNForm:      "alias",
		ValueDigest:   store.ValueDigest(label),
		PublishedAt:   time.Now(),
		Abandoned:     abandoned,
		QueryCount:    queries,
	}
	if withdrawn {
		now := time.Now()
		in.WithdrawnAt = &now
	}
	// challenge_interactions carries no account and no policy, so a direct
	// insert is still the honest fixture here — unlike every inventory table,
	// where one would be refused by the database.
	if err := v.db.Create(in).Error; err != nil {
		v.t.Fatalf("seed interaction %s: %v", label, err)
	}
}

// All five outcomes, the filter an operator actually acts on, the total, and
// the fact that a READ_ONLY user can open the screen at all.
//
// The outcome is derived on read rather than stored, so these five are the
// whole vocabulary — and `abandoned` is checked FIRST, which is why a swept row
// with no queries must read "abandoned" and not "unqueried": the two point at
// different people. One says the client vanished mid-validation, the other says
// the customer's CNAME is not published.
func TestInteractionOutcomesFilterAndTotal(t *testing.T) {
	v := newAdminEnv(t)
	reader := v.session("reader", roleRead)
	path := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID) + "/interactions"

	v.seedInteraction("validated-one", true, false, 4)
	v.seedInteraction("unqueried-one", true, false, 0)
	v.seedInteraction("abandoned-one", true, true, 0)
	v.seedInteraction("live-one", false, false, 2)
	v.seedInteraction("pending-one", false, false, 0)
	// A swept row that WAS queried still reads as abandoned: the sweep is the
	// stronger fact about what happened to it.
	v.seedInteraction("abandoned-queried", true, true, 3)

	rec := reader.do(http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("READ_ONLY reading the history: %d %s, want 200", rec.Code, rec.Body.String())
	}
	var all []interactionView
	decode(t, rec, &all)
	if got := totalCount(t, rec); got != 6 {
		t.Errorf("%s = %d, want 6", totalCountHeader, got)
	}

	outcomes := map[string]string{}
	for _, in := range all {
		outcomes[in.AliasLabel] = in.Outcome
	}
	for label, want := range map[string]string{
		"validated-one":     "validated",
		"unqueried-one":     "unqueried",
		"abandoned-one":     "abandoned",
		"live-one":          "live",
		"pending-one":       "pending",
		"abandoned-queried": "abandoned",
	} {
		if outcomes[label] != want {
			t.Errorf("%s: outcome %q, want %q", label, outcomes[label], want)
		}
	}

	// The filter that earns its place: the rows nobody ever queried are the
	// list of customer CNAMEs to go and fix.
	rec = reader.do(http.MethodGet, path+"?unqueried=true", "")
	var unqueried []interactionView
	decode(t, rec, &unqueried)
	if len(unqueried) != 3 {
		t.Fatalf("unqueried=true returned %d rows, want the three with no queries: %+v", len(unqueried), unqueried)
	}
	for _, in := range unqueried {
		if in.QueryCount != 0 {
			t.Errorf("unqueried=true returned %s with %d queries", in.AliasLabel, in.QueryCount)
		}
	}
	if got := totalCount(t, rec); got != 3 {
		t.Errorf("%s = %d under the filter, want 3", totalCountHeader, got)
	}

	// The total stays the filtered count when a page is asked for.
	rec = reader.do(http.MethodGet, path+"?limit=2", "")
	decode(t, rec, &all)
	if len(all) != 2 || totalCount(t, rec) != 6 {
		t.Errorf("limit=2: %d rows and total %s, want 2 rows of 6", len(all), rec.Header().Get(totalCountHeader))
	}
	if rec := reader.do(http.MethodGet, path+"?limit=501", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("limit=501: %d, want 400", rec.Code)
	}

	// The free-text filter reaches the fields the screen shows.
	rec = reader.do(http.MethodGet, path+"?q=live-one.example.com", "")
	decode(t, rec, &all)
	if len(all) != 1 || all[0].AliasLabel != "live-one" {
		t.Fatalf("q over the domain names returned %+v, want the one row", all)
	}
}

// TestDomainCreateRefusesASuppliedAliasLabel is design A5: the label is
// system configuration, so a request that still carries the removed
// `aliasLabel` field is refused outright rather than having it silently
// dropped — a script that used to set it learns the first time it runs.
func TestDomainCreateRefusesASuppliedAliasLabel(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	proj := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	rec := cl.do(http.MethodPost, proj+"/domains", `{"name":"shop.example.com","aliasLabel":"shop-alias"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s, want 400 for an unknown aliasLabel field", rec.Code, rec.Body.String())
	}

	// And the service reports the label it chose itself.
	rec = cl.do(http.MethodPost, proj+"/domains", `{"name":"shop.example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s, want 201", rec.Code, rec.Body.String())
	}
	var dom domainCreated
	decode(t, rec, &dom)
	if dom.AliasLabel == "shop-alias" {
		t.Fatal("the refused label was used anyway")
	}
	if err := dnsname.ValidateLabel(dom.AliasLabel); err != nil {
		t.Errorf("assigned label %q is not a valid label: %v", dom.AliasLabel, err)
	}
}

// --- includeWildcard -------------------------------------------------------

// domainNamesOn lists what the load balancer holds, which is how "neither
// domain was created" is checked: a refusal that left a row behind is the
// failure this whole feature is designed around.
func domainNamesOn(t *testing.T, cl *client, projPath string) []string {
	t.Helper()
	var listed []domainView
	decode(t, cl.do(http.MethodGet, projPath+"/domains", ""), &listed)
	out := make([]string, 0, len(listed))
	for _, d := range listed {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

// TestDomainCreateIncludeWildcard covers the request field end to end: it
// creates the pair against one alias, it mirrors for a wildcard input, and its
// absence still creates exactly one domain (design B1, B2, B4).
func TestDomainCreateIncludeWildcard(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{"apex asks for its wildcard", `{"name":"shop.example.com","includeWildcard":true}`,
			[]string{"*.shop.example.com", "shop.example.com"}},
		{"wildcard asks for its apex", `{"name":"*.shop.example.com","includeWildcard":true}`,
			[]string{"*.shop.example.com", "shop.example.com"}},
		{"the field is absent", `{"name":"shop.example.com"}`,
			[]string{"shop.example.com"}},
		{"the field is false", `{"name":"shop.example.com","includeWildcard":false}`,
			[]string{"shop.example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newAdminEnv(t)
			cl := v.session("admin", roleAdmin)
			projPath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

			rec := cl.do(http.MethodPost, projPath+"/domains", tc.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d %s, want 201", rec.Code, rec.Body.String())
			}
			var got domainCreated
			decode(t, rec, &got)

			if len(got.Domains) != len(tc.want) {
				t.Fatalf("response reports %d domains, want %d: %+v", len(got.Domains), len(tc.want), got.Domains)
			}
			// One alias label, shared: the pair costs no second thing for the
			// customer to publish.
			for _, d := range got.Domains {
				if d.AliasLabel != got.AliasLabel || d.AliasID != got.AliasID {
					t.Errorf("domain %q has alias %d/%q, want the shared %d/%q",
						d.Name, d.AliasID, d.AliasLabel, got.AliasID, got.AliasLabel)
				}
			}
			if got.Domains[0].Name != got.Name {
				t.Errorf("first reported domain is %q, want the submitted %q", got.Domains[0].Name, got.Name)
			}
			if listed := domainNamesOn(t, cl, projPath); !reflect.DeepEqual(listed, tc.want) {
				t.Errorf("inventory holds %v, want %v", listed, tc.want)
			}
		})
	}
}

// TestDomainCreateIncludeWildcardRefusesTheWholeRequest is decision B3 through
// the API. `co.uk` is a valid plain name whose counterpart `*.co.uk` sits at a
// public suffix; the request is refused naming that counterpart, and the valid
// half is not created either.
func TestDomainCreateIncludeWildcardRefusesTheWholeRequest(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	projPath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)

	// The premise: without the flag, `co.uk` is accepted.
	rec := cl.do(http.MethodPost, projPath+"/domains", `{"name":"co.uk","includeWildcard":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "*.co.uk") {
		t.Errorf("error %s does not name the refused counterpart *.co.uk", rec.Body.String())
	}
	if listed := domainNamesOn(t, cl, projPath); len(listed) != 0 {
		t.Fatalf("inventory holds %v after the refusal, want nothing — not even the valid half", listed)
	}
}

// TestDomainPairIsAuditedTwice: two domains are created, so the trail records
// two objects, both from the transaction that wrote the rows (design B1).
func TestDomainPairIsAuditedTwice(t *testing.T) {
	v := newAdminEnv(t)
	cl := v.session("admin", roleAdmin)
	r := &auditRecorder{cl: cl, auditAt: "/api/v1/accounts/" + v.sc.AccountID().String() + "/audit"}
	projPath := "/api/v1/accounts/" + v.sc.AccountID().String() + "/projects/" + itoa(v.projID)
	pid := &v.projID

	r.step(t, http.MethodPost, projPath+"/domains", `{"name":"shop.example.com","includeWildcard":true}`,
		http.StatusCreated,
		wantEntry{authz.ActionDomainCreate, "shop.example.com", pid},
		wantEntry{authz.ActionDomainCreate, "*.shop.example.com", pid},
		// One alias for the pair, so one alias entry.
		wantEntry{authz.ActionAliasCreate, anything, pid})

	// A refused pair writes nothing at all — the transaction rolled back.
	r.step(t, http.MethodPost, projPath+"/domains", `{"name":"co.uk","includeWildcard":true}`,
		http.StatusBadRequest)
}
