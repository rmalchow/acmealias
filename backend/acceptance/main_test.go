//go:build acceptance

// Package acceptance drives the repository's own development stack
// (development/compose.yaml, `acme` profile) through a real DNS-01 issuance
// and asserts the things no script asserts today.
//
// There is deliberately NO second environment: see
// openspec/changes/add-acceptance-environment/design.md A1. The stack is
// driven through its own front door, ./scripts/dev.sh, because those scripts
// already know how to extract pebble's CA, migrate, seed and capture the API
// key — a second orchestrator written in Go would drift from them.
//
// Everything here runs on the HOST and reaches the stack through its published
// ports: the console on :80 by name, our authoritative DNS on :8153, the fake
// internet's CoreDNS on :8053, pebble's management API on :15000, and the
// customer-like load balancer on :443.
package acceptance

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Where the stack is reachable from the host, and what the seed put in it.
// These mirror development/.env, development/compose.yaml and
// development/scripts/seed.sh; if any of them changes, this list changes with
// it, and that coupling is the point (design A1).
const (
	consoleURL   = "http://console.localhost"
	challengeURL = "http://acme-challenge.localhost"
	serviceDNS   = "127.0.0.1:8153" // the api container's authoritative listener
	fakeNetDNS   = "127.0.0.1:8053" // CoreDNS, playing the rest of the world
	pebbleMgmt   = "https://127.0.0.1:15000"
	lbAddr       = "127.0.0.1:443" // traefik-lb, the customer-like proxy

	demoDomain     = "demo.example.com"
	wildcardDomain = "*.demo.example.com"
	// One challenge name for both domains, hence one alias, hence one CNAME.
	// This is the invariant the whole design turns on (D6).
	challengeName = "_acme-challenge.demo.example.com."

	adminUser       = "admin-user"
	configAdminUser = "config-admin-user"
	keyRotationUser = "key-rotation-user"
	readOnlyUser    = "read-only-user"
	devPassword     = "devpass"
)

// The alias the seeded inventory actually got, discovered at start-up rather
// than assumed: with the shipped default (ACME_ALIAS_USE_DOMAIN unset) the
// label is randomly generated, so a fresh seed picks a new one every time.
// The source of truth read here is the customer-side delegation the fake
// internet serves — the same CNAME a CA would follow — which is what
// development/scripts/seed.sh generated from the alias the service created.
var (
	aliasFQDN  string // "jxet64y6bngq4j37.dev.acmeservice.internal."
	aliasLabel string // "jxet64y6bngq4j37"
)

// discoverAlias resolves the challenge name through the fake internet and
// records the single CNAME target it finds. More than one would mean the seed
// grew a second alias for the same challenge name, which the design forbids.
func discoverAlias() error {
	r, err := query(fakeNetDNS, challengeName, dns.TypeCNAME)
	if err != nil {
		return err
	}
	var targets []string
	for _, rr := range r.Answer {
		if c, ok := rr.(*dns.CNAME); ok {
			targets = append(targets, c.Target)
		}
	}
	if len(targets) != 1 {
		return fmt.Errorf("%s: got CNAME targets %v, want exactly one "+
			"(development/scripts/seed.sh publishes one per alias)", challengeName, targets)
	}
	aliasFQDN = targets[0]
	aliasLabel, _, _ = strings.Cut(aliasFQDN, ".")
	return nil
}

// aliasNameFor is the authoritative name one alias answers TXT at.
//
// The zone suffix is taken from the alias discovered above rather than written
// out, for the same reason the label is: it comes from configuration
// (ACME_DNS_ZONE) and a second copy here would be a second thing to keep in
// step. Tests that create their own inventory — the personal and team arcs —
// need this because their alias is not the seeded one the fake internet
// delegates to; they query our authoritative listener directly, which is where
// challenge values live in any case (CoreDNS does not chase the CNAME).
func aliasNameFor(label string) string {
	return label + "." + strings.TrimPrefix(aliasFQDN, aliasLabel+".")
}

// repoDir is the checkout root. The suite lives at backend/acceptance, so the
// scripts are two levels up.
var repoDir = func() string {
	d, err := filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	return d
}()

// keepStack is ACCEPTANCE_KEEP_STACK=1: skip both the reset and the teardown
// and run against whatever is already up. A full reset is minutes, and an
// iterating developer should not pay it per run.
var keepStack = os.Getenv("ACCEPTANCE_KEEP_STACK") == "1"

