package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// AppendAuditEntry inserts one audit row. The trail is append-only: there is
// deliberately no update or delete here.
func AppendAuditEntry(ctx context.Context, db *gorm.DB, e *model.AuditEntry) error {
	if err := db.WithContext(ctx).Create(e).Error; err != nil {
		return fmt.Errorf("store: append audit entry: %w", err)
	}
	return nil
}

// AuditFilter narrows ListAuditEntries. Zero values are "don't filter on
// this field"; Limit <= 0 defaults to 200.
type AuditFilter struct {
	ProjectID  *int64
	ObjectType string
	ObjectID   *int64
	Action     string
	Limit      int
}

// ListAuditEntries returns matching audit rows, newest first.
func ListAuditEntries(ctx context.Context, db *gorm.DB, f AuditFilter) ([]model.AuditEntry, error) {
	q := db.WithContext(ctx).Model(&model.AuditEntry{})
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
