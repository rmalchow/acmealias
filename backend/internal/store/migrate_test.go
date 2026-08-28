package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestEmbeddedVersion(t *testing.T) {
	v, err := EmbeddedVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v < 1 {
		t.Fatalf("embedded version = %d, want at least 1", v)
	}
}

// TestIntegrationMigrationsAreIdempotent covers "migrations applied twice make no
// changes and report no error".
func TestIntegrationMigrationsAreIdempotent(t *testing.T) {
	dsn := freshSchemaURL(t)
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// TestIntegrationMigrateDownReverses populates every table with a realistic row
// graph, then reverts: the down migration must leave the schema empty.
func TestIntegrationMigrateDownReverses(t *testing.T) {
	db, dsn := migratedDB(t)

	// The row graph starts at an account, and the migration seeds none: design
	// C10 removed the edition that had exactly one, so every account is created
	// by a person. The graph therefore begins by making the account and its
	// owner, which is also what a real deployment does.
	owner := model.User{Subject: "sub-owner", DisplayName: "Owner"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	account := model.Account{
		Slug: "acme", Name: "Acme",
		Kind: model.AccountKindTeam, OwnerUserID: owner.ID,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("insert account: %v", err)
	}

	project := model.Project{AccountID: account.ID, Slug: "acme-shop", Name: "Acme Shop"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("insert project: %v", err)
	}
	lb := model.LoadBalancer{AccountID: account.ID, ProjectID: project.ID, Slug: "eu-prod", Name: "EU production"}
	if err := db.Create(&lb).Error; err != nil {
		t.Fatalf("insert load_balancer: %v", err)
	}
	ipv4, ipv6 := "192.0.2.10", "2001:db8::10"
	if err := db.Create(&model.LBNode{AccountID: account.ID, ProjectID: project.ID, LoadBalancerID: lb.ID, Label: "a", IPv4: &ipv4, IPv6: &ipv6}).Error; err != nil {
		t.Fatalf("insert lb_node: %v", err)
	}
	alias := model.Alias{AccountID: account.ID, ProjectID: project.ID, LoadBalancerID: lb.ID, ChallengeFQDN: "_acme-challenge.example.com", Label: "example-com"}
	if err := db.Create(&alias).Error; err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	// example.com and *.example.com share one alias — the wildcard case from design D4.
	for _, name := range []string{"example.com", "*.example.com"} {
		if err := db.Create(&model.Domain{AccountID: account.ID, ProjectID: project.ID, LoadBalancerID: lb.ID, AliasID: alias.ID, Name: name}).Error; err != nil {
			t.Fatalf("insert domain %s: %v", name, err)
		}
	}
	key := model.APIKey{
		AccountID: account.ID, Identifier: "k_7f3a91c4",
		SecretHash: []byte("0123456789abcdef0123456789abcdef"), DisplayPrefix: "AbCdEf",
	}
	if err := db.Create(&key).Error; err != nil {
		t.Fatalf("insert api_key: %v", err)
	}
	if err := db.Create(&model.APIKeyNetwork{APIKeyID: key.ID, CIDR: "192.0.2.0/24"}).Error; err != nil {
		t.Fatalf("insert api_key_network: %v", err)
	}
	publishedBy := key.Identifier
	if err := db.Create(&model.Challenge{
		AliasID: alias.ID, Value: "token-one", PublishedBy: &publishedBy,
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("insert challenge: %v", err)
	}
	if err := db.Create(&model.Session{
		ID: "s_abc", Subject: "sub-1", DisplayName: "Ruben",
		Groups: model.StringArray{"infra-admins", "everyone"}, ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("insert session: %v", err)
	}
	clientIP := "192.0.2.10"
	if err := db.Create(&model.AuditEntry{
		OccurredAt: time.Now(), ActorSource: "console", ClientIP: &clientIP,
		ActorGroups: model.StringArray{"infra-admins"}, ActorRoles: model.StringArray{"ADMIN"},
		Action: "domain.create", ObjectType: "domain", ObjectLabel: "example.com",
		ProjectID: &project.ID, After: []byte(`{"name":"example.com"}`),
	}).Error; err != nil {
		t.Fatalf("insert audit_entry: %v", err)
	}

	// Round-trip the array columns: encoding them is the one non-obvious mapping.
	var session model.Session
	if err := db.First(&session, "id = ?", "s_abc").Error; err != nil {
		t.Fatalf("read session: %v", err)
	}
	if len(session.Groups) != 2 || session.Groups[0] != "infra-admins" {
		t.Errorf("session groups round-tripped as %#v", session.Groups)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.Close() // the down migration drops the schema objects this pool holds

	if err := MigrateDown(dsn); err != nil {
		t.Fatalf("migrate down: %v", err)
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var remaining []string
	rows, err := conn.Query(`select table_name from information_schema.tables
		 where table_schema = current_schema() and table_name <> 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("down migration left tables behind: %v", remaining)
	}
}

// TestIntegrationRefusesDatabaseAhead is the guard golang-migrate does not perform
// itself: a database migrated by a newer binary must stop this one, not be silently
// treated as up to date.
func TestIntegrationRefusesDatabaseAhead(t *testing.T) {
	dsn := freshSchemaURL(t)
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	embedded, err := EmbeddedVersion()
	if err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec("update schema_migrations set version = $1", int64(embedded)+1); err != nil {
		t.Fatalf("fake a newer schema version: %v", err)
	}

	if err := CheckSchemaVersion(dsn); err == nil {
		t.Fatal("CheckSchemaVersion accepted a database ahead of the binary")
	} else if !strings.Contains(err.Error(), "newer") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := MigrateUp(dsn); err == nil {
		t.Fatal("MigrateUp ran against a database ahead of the binary")
	}
}

// TestIntegrationBackfillActivatesExistingDomains covers task 5.5: a deployment
// that already holds domains must find every one of them activated after
// 000002, because those rows predate the rule and refusing their /present calls
// would be a regression dressed as a feature (task 1.3).
//
// IT MIGRATES AS A NON-SUPERUSER OWNER, and that is the whole test. Run this as
// the local superuser and it passes while production breaks: `domains` is FORCE
// row level security, so the migrating role is subject to its own table's
// policy, and a backfill that does not lift it updates zero rows, raises no
// error, and is recorded as applied. Only a superuser or a BYPASSRLS role
// escapes — and store.CheckTenantIsolation refuses to serve as either, so the
// only deployments where an unlifted backfill would work are the ones that
// cannot boot.
func TestIntegrationBackfillActivatesExistingDomains(t *testing.T) {
	dsn := freshSchemaURL(t)
	ownerDSN := ownerRoleURL(t, dsn)

	m, err := openMigrate(ownerDSN)
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	// Stop at the baseline: the domain has to exist BEFORE 000002 runs, which
	// is the situation an upgrade is.
	if err := m.Migrate(1); err != nil {
		t.Fatalf("migrate to the baseline: %v", err)
	}

	db, err := sql.Open("pgx", ownerDSN)
	if err != nil {
		t.Fatalf("open as owner: %v", err)
	}
	defer db.Close()

	var super, bypass bool
	if err := db.QueryRow(
		"select rolsuper, rolbypassrls from pg_roles where rolname = current_user").
		Scan(&super, &bypass); err != nil {
		t.Fatalf("read role attributes: %v", err)
	}
	if super || bypass {
		t.Fatalf("the migrating role is superuser=%v bypassrls=%v; this test proves nothing "+
			"unless it runs as the ordinary owner a deployment actually migrates with", super, bypass)
	}

	seedDomainForBackfill(t, db)

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	total, activated := countDomains(t, db)
	if total == 0 {
		t.Fatal("the seed inserted no domain, so the backfill had nothing to prove itself on")
	}
	if activated != total {
		t.Errorf("%d of %d existing domains were activated by the migration; "+
			"every one of them is now refused 409 on /present until the worker's first pass sees it",
			activated, total)
	}
}

// ownerRoleURL creates an ordinary role that owns the tables it migrates, and
// returns a URL connecting as it. It is the deployment's role: it may create in
// the schema, holds neither SUPERUSER nor BYPASSRLS, and is therefore subject to
// FORCE row-level security on its own tables.
//
// Distinct from appRoleURL, which is the role the SERVICE connects as and which
// owns nothing. Both are needed here: what a migration can see is a question
// about the owner.
func ownerRoleURL(t *testing.T, adminDSN string) string {
	t.Helper()
	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	schema := u.Query().Get("search_path")

	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer admin.Close()

	var buf [4]byte
	rand.Read(buf[:])
	role := "owner_" + hex.EncodeToString(buf[:])
	for _, stmt := range []string{
		"create role " + role + " login password '" + role + "'",
		"grant usage, create on schema " + schema + " to " + role,
	} {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		c, err := sql.Open("pgx", adminDSN)
		if err != nil {
			return
		}
		defer c.Close()
		// The role owns the tables in the schema, so it cannot be dropped until
		// they are gone; the schema itself is dropped by freshSchemaURL's own
		// cleanup, which runs after this one.
		_, _ = c.Exec("drop owned by " + role + " cascade")
		_, _ = c.Exec("drop role " + role)
	})

	ou := *u
	ou.User = url.UserPassword(role, role)
	return ou.String()
}

// seedDomainForBackfill inserts the smallest row graph that reaches a domain:
// user, account, project, load balancer, alias, domain. It writes with the
// policies lifted the way store.CrossAccount does, because the owner is subject
// to them here too — which is the very thing this file is about.
func seedDomainForBackfill(t *testing.T, db *sql.DB) {
	t.Helper()
	withCrossAccount(t, db, func(tx *sql.Tx) {
		for _, stmt := range []string{
			`insert into users (id, subject) values ('99999999-9999-9999-9999-999999999999', 'sub-upgrade')`,
			`insert into accounts (id, slug, name, kind, owner_user_id)
			   values ('11111111-1111-1111-1111-111111111111', 'acme', 'Acme', 'team',
			           '99999999-9999-9999-9999-999999999999')`,
			`insert into projects (id, account_id, slug, name)
			   values ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'shop', 'Shop')`,
			`insert into load_balancers (id, account_id, project_id, slug, name)
			   values ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111',
			           '22222222-2222-2222-2222-222222222222', 'eu', 'EU')`,
			`insert into aliases (id, account_id, project_id, load_balancer_id, label, challenge_fqdn)
			   values ('44444444-4444-4444-4444-444444444444', '11111111-1111-1111-1111-111111111111',
			           '22222222-2222-2222-2222-222222222222', '33333333-3333-3333-3333-333333333333',
			           'example-com', '_acme-challenge.example.com')`,
			`insert into domains (id, account_id, project_id, load_balancer_id, alias_id, name)
			   values ('55555555-5555-5555-5555-555555555555', '11111111-1111-1111-1111-111111111111',
			           '22222222-2222-2222-2222-222222222222', '33333333-3333-3333-3333-333333333333',
			           '44444444-4444-4444-4444-444444444444', 'example.com')`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				t.Fatalf("seed: %v\n%s", err, stmt)
			}
		}
	})
}

// countDomains reports how many domains exist and how many the backfill
// activated, read across accounts for the same reason the seed is written that
// way.
func countDomains(t *testing.T, db *sql.DB) (total, activated int) {
	t.Helper()
	withCrossAccount(t, db, func(tx *sql.Tx) {
		if err := tx.QueryRow("select count(*), count(last_success_at) from domains").
			Scan(&total, &activated); err != nil {
			t.Fatalf("count domains: %v", err)
		}
	})
	return total, activated
}

// withCrossAccount runs fn in a transaction with app.cross_account set locally —
// the same escape store.CrossAccount uses, spelled out here because these
// statements go through database/sql rather than through the store.
func withCrossAccount(t *testing.T, db *sql.DB, fn func(tx *sql.Tx)) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("select set_config('app.cross_account', 'on', true)"); err != nil {
		t.Fatalf("lift row-level security: %v", err)
	}
	fn(tx)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
