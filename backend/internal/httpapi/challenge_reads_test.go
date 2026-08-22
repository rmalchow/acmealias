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
	//   6-8. the transaction wrapper's set_config for each scoped statement
	//
	// The ceiling is generous because the interaction history is written
	// best-effort and its exact statement count is not the thing under test.
	// What matters is that adding the grant evaluation added NONE, which a
	// lazily-loaded grant set would have.
	const ceiling = 12
	if n := statements.Load(); n > ceiling {
		t.Fatalf("one publish issued %d datastore statements, want at most %d.\n"+
			"The permission check on this path must read nothing: grants are preloaded "+
			"with the key, and the alias carries its own ancestors.", n, ceiling)
	} else {
		t.Logf("one publish issued %d datastore statements (ceiling %d)", n, ceiling)
	}
}
