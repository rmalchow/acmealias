package store

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The four tests here are the evidence for design P1 and P20. They are written
// against the claims those decisions make, in the order the claims are made:
// a query cannot be written without a scope; a scoped query cannot reach another
// account; a scoped query that forgets its own predicate still cannot; and the
// setting that makes the last one true cannot leak between pooled connections.

// crossAccountReaders are the exported functions that deliberately take no
// Scope, each because it reads across accounts by construction. The list is the
// point of this test: it should be short, and adding to it should be a decision
// somebody makes on purpose rather than an omission nobody notices.
var crossAccountReaders = map[string]string{
	"ListAllAliases":              "the nameserver answers for every account from one process",
	"ActiveByAlias":               "the zone view is fleet-wide, not per tenant",
	"CountActive":                 "a process-level gauge, not a tenant-facing number",
	"DeleteExpired":               "the expiry sweep is fleet-wide",
	"FindAPIKeyByIdentifier":      "this is what establishes which account a request belongs to",
	"TouchLastUsed":               "runs on the authentication path, against the row establishing the account",
	"FindAliasByChallengeName":    "a challenge name is claimed globally; the account is compared afterwards",
	"AliasIDsByLabel":             "maps observed labels for every account's aliases at once",
	"ApplyQueryObservations":      "applies fleet-wide DNS observations",
	"AbandonStaleInteractions":    "retention over the whole history",
	"DeleteOldClosedInteractions": "retention over the whole history",
	"OpenInteraction":             "written on the publish path from an alias already resolved under a scope",
	"CloseInteraction":            "written on the withdraw path from an alias already resolved under a scope",
	"AppendAuditEntry":            "the entry carries its own account and is written inside the caller's transaction",
	"CrossAccount":                "the escape itself",
	"MembershipsFor":              "resolves which account a person may act in, before any scope exists",
	"AccountsForUser":             "which accounts a person can reach cannot be answered from inside one",
	"FindOrCreateUser":            "a user is global (design P18) and exists before belonging to any account",
	"GetAccount":                  "reads the account a scope names, which a scope cannot confine",
	"CreateAccountWithOwner":      "runs before the account it creates exists",
	"IsMember":                    "answers may-this-person-act-here, which is the question a scope would beg",
	"EnsurePersonalAccount":       "materializes the account a scope would have to name; it runs at sign-in, before one exists",
	"Credit":                      "reachable in an account no scope-taking route will serve — its owner has to be able to pay for it (design C4)",
	"AccountForSelection":         "reads the account a scope is about to be BUILT from; it is the producer, so it cannot take one",
	"AccountOwnedBy":              "checkout's producer: it asks 'the account with this id that this person owns' before any scope exists, and answers not-found for every other case so the route is not an existence oracle",
}

// nonQueries are exported functions that touch no table at all.
var nonQueries = map[string]bool{
	"Open": true, "MigrateUp": true, "MigrateDown": true, "CheckSchemaVersion": true,
	"EmbeddedVersion": true, "ValueDigest": true, "ScopeFor": true, "ScopeForKey": true,
	"ScopeForSelection": true, "ScopeForSlug": true, "DefaultRoles": true,
	// Catalog reads about the connection itself, not about any tenant's rows.
	"CheckTenantIsolation": true, "PoliciesInstalled": true,
	// Sessions are global: a session belongs to a person, not to an account, and
	// exists before any account is selected.
	"CreateSession": true, "FindSession": true, "TouchSession": true,
	"RevokeSession": true, "DeleteSession": true, "DeleteExpiredSessions": true,
	"SelectAccount":          true,
	"ListSessionsForSubject": true,
}

// TestEveryStoreQueryTakesAScope is design P1's "a forgotten scope must not
// compile", asserted rather than assumed. It reads this package's own source, so
// a new query added tomorrow is covered without anyone remembering to add it
// here.
func TestEveryStoreQueryTakesAScope(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	pkg, ok := pkgs["store"]
	if !ok {
		t.Fatal("package store not found in .")
	}

	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			name := fn.Name.Name
			if nonQueries[name] {
				continue
			}
			var takesDB, takesScope bool
			for _, p := range fn.Type.Params.List {
				switch typeName(p.Type) {
				case "*gorm.DB":
					takesDB = true
				case "Scope":
					takesScope = true
				}
			}
			if !takesDB || takesScope {
				continue
			}
			if why, allowed := crossAccountReaders[name]; allowed {
				if why == "" {
					t.Errorf("%s is exempt from scoping with no reason given", name)
				}
				continue
			}
			t.Errorf("%s takes a *gorm.DB and no Scope, and is not a declared cross-account reader.\n"+
				"Either give it a Scope, or add it to crossAccountReaders with the reason it reads across tenants.", name)
		}
	}
}

