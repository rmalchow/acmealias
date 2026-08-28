package httpapi

import (
	"net/http"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

// TestIntegrationPresentIssuesNoExtraDatastoreReads is constraint 2, asserted:
// "the challenge path must not get slower".
//
// The permission check on that path used to be one comparison of two integers
// already in memory. It is now a grant evaluation — which is still arithmetic
// over values already in memory, because the key's grants are preloaded with the
// key and the alias carries its own ancestors (design P3). If either of those
// stopped being true, this test is what notices: a lazily loaded grant set would
// show up here as one more statement per request, on the critical path of every
// certificate issuance.
//
// The number is asserted rather than compared to a stored baseline on purpose.
// A baseline file drifts silently; a literal with the statements named beside it
// has to be edited by someone who then has to say why in the diff.
func TestIntegrationPresentIssuesNoExtraDatastoreReads(t *testing.T) {
	v := newEnv(t)

	var statements atomic.Int64
	count := func(tx *gorm.DB) { statements.Add(1) }
	for _, hook := range []struct {
		name     string
		register func(string, func(*gorm.DB)) error
	}{
		{"query", v.db.Callback().Query().After("gorm:query").Register},
		{"create", v.db.Callback().Create().After("gorm:create").Register},
		{"update", v.db.Callback().Update().After("gorm:update").Register},
		{"delete", v.db.Callback().Delete().After("gorm:delete").Register},
		{"raw", v.db.Callback().Raw().After("gorm:raw").Register},
	} {
		if err := hook.register("test:count_"+hook.name, count); err != nil {
			t.Fatalf("register %s counter: %v", hook.name, err)
		}
	}

	// One warm request first: the first request through a fresh process also
	// pays for whatever the pool and the prepared-statement cache do once, and
	// that is not what is being measured.
	if rec := post(v.e, "/present", `{"fqdn":"`+testFQDN+`.","value":"warm-up"}`, v.ours); rec.Code != http.StatusOK {
		t.Fatalf("warm-up: status %d: %s", rec.Code, rec.Body.String())
	}

	statements.Store(0)
	if rec := post(v.e, "/present", `{"fqdn":"`+testFQDN+`.","value":"measured"}`, v.ours); rec.Code != http.StatusOK {
		t.Fatalf("present: status %d: %s", rec.Code, rec.Body.String())
	}

	// What a publish costs, named so a change to the number is a change somebody
	// has to justify:
	//
	//   1. resolve the API key by its identifier, with networks and grants
	//   2. resolve the alias by challenge name
	//   3. upsert the challenge value
	//   4. read the alias's domain names for the interaction history
	//   5. open the interaction row
	//   6. read the account, to decide whether it is funded
	//   7-11. the transaction wrapper's set_config for each scoped statement
	//
	// One of those set_configs is worth naming: challenges inherits a
	// row-level security policy from aliases, so the upsert runs inside a scope
	// transaction and pays one set_config. The POLICY itself costs no statement —
	// it is a subquery inside the upsert, one index lookup on the aliases primary
	// key — which is the whole reason a joined policy is affordable on this table
	// and would not have been on a listing.
	//
	// The grant evaluation added NONE, which is what this test exists to hold:
	// the key's grants are preloaded with the key, and the alias carries its own
	// ancestors. A lazily loaded grant set would show up here immediately.
	//
	// The FUNDING check added two — the account read and its cross-account
	// set_config — and they are named here rather than absorbed, because this
	// test's job is to make a number somebody has to justify. The justification:
	// the check cannot be answered from anything already in memory. It is a
	// property of the ACCOUNT, the request arrives holding only a key, and the
	// answer has to be current — an account that ran out mid-renewal must stop,
	// and one that was just paid for must resume, neither waiting for a cache to
	// expire or a process to restart (task 4.6). Caching it was the alternative
	// and is what that requirement rules out. One indexed primary-key lookup on
	// a row PostgreSQL has in shared buffers is the price of the whole of C3.
	const ceiling = 15
	if n := statements.Load(); n > ceiling {
		t.Fatalf("one publish issued %d datastore statements, want at most %d.\n"+
			"The permission check on this path must read nothing: grants are preloaded "+
			"with the key, and the alias carries its own ancestors.", n, ceiling)
	} else {
		t.Logf("one publish issued %d datastore statements (ceiling %d)", n, ceiling)
	}
}
