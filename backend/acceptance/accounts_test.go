//go:build acceptance

package acceptance

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The account fixtures (task 7.1).
//
// WHAT CHANGED AND WHY THIS FILE EXISTS. The suite used to need no account
// fixture at all: a deployment had exactly one account, sign-in selected it, and
// the four seeded users got their authority from an identity-provider group
// mapped onto a global role by four environment variables. Design C10 deleted
// all of that. There is no shared account, no group grants anything, and a
// session begins with nothing selected — so every session in this suite now has
// to say which account it is acting in, and a user has authority in one only
// because there is a row saying so.
//
// Two kinds of account are therefore fixtured here, and they are reached in
// opposite ways:
//
//   - the PERSONAL account, which nobody creates. It is materialized at the
//     user's first sign-in (C1), so the fixture for it is signing in.
//   - the SEEDED TEAM account `internal`, which holds the inventory the whole
//     issuance arc runs against — the `demo` project, its `prod` load balancer,
//     the domains, the alias the fake internet delegates to, and the API key
//     traefik-lb authenticates with. development/scripts/seed.sh creates it
//     through adminctl with a STAND-IN owner, because authentik's provider uses
//     sub_mode: hashed_user_id and a shell script cannot know a person's subject
//     before they have signed in once. So a test user has to be put into it.

const (
	// The account development/scripts/seed.sh creates and every adminctl verb in
	// it acts in; ACME_ADMINCTL_ACCOUNT in development/.env names it. Mirroring
	// the value here is the same deliberate coupling as the ports and the
	// domains at the top of main_test.go (design A1).
	seededAccountSlug = "internal"

	// POSTGRES_USER and POSTGRES_DB from development/.env. This is the
	// superuser the container's entrypoint creates, NOT the role the service
	// connects as — which is the point below.
	postgresUser = "acmeservice"
	postgresDB   = "acmeservice"
)

// seededRoles is what replaces the group-to-role table this suite used to
// assert (task 7.2).
//
// The old fixture was configuration: ACME_ADMIN_GROUP and its three siblings
// named an authentik group per global role, and a user held a role because their
// token carried a claim. The four names survive — they are the default role set
// every new team account is seeded with (design C10, task 2.5) — but they are now
// rows in ONE account, and a user holds one because they are a member of that
// account with that role. The users are still four and still distinguishable,
// which is why development/authentik/blueprints/acmeservice.yaml kept them.
var seededRoles = map[string]string{
	adminUser:       "ADMIN",
	configAdminUser: "CONFIG_ADMIN",
	keyRotationUser: "KEY_ROTATION",
	readOnlyUser:    "READ_ONLY",
}

// accountView is one entry of the picker, as GET /api/v1/accounts answers it.
// Only the fields this suite asserts on are decoded.
type accountView struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Selected        bool   `json:"selected"`
	Owner           bool   `json:"owner"`
	Credits         int64  `json:"credits"`
	Suspended       bool   `json:"suspended"`
	SuspendedReason string `json:"suspendedReason"`
}

// accounts is the picker: the accounts this session's person may act in.
//
// It is reachable with NO account selected, which is the state every session now
// starts in, and it is the route that makes that state navigable rather than
// stuck (design P19).
func (s *session) accounts(t *testing.T) []accountView {
	t.Helper()
	var out []accountView
	if err := s.getJSON("/api/v1/accounts", &out); err != nil {
		t.Fatalf("listing accounts as %s: %v", s.user, err)
	}
	return out
}

// accountBySlug finds one account in the picker, failing with the whole listing
// when it is absent — which is the information a failure actually needs.
func (s *session) accountBySlug(t *testing.T, slug string) accountView {
	t.Helper()
	all := s.accounts(t)
	for _, a := range all {
		if a.Slug == slug {
			return a
		}
	}
	t.Fatalf("%s cannot act in the account %q; the picker offers %v", s.user, slug, slugs(all))
	return accountView{}
}

// personalAccount is the account the user did not create and cannot lose: it is
// materialized at their first sign-in (design C1).
func (s *session) personalAccount(t *testing.T) accountView {
	t.Helper()
	all := s.accounts(t)
	var found []accountView
	for _, a := range all {
		if a.Kind == "personal" {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		// Not "at least one": a user may own exactly one personal account, and
		// the partial unique index of task 1.3 is what enforces it. Two here
		// would mean the index is gone and a concurrent first sign-in produced
		// one each (design C2).
		t.Fatalf("%s has %d personal accounts, want exactly 1; the picker offers %v",
			s.user, len(found), slugs(all))
	}
	return found[0]
}

// selectInto switches the session into one account and records it, so that every
// s.acct(...) address afterwards names the same account the service thinks the
// session is in — the redundancy design P19 asks for.
func (s *session) selectInto(t *testing.T, account accountView) {
	t.Helper()
	if err := s.json(http.MethodPost, "/api/v1/accounts/"+account.ID+"/select",
		nil, http.StatusNoContent, nil); err != nil {
		t.Fatalf("%s selecting %s: %v", s.user, account.Slug, err)
	}
	s.account = account.ID
}

// trySelect is selectInto without the assertion: the status, for the tests whose
// subject is the refusal (a suspended account answers 402).
func (s *session) trySelect(account accountView) (int, []byte, error) {
	return s.do(http.MethodPost, "/api/v1/accounts/"+account.ID+"/select", nil)
}

func slugs(as []accountView) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Slug+"("+a.Kind+")")
	}
	return out
}