func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeName(t.X)
	case *ast.SelectorExpr:
		return typeName(t.X) + "." + t.Sel.Name
	}
	return ""
}

// twoAccounts seeds one project, load balancer, alias and domain in each of two
// accounts, and returns a scope for each.
func twoAccounts(t *testing.T) (db *gorm.DB, mine, theirs Scope, myLB, theirLB model.ID) {
	t.Helper()
	// isolatedDB, not scopedDB: these tests are about what the DATABASE enforces,
	// and a superuser or table owner bypasses every policy, so a handle connected
	// that way would pass this file while proving nothing.
	db, mine = isolatedDB(t)
	theirs = secondAccount(t, db, "globex")
	ctx := context.Background()

	seed := func(sc Scope, slug string) model.ID {
		p := &model.Project{Slug: slug, Name: slug}
		if err := CreateProject(ctx, db, sc, p); err != nil {
			t.Fatalf("create project %s: %v", slug, err)
		}
		lb := &model.LoadBalancer{ProjectID: p.ID, Slug: slug + "-lb", Name: slug}
		if err := CreateLoadBalancer(ctx, db, sc, lb); err != nil {
			t.Fatalf("create load balancer %s: %v", slug, err)
		}
		a := &model.Alias{LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge." + slug + ".example.com", Label: slug + "-label"}
		if err := CreateAlias(ctx, db, sc, a); err != nil {
			t.Fatalf("create alias %s: %v", slug, err)
		}
		d := &model.Domain{LoadBalancerID: lb.ID, AliasID: a.ID, Name: slug + ".example.com"}
		if err := CreateDomain(ctx, db, sc, d); err != nil {
			t.Fatalf("create domain %s: %v", slug, err)
		}
		return lb.ID
	}
	return db, mine, theirs, seed(mine, "acme"), seed(theirs, "globex")
}

// TestIntegrationAnotherAccountsObjectIsNotFound covers the spec requirement
// that an operation naming another account's object is refused "as though the
// object did not exist" — the same error, so the endpoint is not an oracle for
// what other tenants hold.
func TestIntegrationAnotherAccountsObjectIsNotFound(t *testing.T) {
	db, mine, _, _, theirLB := twoAccounts(t)
	ctx := context.Background()

	t.Run("read", func(t *testing.T) {
		_, err := GetLoadBalancer(ctx, db, mine, theirLB)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("err = %v, want ErrRecordNotFound", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		ps, err := ListProjects(ctx, db, mine)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.Slug == "globex" {
				t.Fatalf("listing returned another account's project %q", p.Slug)
			}
		}
		if len(ps) != 1 {
			t.Fatalf("listed %d projects, want 1", len(ps))
		}
	})

	t.Run("count", func(t *testing.T) {
		_, total, err := ListProjectsWithLoadBalancersPage(ctx, db, mine, "", 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Fatalf("count = %d, want 1 — the count must use the same predicate as the page", total)
		}
	})

	t.Run("write", func(t *testing.T) {
		err := DeleteLoadBalancer(ctx, db, mine, theirLB)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("err = %v, want ErrRecordNotFound", err)
		}
		// And it is still there.
		if _, err := GetLoadBalancer(ctx, db, Scope{accountID: mustAccount(t, db, "globex")}, theirLB); err != nil {
			t.Fatalf("the other account's load balancer was deleted: %v", err)
		}
	})

	t.Run("the same domain name may be held by both accounts", func(t *testing.T) {
		// P8: uniqueness is per account, not global. If it were global, the first
		// account to add a name could deny it to every other one — a denial of
		// service with a database constraint as the weapon.
		theirScope := Scope{accountID: mustAccount(t, db, "globex")}
		for _, sc := range []Scope{mine, theirScope} {
			lbs, err := ListProjects(ctx, db, sc)
			if err != nil || len(lbs) != 1 {
				t.Fatalf("setup: %d projects, err %v", len(lbs), err)
			}
			var lb model.LoadBalancer
			if err := sc.Run(ctx, db, func(tx *gorm.DB) error {
				return sc.Where(tx).First(&lb).Error
			}); err != nil {
				t.Fatalf("read load balancer: %v", err)
			}
			var alias model.Alias
			if err := sc.Run(ctx, db, func(tx *gorm.DB) error {
				return sc.Where(tx).First(&alias).Error
			}); err != nil {
				t.Fatalf("read alias: %v", err)
			}
			d := &model.Domain{LoadBalancerID: lb.ID, AliasID: alias.ID, Name: "shared.example.com"}
			if err := CreateDomain(ctx, db, sc, d); err != nil {
				t.Fatalf("both accounts must be able to hold shared.example.com: %v", err)
			}
		}
	})
}

