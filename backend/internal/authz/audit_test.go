package authz

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", for schema setup only
	"github.com/labstack/echo/v4"
	dto "github.com/prometheus/client_model/go"
	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

func TestConsoleActorCarriesGroupsAndRoles(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	req.Header.Set(echo.HeaderXRequestID, "req-42")
	req.RemoteAddr = "192.0.2.9:1234"
	c := e.NewContext(req, httptest.NewRecorder())
	c.Set(SessionContextKey, sessionIn("acme-admins", "acme-viewers"))
	c.Set(RolesContextKey, testMapping.RolesFor([]string{"acme-admins", "acme-viewers"}))

	a := ConsoleActor(c)

	if a.Subject != "u-1" || a.DisplayName != "Ada" {
		t.Errorf("actor = %+v, want subject u-1 / Ada", a)
	}
	if !slices.Equal(a.Groups, []string{"acme-admins", "acme-viewers"}) {
		t.Errorf("groups = %v", a.Groups)
	}
	// Both halves of "how was this person allowed to do that": the groups the
	// provider asserted, and the roles the mapping turned them into.
	if got := a.Roles.List(); !slices.Equal(got, []string{"ADMIN", "READ_ONLY"}) {
		t.Errorf("roles = %v, want [ADMIN READ_ONLY]", got)
	}
	if a.Source != SourceConsole || a.RequestID != "req-42" || a.ClientIP != "192.0.2.9" {
		t.Errorf("actor = %+v", a)
	}
}

func TestScrubRemovesSecretsAtAnyDepth(t *testing.T) {
	before, err := scrub(map[string]any{
		"identifier": "k_abc",
		"secret":     "s3cr3t-value",
		"SecretHash": "$argon2id$deadbeef",
		"nested": map[string]any{
			"refresh_token": "rt-value",
			"label":         "prod key",
		},
		"list": []any{
			map[string]any{"password": "hunter2", "user": "ada"},
		},
		"session_id": "sid-value",
	})
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}

	body := string(before)
	for _, leaked := range []string{"s3cr3t-value", "deadbeef", "rt-value", "hunter2", "sid-value"} {
		if strings.Contains(body, leaked) {
			t.Errorf("scrubbed payload still contains %q: %s", leaked, body)
		}
	}
	// Everything that is not secret survives — a redacting audit trail that
	// records nothing is no better than no audit trail.
	for _, kept := range []string{"k_abc", "prod key", "ada"} {
		if !strings.Contains(body, kept) {
			t.Errorf("scrubbed payload lost %q: %s", kept, body)
		}
	}
}

func TestScrubNilStaysNil(t *testing.T) {
	got, err := scrub(nil)
	if err != nil || got != nil {
		t.Fatalf("scrub(nil) = %v, %v", got, err)
	}
}

func TestScrubHandlesStructs(t *testing.T) {
	type key struct {
		Identifier string `json:"identifier"`
		Secret     string `json:"secret"`
	}
	got, err := scrub(key{Identifier: "k_1", Secret: "nope"})
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if strings.Contains(string(got), "nope") {
		t.Errorf("struct field leaked: %s", got)
	}
}

// TestIntegrationRecordWritesAnAttributableRow is the round trip: it proves the
// text[] and inet columns take what Record hands them, which no in-memory test
// can. It skips without TEST_DATABASE_URL (design D13).
func TestIntegrationRecordWritesAnAttributableRow(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()

	actor := Actor{
		Subject:     "u-1",
		DisplayName: "Ada",
		Groups:      []string{"acme-keys"},
		Roles:       testMapping.RolesFor([]string{"acme-keys"}),
		Source:      SourceConsole,
		RequestID:   "req-42",
		ClientIP:    "192.0.2.9",
	}
	keyID := int64(7)
	err := Record(ctx, db, nil, actor, Event{
		Action:      "create",
		ObjectType:  "api_key",
		ObjectID:    &keyID,
		ObjectLabel: "k_abc (acme-prod)",
		After:       map[string]any{"identifier": "k_abc", "secret": "s3cr3t-value"},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	entries, err := store.ListAuditEntries(ctx, db, store.AuditFilter{ObjectType: "api_key"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]

	if e.ActorSubject == nil || *e.ActorSubject != "u-1" || e.ActorName == nil || *e.ActorName != "Ada" {
		t.Errorf("actor = %v / %v", e.ActorSubject, e.ActorName)
	}
	if !slices.Equal(e.ActorGroups, []string{"acme-keys"}) {
		t.Errorf("groups = %v", e.ActorGroups)
	}
	if !slices.Equal(e.ActorRoles, []string{"KEY_ROTATION"}) {
		t.Errorf("roles = %v — without them the entry cannot answer how this was allowed", e.ActorRoles)
	}
	if e.ClientIP == nil || *e.ClientIP != "192.0.2.9" || e.RequestID == nil || *e.RequestID != "req-42" {
		t.Errorf("client ip = %v, request id = %v", e.ClientIP, e.RequestID)
	}
	if strings.Contains(string(e.After), "s3cr3t-value") {
		t.Errorf("the audit row contains the key secret: %s", e.After)
	}
	var after map[string]any
	if err := json.Unmarshal(e.After, &after); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	if after["identifier"] != "k_abc" {
		t.Errorf("after = %v, want the key identifier preserved", after)
	}
}

// TestIntegrationRecordCountsTheEntryByActionClass pins the counter to the
// class of the action written, not to the caller: the two questions an audit
// trail answers ("who changed the setup", "who touched the credentials") are
// asked by different people, so a config action landing in the credential
// series is a wrong answer, not a rounding error. An action outside the
// vocabulary is counted too, under "unknown" — invisible entries would hide
// exactly the drift this catches.
func TestIntegrationRecordCountsTheEntryByActionClass(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	m := obs.New()

	for _, action := range []string{ActionProjectCreate, ActionProjectDelete, ActionKeyRevoke, "delte"} {
		if err := Record(ctx, db, m, SystemActor(), Event{Action: action, ObjectType: ObjectProject}); err != nil {
			t.Fatalf("record %s: %v", action, err)
		}
	}

	for _, tc := range []struct {
		class string
		want  float64
	}{
		{obs.ActionClassConfig, 2},
		{obs.ActionClassCredential, 1},
		{obs.ActionClassUnknown, 1},
	} {
		var got dto.Metric
		if err := m.AuditEntries.WithLabelValues(tc.class).Write(&got); err != nil {
			t.Fatal(err)
		}
		if v := got.GetCounter().GetValue(); v != tc.want {
			t.Errorf("action_class=%s count = %v, want %v", tc.class, v, tc.want)
		}
	}
}

func TestIntegrationRecordSystemActorHasNoIdentity(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()

	if err := Record(ctx, db, nil, SystemActor(), Event{
		Action: "expire", ObjectType: "challenge", ObjectLabel: "_acme-challenge.example.com",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	entries, err := store.ListAuditEntries(ctx, db, store.AuditFilter{ObjectType: "challenge"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if e := entries[0]; e.ActorSubject != nil || len(e.ActorRoles) != 0 || e.ActorSource != SourceSystem {
		t.Errorf("system entry = %+v", e)
	}
}

const testDatabaseURLEnv = "TEST_DATABASE_URL"

// migratedDB gives each test its own migrated schema, dropped afterwards.
// ponytail: a third near-copy of the helper in internal/store, which lives in a
// _test.go file and so cannot be imported. Upgrade path, now overdue: move it
// into an internal/storetest package and delete all three.
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
	db, err := store.Open(store.Options{DatabaseURL: dsn, Logger: slog.New(slog.DiscardHandler)})
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
