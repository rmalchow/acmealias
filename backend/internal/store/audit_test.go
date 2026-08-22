package store

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationAuditEntryAppendAndFilter(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()

	projectID := model.NewID()
	otherProjectID := model.NewID()
	account := sc.AccountID()

	// Every entry names the account it happened inside: a trail is read back
	// through the same confinement as everything else, so an entry with no
	// account is an entry nobody can find.
	entries := []model.AuditEntry{
		{AccountID: &account, Action: "create", ObjectType: "domain", ObjectLabel: "example.com", ProjectID: &projectID, ActorSource: "console"},
		{AccountID: &account, Action: "delete", ObjectType: "domain", ObjectLabel: "shop.example.com", ProjectID: &projectID, ActorSource: "console"},
		{AccountID: &account, Action: "create", ObjectType: "load_balancer", ObjectLabel: "eu-prod", ProjectID: &otherProjectID, ActorSource: "cli"},
	}
	for i := range entries {
		if err := AppendAuditEntry(ctx, db, &entries[i]); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	all, err := ListAuditEntries(ctx, db, sc, AuditFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}

	byProject, err := ListAuditEntries(ctx, db, sc, AuditFilter{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("len(byProject) = %d, want 2", len(byProject))
	}

	byAction, err := ListAuditEntries(ctx, db, sc, AuditFilter{Action: "delete"})
	if err != nil {
		t.Fatalf("list by action: %v", err)
	}
	if len(byAction) != 1 || byAction[0].ObjectLabel != "shop.example.com" {
		t.Fatalf("byAction = %+v, want one delete of shop.example.com", byAction)
	}

	limited, err := ListAuditEntries(ctx, db, sc, AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("len(limited) = %d, want 1", len(limited))
	}
}

// TestIntegrationAuditEntriesRefuseUpdateAndDelete is the enforcement half of
// "the trail is append-only to the application": the Go code containing no
// update path is necessary but not sufficient, because the next person to add
// one gets no warning from it. This one fails.
func TestIntegrationAuditEntriesRefuseUpdateAndDelete(t *testing.T) {
	db, _ := scopedDB(t)
	ctx := context.Background()

	e := model.AuditEntry{Action: "project.create", ObjectType: "project", ObjectLabel: "acme", ActorSource: "cli"}
	if err := AppendAuditEntry(ctx, db, &e); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := db.Exec("update audit_entries set object_label = 'tampered'").Error; err == nil {
		t.Error("UPDATE on audit_entries was accepted; the trail is rewritable")
	}
	if err := db.Exec("delete from audit_entries where id = ?", e.ID).Error; err == nil {
		t.Error("DELETE on audit_entries was accepted; the trail is erasable")
	}
	// A DELETE matching nothing must be refused too, or the refusal is a
	// property of the data rather than of the table.
	if err := db.Exec("delete from audit_entries where id = -1").Error; err == nil {
		t.Error("DELETE matching no rows was accepted")
	}

	var got model.AuditEntry
	if err := db.First(&got, e.ID).Error; err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.ObjectLabel != "acme" {
		t.Errorf("object_label = %q after the refused UPDATE, want %q", got.ObjectLabel, "acme")
	}
}