func mustAccount(t *testing.T, db *gorm.DB, slug string) model.ID {
	t.Helper()
	var a model.Account
	if err := CrossAccount(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Where("slug = ?", slug).First(&a).Error
	}); err != nil {
		t.Fatalf("read account %q: %v", slug, err)
	}
	return a.ID
}

// TestIntegrationAQueryThatForgetsItsPredicateStillReturnsNothing is the test
// for the half of design P20 that the compiler cannot cover. It runs a query
// with a valid Scope and NO ownership predicate — the exact bug a review is
// supposed to catch and sometimes will not — and asserts the database refuses it
// anyway.
func TestIntegrationAQueryThatForgetsItsPredicateStillReturnsNothing(t *testing.T) {
	db, mine, _, myLB, theirLB := twoAccounts(t)
	ctx := context.Background()

	var got []model.LoadBalancer
	err := mine.Run(ctx, db, func(tx *gorm.DB) error {
		// Deliberately NOT sc.Where(tx): this is the forgotten predicate.
		return tx.Find(&got).Error
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 1 || got[0].ID != myLB {
		ids := make([]string, 0, len(got))
		for _, lb := range got {
			ids = append(ids, lb.ID.String())
		}
		t.Fatalf("an unfiltered SELECT inside a scope returned %d rows (%s), want only %s.\n"+
			"The row-level security policy is what is supposed to make this true — check that "+
			"FORCE ROW LEVEL SECURITY survived the migration.", len(got), strings.Join(ids, ", "), myLB)
	}
	for _, lb := range got {
		if lb.ID == theirLB {
			t.Fatal("the other account's load balancer was visible to an unfiltered query")
		}
	}
}

// TestIntegrationScopeDoesNotSurviveAPooledConnection is the failure mode the
// transaction in Scope.Run exists to prevent: a tenant setting left on a
// connection that is handed to the next request. SET LOCAL is discarded at
// commit, so borrowing the same connection again must see nothing.
func TestIntegrationScopeDoesNotSurviveAPooledConnection(t *testing.T) {
	db, mine, theirs, myLB, theirLB := twoAccounts(t)
	ctx := context.Background()

	// Pin the pool to a single connection, so the second read is guaranteed to
	// get the one the first read used. Without this the test could pass by
	// accident on a fresh connection.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if _, err := GetLoadBalancer(ctx, db, mine, myLB); err != nil {
		t.Fatalf("first read: %v", err)
	}
	// The same connection now serves the other account.
	if _, err := GetLoadBalancer(ctx, db, theirs, theirLB); err != nil {
		t.Fatalf("second read on the recycled connection: %v — a residual app.account_id "+
			"from the first read would produce exactly this", err)
	}
	// And with no scope at all, the connection must be inert rather than
	// carrying whichever account happened to use it last.
	var leaked []model.LoadBalancer
	if err := db.WithContext(ctx).Find(&leaked).Error; err != nil {
		t.Fatalf("unscoped read: %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("a connection outside any scope returned %d load balancers, want 0 — "+
			"the tenant setting survived its transaction", len(leaked))
	}
}

// TestIntegrationChallengesInheritIsolationFromTheirAlias covers the one policy
// in the schema that reaches through a join rather than reading a column.
//
// challenges carries no account_id and cannot: it is written on the certificate
// hot path and its parent is what identifies the tenant. So its policy asks "is
// my alias visible", and the ANSWER to that runs under the caller's own
// policies — which means the aliases policy applies inside it and the tenant
// filter propagates down. A chain, not a cycle, so nothing recurses.
//
// What this asserts is that the propagation is real: a query with a valid scope
// and no predicate of its own sees only its own account's challenge values.
func TestIntegrationChallengesInheritIsolationFromTheirAlias(t *testing.T) {
	db, mine, theirs, _, _ := twoAccounts(t)
	ctx := context.Background()

	// One published value in each account, against that account's own alias.
	publish := func(sc Scope, value string) {
		t.Helper()
		var a model.Alias
		if err := sc.Run(ctx, db, func(tx *gorm.DB) error {
			return sc.Where(tx).First(&a).Error
		}); err != nil {
			t.Fatalf("read alias: %v", err)
		}
		if _, err := Present(ctx, db, sc, a.ID, value, "k_test", time.Minute); err != nil {
			t.Fatalf("present %q: %v", value, err)
		}
	}
	publish(mine, "mine")
	publish(theirs, "theirs")

	// Deliberately no predicate: the policy is the whole of the filtering.
	var got []model.Challenge
	if err := mine.Run(ctx, db, func(tx *gorm.DB) error {
		return tx.Find(&got).Error
	}); err != nil {
		t.Fatalf("read challenges: %v", err)
	}
	if len(got) != 1 || got[0].Value != "mine" {
		values := make([]string, 0, len(got))
		for _, c := range got {
			values = append(values, c.Value)
		}
		t.Fatalf("an unfiltered read inside one scope saw %v, want only [mine].\n"+
			"The policy on challenges is inherited through aliases; check that both are forced.", values)
	}
}

// A publish into another account's alias is refused by the policy, not merely by
// the caller remembering to check.
func TestIntegrationPublishIntoAnotherAccountsAliasIsRefused(t *testing.T) {
	db, mine, theirs, _, _ := twoAccounts(t)
	ctx := context.Background()

	var theirAlias model.Alias
	if err := theirs.Run(ctx, db, func(tx *gorm.DB) error {
		return theirs.Where(tx).First(&theirAlias).Error
	}); err != nil {
		t.Fatalf("read their alias: %v", err)
	}

	if _, err := Present(ctx, db, mine, theirAlias.ID, "smuggled", "k_test", time.Minute); err == nil {
		t.Fatal("a value was published into another account's alias")
	}
}

// TestIntegrationCrossAccountDoesNotOutliveItsNesting is the fourth way the
// tenant filter could be off without anybody having turned it off.
//
// CrossAccount lifts the policies with `set_config(..., true)` — transaction
// local. Called on an already-open *gorm.DB it joins as a nested transaction,
// which gorm implements as a savepoint, and PostgreSQL does NOT undo a SET LOCAL
// at RELEASE SAVEPOINT. So a cross-account read nested inside a mutation left
// the policies down for the rest of the OUTER transaction: every statement
// after it, in a transaction the caller believed was confined.
//
// This change was the first to nest one (transferAccount and checkout both do,
// through store.TransferOwnership and store.Credit). Nothing leaked, because
// both carry their own account_id predicates — which is exactly the reassurance
// that stops being true the day somebody adds a statement that does not.
func TestIntegrationCrossAccountDoesNotOutliveItsNesting(t *testing.T) {
	db, mine, theirs, _, _ := twoAccounts(t)
	ctx := context.Background()

	var seenInside, seenAfter []model.LoadBalancer
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := mine.Run(ctx, tx, func(tx *gorm.DB) error { return nil }); err != nil {
			return err
		}
		// The escape, nested inside the caller's transaction.
		if err := CrossAccount(ctx, tx, func(tx *gorm.DB) error {
			return tx.Find(&seenInside).Error
		}); err != nil {
			return err
		}
		// A query with no predicate of its own, after it. The policy is what
		// has to answer this, and it has to answer with one account's rows.
		return tx.Find(&seenAfter).Error
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if len(seenInside) < 2 {
		t.Fatalf("the nested cross-account read saw %d load balancers, want both accounts' — "+
			"the escape did not take effect, so the assertion below proves nothing", len(seenInside))
	}
	if len(seenAfter) != 1 {
		t.Fatalf("after a nested cross-account read, a predicate-less query saw %d load balancers, want 1: "+
			"the policies were still lifted for the rest of the transaction", len(seenAfter))
	}
	_ = theirs
}
