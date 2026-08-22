package store

import (
	"context"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

func TestIntegrationSessionRoundTrip(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()

	s := &model.Session{
		ID:          "sess_abc123",
		Subject:     "user@example.com",
		DisplayName: "A User",
		Groups:      model.StringArray{"admins"},
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	}
	if err := CreateSession(ctx, db, s); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := FindSession(ctx, db, s.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Subject != s.Subject {
		t.Fatalf("subject = %q, want %q", got.Subject, s.Subject)
	}

	before := got.LastSeenAt
	time.Sleep(10 * time.Millisecond)
	if err := TouchSession(ctx, db, s.ID); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, _ = FindSession(ctx, db, s.ID)
	if !got.LastSeenAt.After(before) {
		t.Fatalf("last_seen_at did not advance: before=%v after=%v", before, got.LastSeenAt)
	}

	if err := RevokeSession(ctx, db, s.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ = FindSession(ctx, db, s.ID)
	if got.RevokedAt == nil {
		t.Fatal("revoked_at still nil after revoke")
	}

	if err := DeleteSession(ctx, db, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := FindSession(ctx, db, s.ID); err == nil {
		t.Fatal("find after delete: want an error, got nil")
	}
}

func TestIntegrationDeleteExpiredSessions(t *testing.T) {
	db, _ := migratedDB(t)
	ctx := context.Background()

	live := &model.Session{ID: "sess_live", Subject: "u1", DisplayName: "U1", Groups: model.StringArray{}, ExpiresAt: time.Now().Add(time.Hour).UTC()}
	dead := &model.Session{ID: "sess_dead", Subject: "u2", DisplayName: "U2", Groups: model.StringArray{}, ExpiresAt: time.Now().Add(-time.Hour).UTC()}
	if err := CreateSession(ctx, db, live); err != nil {
		t.Fatalf("create live: %v", err)
	}
	if err := CreateSession(ctx, db, dead); err != nil {
		t.Fatalf("create dead: %v", err)
	}

	n, err := DeleteExpiredSessions(ctx, db)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}

	if _, err := FindSession(ctx, db, live.ID); err != nil {
		t.Fatalf("live session missing: %v", err)
	}
	if _, err := FindSession(ctx, db, dead.ID); err == nil {
		t.Fatal("dead session still present after sweep")
	}
}