// --- reaching around the service, and why it is done twice -----------------

// psql runs one statement in the stack's database as the SUPERUSER, and every
// call is a fixture rather than an assertion.
//
// THIS IS DELIBERATELY THE ONLY DOOR OF ITS KIND, and both of its users below
// exist because the service has no surface for what they do — not because a
// surface would have been inconvenient to drive:
//
//   - a membership cannot be created from outside the account. Adding a member
//     needs an unrestricted member of that account already signed in, and the
//     seeded account's owner is a stand-in subject nobody can sign in as. That
//     gap is what `account-invitations` is for; until it lands, and until
//     adminctl grows a verb for it, there is nothing to drive.
//   - nothing spends credit. The ledger is `metering-and-credits`; a balance
//     today only ever goes up, so an account cannot be brought to zero by using
//     it. internal/httpapi/funding_test.go writes the column for the same reason
//     and says so in the same words.
//
// The superuser is used because PostgreSQL exempts superusers from row-level
// security, and every table here is policied. That is exactly why the SERVICE
// must never connect this way (development/postgres-init/02-create-app-role.sh),
// and why this helper is confined to this file: nothing that is being asserted
// may travel through it.
func psql(t *testing.T, statement string) string {
	t.Helper()
	return strings.TrimSpace(compose(t, "exec", "-T", "postgres",
		"psql", "-qtAX", "-v", "ON_ERROR_STOP=1",
		"-U", postgresUser, "-d", postgresDB, "-c", statement))
}

// joinAccount makes a signed-in user a member of an account with one named role.
//
// It is the fixture form of an invitation: the same three columns
// POST /accounts/{id}/members writes, for the case where nobody who could send
// one can sign in. The user row already exists — sign-in creates it, along with
// the personal account — so this only has to find it.
//
// Idempotent, because a suite that keeps its stack (ACCEPTANCE_KEEP_STACK=1)
// runs it again over yesterday's rows.
func joinAccount(t *testing.T, accountSlug, subject, role string) {
	t.Helper()
	// Quoted values are a slug from a constant, a role name from the table
	// above, and a subject the identity provider generated — none of them
	// reaches here from a request.
	psql(t, fmt.Sprintf(`insert into account_members (account_id, user_id, role_id)
		select a.id, u.id, r.id
		  from accounts a, users u, roles r
		 where a.slug = '%s' and u.subject = '%s'
		   and r.account_id = a.id and r.name = '%s'
		on conflict do nothing`, accountSlug, subject, role))

	// Assert the row is there rather than trusting an insert that selected from
	// three tables: if any of the three matched nothing, the statement above
	// succeeds and inserts nothing, and every assertion downstream then fails
	// somewhere unrelated with a 404 or an empty list.
	got := psql(t, fmt.Sprintf(`select count(*) from account_members m
		  join accounts a on a.id = m.account_id
		  join users u on u.id = m.user_id
		  join roles r on r.id = m.role_id
		 where a.slug = '%s' and u.subject = '%s' and r.name = '%s'`,
		accountSlug, subject, role))
	if got != "1" {
		t.Fatalf("after joining %s to %s as %s the membership count is %q, want 1 — "+
			"the user, the account or the role does not exist (has %s signed in yet? "+
			"does %s still seed the four default roles?)",
			subject, accountSlug, role, got, subject, accountSlug)
	}
}

// setCredits writes an account's balance directly. See psql: nothing spends
// credit yet, so this is the only way to reach zero (design C3).
func setCredits(t *testing.T, accountID string, credits int64) {
	t.Helper()
	psql(t, fmt.Sprintf("update accounts set credits = %d where id = '%s'", credits, accountID))
}

// unique makes a name no other run of this suite has used, for the accounts,
// projects and domains a test creates and cannot always delete.
//
// A run normally starts from a reset stack, where a fixed name would be fine.
// ACCEPTANCE_KEEP_STACK=1 is the case this exists for: an iterating developer
// runs against yesterday's data, and a slug is unique across the whole
// deployment (accounts) or the whole account (projects).
func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%1e9)
}
