package store

import (
	"database/sql"
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

	// The row graph starts at an account now: projects reference one, and the
	// migration seeds exactly one for this deployment.
	var account model.Account
	if err := db.First(&account).Error; err != nil {
		t.Fatalf("read seeded account: %v", err)
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