func TestMain(m *testing.M) {
	if !keepStack {
		// The order matters and is not interchangeable. reset.sh runs up.sh
		// WITHOUT --acme, so after it there is no pebble and no traefik-lb —
		// but it does run seed.sh, which is what writes
		// development/data/acme-key.env, the API key traefik-lb authenticates
		// to /present with. So: reset first (database, blueprint, fixtures, key),
		// then up --acme (fake CA and customer-like load balancer, which reads
		// that key file as an env_file at start).
		mustRun("./scripts/dev.sh", "reset", "--yes")
		mustRun("./scripts/dev.sh", "up", "--acme")
	}
	if err := waitUsable(6 * time.Minute); err != nil {
		fmt.Fprintf(os.Stderr, "stack never became usable: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// `down`, never `destroy`: a failed run's containers and logs are the only
	// debugging material there is, and wiping them is the operator's call.
	if !keepStack {
		mustRun("./scripts/dev.sh", "down")
	}
	os.Exit(code)
}

// --- shelling out ----------------------------------------------------------

func mustRun(name string, args ...string) {
	if out, err := run(name, args...); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s failed: %v\n%s\n", name, strings.Join(args, " "), err, out)
		os.Exit(1)
	}
}

// run executes a command from the repository root with a generous timeout: a
// cold `reset` builds the api image, runs `npm ci` and boots authentik.
func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// compose runs `docker compose` against the dev stack with the `acme` profile
// always on, the way development/scripts/lib.sh's compose_all does.
func compose(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"compose", "-f", filepath.Join(repoDir, "development/compose.yaml"), "--profile", "acme"}, args...)
	out, err := run("docker", full...)
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(full, " "), err, out)
	}
	return out
}

// --- readiness -------------------------------------------------------------

// waitUsable blocks until every published surface the suite touches answers.
// up.sh already waits for postgres and authentik to be ready and applies the
// migrations; what is left is the api's own listeners and the acme profile.
func waitUsable(timeout time.Duration) error {
	checks := map[string]func() error{
		"admin api": func() error { return expectStatus(consoleURL+"/api/v1/me", http.StatusUnauthorized) },
		// The identity provider being CONFIGURED, not merely up: authentik's
		// worker applies blueprints after the server starts answering, and in
		// that window every sign-in fails with "client not found" (design K2).
		"identity provider": func() error {
			return expectStatus("http://auth.localhost/application/o/acmeservice-console/.well-known/openid-configuration", http.StatusOK)
		},
		"authoritative dns": func() error {
			_, err := query(serviceDNS, "dev.acmeservice.internal.", dns.TypeSOA)
			return err
		},
		"fake internet dns": discoverAlias,
		"pebble management": func() error { _, _, err := pebbleChain(); return err },
		"traefik-lb":        func() error { return dialable(lbAddr) },
	}
	deadline := time.Now().Add(timeout)
	for name, check := range checks {
		var err error
		for {
			if err = check(); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s: %w", name, err)
			}
			time.Sleep(2 * time.Second)
		}
	}
	return settle(deadline)
}

// settle waits for the issuance traefik-lb starts BY ITSELF.
//
// This is not optional and it is not obvious. traefik-lb orders a certificate
// the moment it starts, so `up --acme` on a fresh stack already has an ACME
// exchange in flight — and the first thing this suite does is restart
// traefik-lb to force one of its own. Restarting it mid-order kills the client
// between /present and /cleanup, and the two orphaned challenge values then sit
// in the store until they expire, failing the cleanup assertion (2.3) and the
// "nothing became answerable" assertions (2.4) for a reason that has nothing to
// do with the code under test.
//
// So: wait for that first order to finish, and then for its values to be
// withdrawn, before any test runs.
func settle(deadline time.Time) error {
	for !issuedByPebble(demoDomain) {
		if time.Now().After(deadline) {
			return fmt.Errorf("traefik-lb never obtained a certificate on start — " +
				"is development/data/acme-key.env populated? (./scripts/dev.sh seed)")
		}
		time.Sleep(2 * time.Second)
	}
	for {
		vals, _, err := txtValues(serviceDNS, aliasFQDN)
		if err == nil && len(vals) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("challenge values are still answerable at %s after the "+
				"stack's own start-up issuance: %v", aliasFQDN, vals)
		}
		time.Sleep(2 * time.Second)
	}
}

func dialable(addr string) error {
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	return c.Close()
}

func expectStatus(url string, want int) error {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return fmt.Errorf("%s: got %d, want %d", url, resp.StatusCode, want)
	}
	return nil
}

// --- dns -------------------------------------------------------------------

