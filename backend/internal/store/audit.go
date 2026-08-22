package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// AppendAuditEntry inserts one audit row. The trail is append-only: there is
// deliberately no update or delete here.
//
// No Scope: the entry carries the account it belongs to, and it is written
// inside the caller's transaction — which is already scoped, because the change
// being recorded was. Taking a Scope here would mean a second SET LOCAL inside
// that transaction for the same account.
func AppendAuditEntry(ctx context.Context, db *gorm.DB, e *model.AuditEntry) error {
	if err := db.WithContext(ctx).Create(e).Error; err != nil {
		return fmt.Errorf("store: append audit entry: %w", err)
	}
	return nil
}

// AuditFilter narrows ListAuditEntries. Zero values are "don't filter on
// this field"; Limit <= 0 defaults to 200.
type AuditFilter struct {
	ProjectID  *model.ID
	ObjectType string
	ObjectID   *model.ID
	Action     string
	Limit      int
}

// ListAuditEntries returns matching audit rows for one account, newest first.
//
// Confined like every other read (task 7.5). An audit trail is a record of what
// happened inside an account, and a tenant reading theirs must not see another's
// — the entries name people, addresses and object labels, which is more
// disclosive than the inventory itself.
//
// audit_entries carries account_id but has no policy: the table is append-only
// through a trigger, and rows written before this change have no account. The
// predicate is therefore the whole of the confinement here, which is why it is
// applied first and unconditionally.
func ListAuditEntries(ctx context.Context, db *gorm.DB, sc Scope, f AuditFilter) ([]model.AuditEntry, error) {
	if !sc.Valid() {
		return nil, ErrNoScope
	}
	q := db.WithContext(ctx).Model(&model.AuditEntry{}).Where("account_id = ?", sc.AccountID())
	if f.ProjectID != nil {
		q = q.Where("project_id = ?", *f.ProjectID)
	}
	if f.ObjectType != "" {
		q = q.Where("object_type = ?", f.ObjectType)
	}
	if f.ObjectID != nil {
		q = q.Where("object_id = ?", *f.ObjectID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}

	var es []model.AuditEntry
	if err := q.Order("occurred_at DESC").Limit(limit).Find(&es).Error; err != nil {
		return nil, fmt.Errorf("store: list audit entries: %w", err)
	}
	return es, nil
}
