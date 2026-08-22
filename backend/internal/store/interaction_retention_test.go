package store

import (
	"context"
	"testing"
	"time"
)

// DeleteOldClosedInteractions: task 10.6 of rebuild-console-and-add-interaction-history.
//
// Four cases, each its own row so one assertion failure doesn't hide another:
// a closed row inside the retention period survives, a closed row past it is
// deleted, an OPEN row past it survives regardless of age (that's the rule
// AbandonStaleInteractions exists to enforce separately, not this function),
// and a retention of 0 (the keep-forever default) deletes nothing at all even
// though rows exist that would otherwise qualify.
func TestIntegrationDeleteOldClosedInteractionsRetentionRules(t *testing.T) {
	v := newInteractionEnv(t)
	ctx := context.Background()

	now := time.Now()
	longAgo := now.Add(-200 * 24 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	young := v.insert(v.interaction("young", recent))
	young.WithdrawnAt = ptrTime(recent)
	if err := v.db.Save(young).Error; err != nil {
		t.Fatalf("close young row: %v", err)
	}

	oldClosed := v.insert(v.interaction("old-closed", longAgo))
	oldClosed.WithdrawnAt = ptrTime(longAgo)
	if err := v.db.Save(oldClosed).Error; err != nil {
		t.Fatalf("close old row: %v", err)
	}

	oldOpen := v.insert(v.interaction("old-open", longAgo))
	// Never withdrawn: open regardless of how old PublishedAt is.

	const retention = 180 * 24 * time.Hour

	// A retention of 0 must delete nothing, even though oldClosed already
	// qualifies for deletion under the 180-day period below — this is the
	// keep-forever default and it must not be a "very long period" in
	// disguise.
	n, err := DeleteOldClosedInteractions(ctx, v.db, 0)
	if err != nil {
		t.Fatalf("delete with retention=0: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows deleted at retention=0 = %d, want 0", n)
	}
	for _, id := range []int64{young.ID, oldClosed.ID, oldOpen.ID} {
		v.reload(id)
	}

	n, err = DeleteOldClosedInteractions(ctx, v.db, retention)
	if err != nil {
		t.Fatalf("delete with retention=%s: %v", retention, err)
	}
	if n != 1 {
		t.Fatalf("rows deleted at retention=%s = %d, want 1", retention, n)
	}

	// Survives: closed too recently.
	v.reload(young.ID)
	// Survives: open, no matter its age.
	v.reload(oldOpen.ID)
	// Gone: closed, past the period.
	var count int64
	if err := v.db.Model(young).Where("id = ?", oldClosed.ID).Count(&count).Error; err != nil {
		t.Fatalf("count old closed row: %v", err)
	}
	if count != 0 {
		t.Fatalf("old closed row still present after retention sweep")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