// query asks one server one question. No recursion, no search list: every
// name here is fully qualified and every server is authoritative or forwards.
func query(server, name string, qtype uint16) (*dns.Msg, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	c := &dns.Client{Timeout: 5 * time.Second}
	r, _, err := c.Exchange(m, server)
	return r, err
}

// txtValues returns the TXT strings answered for name, and the response code.
func txtValues(server, name string) ([]string, int, error) {
	r, err := query(server, name, dns.TypeTXT)
	if err != nil {
		return nil, 0, err
	}
	var out []string
	for _, rr := range r.Answer {
		if t, ok := rr.(*dns.TXT); ok {
			out = append(out, strings.Join(t.Txt, ""))
		}
	}
	return out, r.Rcode, nil
}

// --- tls -------------------------------------------------------------------

// servedChain returns the certificate chain traefik-lb offers for serverName.
//
// InsecureSkipVerify is set because this is an INSPECTION, not the assertion:
// the point is to look at whatever is being served — including Traefik's
// self-signed "TRAEFIK DEFAULT CERT" fallback — and decide whether issuance has
// completed yet. Every actual verification in this suite goes through
// x509.Certificate.Verify or an http.Client carrying the pebble pool as RootCAs
// (see trust.go and cert_test.go); nothing is ever asserted from here.
func servedChain(serverName string) ([]*x509.Certificate, error) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", lbAddr, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, //nolint:gosec // inspection only; see doc comment
	})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.ConnectionState().PeerCertificates, nil
}

// issuedByPebble reports whether the certificate on the wire came from the fake
// CA rather than from Traefik's own self-signed fallback. This is the same
// signal development/scripts/issue.sh waits on, and for the same reason: the
// acme.json file appears as soon as the ACME account registers, long before any
// certificate is in it, so polling the file reports success mid-order.
func issuedByPebble(serverName string) bool {
	chain, err := servedChain(serverName)
	if err != nil || len(chain) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(chain[0].Issuer.String()), "pebble")
}

// --- forcing an issuance ---------------------------------------------------

// issuance is what one forced issuance leaves behind for the assertions.
type issuance struct {
	start, end time.Time
	// maxTXT is the greatest number of TXT values seen at the shared alias name
	// in a single query while the order was in flight, and values is that
	// widest observation. Two is the wildcard invariant (D6).
	maxTXT int
	values []string
}

var (
	sharedOnce sync.Once
	shared     issuance
	sharedErr  error
)

// forcedIssuance runs exactly one issuance for the whole package and hands the
// same observation to every assertion that needs it. Tests that need their own
// issuance (restart_test.go) call runIssuance directly.
func forcedIssuance(t *testing.T) issuance {
	t.Helper()
	sharedOnce.Do(func() { shared, sharedErr = runIssuance(t, nil) })
	if sharedErr != nil {
		t.Fatalf("the shared issuance failed: %v", sharedErr)
	}
	return shared
}

// runIssuance forces a genuine issuance the way development/scripts/issue.sh
// does, watching our own authoritative listener throughout.
//
// Discarding acme.json first is not optional: Traefik keeps a valid certificate
// for ~90 days and a plain restart finds it, logs "Testing certificate
// renew...", and talks to nobody. Throwing the file away is what makes the next
// start a real order — account registration, DNS-01, /present, propagation
// check, /cleanup, certificate.
//
// midway, when set, is called once the first challenge value has appeared in
// DNS and before the CA has validated it — the window restart_test.go needs.
func runIssuance(t *testing.T, midway func()) (issuance, error) {
	t.Helper()
	res := issuance{start: time.Now()}

	// Traefik writes acme.json as root inside the container, so it is removed
	// through a throwaway root container rather than asking anybody to sudo —
	// exactly what development/scripts/issue.sh does.
	if out, err := run("docker", "run", "--rm",
		"-v", filepath.Join(repoDir, "development/data/traefik_lb")+":/d",
		"alpine:3", "rm", "-f", "/d/acme.json"); err != nil {
		return res, fmt.Errorf("discarding acme.json: %v\n%s", err, out)
	}
	compose(t, "restart", "traefik-lb")

	done := make(chan struct{})
	firstSeen := make(chan struct{})
	var obs sync.WaitGroup
	obs.Add(1)
	go func() {
		defer obs.Done()
		var signalled bool
		for {
			select {
			case <-done:
				return
			default:
			}
			// The service's own listener, directly: CoreDNS's `file` plugin
			// answers the customer's CNAME but does not chase it into our
			// forwarded zone, so the TXT set lives here.
			vals, _, err := txtValues(serviceDNS, aliasFQDN)
			if err == nil && len(vals) > res.maxTXT {
				res.maxTXT = len(vals)
				res.values = vals
			}
			if !signalled && len(vals) > 0 {
				signalled = true
				close(firstSeen)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	if midway != nil {
		select {
		case <-firstSeen:
			midway()
		case <-time.After(3 * time.Minute):
			close(done)
			obs.Wait()
			return res, fmt.Errorf("no challenge value ever appeared at %s", aliasFQDN)
		}
	}

	// Wait on the SERVED certificate's issuer, not on a file: that is the thing
	// that changes exactly when the issuance completes.
	deadline := time.Now().Add(5 * time.Minute)
	for !issuedByPebble(demoDomain) {
		if time.Now().After(deadline) {
			close(done)
			obs.Wait()
			return res, fmt.Errorf("no pebble-issued certificate on %s after 5m — try ./scripts/dev.sh logs traefik-lb", lbAddr)
		}
		time.Sleep(2 * time.Second)
	}
	close(done)
	obs.Wait()
	res.end = time.Now()
	return res, nil
}

// eventually polls f until it returns nil or the timeout expires, and reports
// the last failure. Every wait in this suite is minutes-scale on purpose: a
// real ACME exchange, a container restart and a Go rebuild are all in the way.
func eventually(t *testing.T, timeout time.Duration, what string, f func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var err error
	for {
		if err = f(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: still failing after %s: %v", what, timeout, err)
		}
		time.Sleep(time.Second)
	}
}

// --- seeded inventory ------------------------------------------------------

// Identifiers are UUIDs and are carried as the strings the API answers with.
//
// They are never parsed into a uuid.UUID here: this suite is a client, and a
// client's only business with an identifier is to send back the one it was
// given. Decoding it would add a way for the test to disagree with the service
// about a value it does not interpret.
type projectRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type lbRef struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Slug      string `json:"slug"`
}

// demoProd resolves the seeded project `demo` and its load balancer `prod` to
// the ids the management API addresses them by.
func demoProd(t *testing.T, s *session) (project, lb string) {
	t.Helper()
	var projects []projectRef
	if err := s.getJSON(s.acct("/projects"), &projects); err != nil {
		t.Fatalf("listing projects: %v", err)
	}
	for _, p := range projects {
		if p.Slug != "demo" {
			continue
		}
		var lbs []lbRef
		if err := s.getJSON(s.acct("/projects/"+p.ID+"/loadbalancers"), &lbs); err != nil {
			t.Fatalf("listing load balancers: %v", err)
		}
		for _, l := range lbs {
			if l.Slug == "prod" {
				return p.ID, l.ID
			}
		}
	}
	t.Fatalf("the seeded project demo / load balancer prod is missing — did ./scripts/dev.sh seed run?")
	return "", ""
}

// mustSignIn is signIn with the failure reported against the test.
//
// The session it returns has NO account selected — the state every session now
// begins in (design P19, task 7.5). A test that needs to act on inventory has to
// choose one first: mustSignInSeeded below for the seeded inventory, or
// s.selectInto(t, s.personalAccount(t)) for the account the user simply has.
func mustSignIn(t *testing.T, user, password string) *session {
	t.Helper()
	s, err := signIn(user, password)
	if err != nil {
		t.Fatalf("signing in as %s: %v", user, err)
	}
	return s
}

// mustSignInSeeded signs a user in and puts them to work in the seeded team
// account, holding the role seededRoles gives them.
//
// This is the fixture that replaces "everybody shares one account and their
// group grants them a role" (task 7.1). The three steps are the three things
// that used to be implicit: the person exists (sign-in), they are a member of
// this account with this role (joinAccount — see the note there on why it is
// written rather than requested), and this session is acting in it (selectInto).
//
// Only the seeded account is reached this way, because it is the only account
// nobody can be invited into: it holds the inventory the whole issuance arc runs
// against and its owner is a stand-in nobody can sign in as. Team accounts the
// suite creates itself are joined through the real API — see team_test.go.
func mustSignInSeeded(t *testing.T, user string) *session {
	t.Helper()
	role, ok := seededRoles[user]
	if !ok {
		t.Fatalf("no seeded role for %s; seededRoles covers the four users the blueprint creates", user)
	}
	s := mustSignIn(t, user, devPassword)
	joinAccount(t, seededAccountSlug, s.subject, role)
	s.selectInto(t, s.accountBySlug(t, seededAccountSlug))
	return s
}
