package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// ReorderTask moves a task to a project/parent and assigns a stable position.
// beforeID and afterID are mutually exclusive anchors in the destination list.
func (s *Store) ReorderTask(ctx context.Context, tc *tenant.Context, id string, projectID, parentID *string, beforeID, afterID string) (*model.Task, error) {
	if beforeID != "" && afterID != "" {
		return nil, errors.New("use before_id or after_id, not both")
	}
	if _, err := s.GetTask(ctx, tc, id); err != nil {
		return nil, err
	}
	if err := s.validateTaskRelations(ctx, tc, projectID, parentID, id); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	position, err := destinationPosition(ctx, tx, tc.TenantID, projectID, parentID, beforeID, afterID)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET project_id = ?, parent_id = ?, position = ?, updated_at = unixepoch('subsec') * 1000 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`, projectID, parentID, position, id, tc.TenantID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetTask(ctx, tc, id)
}

func destinationPosition(ctx context.Context, tx *sql.Tx, tenantID string, projectID, parentID *string, beforeID, afterID string) (int64, error) {
	var anchor int64
	anchorID := beforeID
	if anchorID == "" {
		anchorID = afterID
	}
	if anchorID == "" {
		var max sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM tasks WHERE tenant_id = ? AND project_id IS ? AND parent_id IS ? AND deleted_at IS NULL`, tenantID, projectID, parentID).Scan(&max); err != nil {
			return 0, err
		}
		if !max.Valid {
			return 1024, nil
		}
		return max.Int64 + 1024, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT position FROM tasks WHERE id = ? AND tenant_id = ? AND project_id IS ? AND parent_id IS ? AND deleted_at IS NULL`, anchorID, tenantID, projectID, parentID).Scan(&anchor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("reorder anchor not found in destination")
		}
		return 0, err
	}
	if beforeID != "" {
		var prev sql.NullInt64
		_ = tx.QueryRowContext(ctx, `SELECT MAX(position) FROM tasks WHERE tenant_id = ? AND project_id IS ? AND parent_id IS ? AND deleted_at IS NULL AND position < ?`, tenantID, projectID, parentID, anchor).Scan(&prev)
		if !prev.Valid {
			return anchor - 1024, nil
		}
		if anchor-prev.Int64 > 1 {
			return prev.Int64 + (anchor-prev.Int64)/2, nil
		}
	} else {
		var next sql.NullInt64
		_ = tx.QueryRowContext(ctx, `SELECT MIN(position) FROM tasks WHERE tenant_id = ? AND project_id IS ? AND parent_id IS ? AND deleted_at IS NULL AND position > ?`, tenantID, projectID, parentID, anchor).Scan(&next)
		if !next.Valid {
			return anchor + 1024, nil
		}
		if next.Int64-anchor > 1 {
			return anchor + (next.Int64-anchor)/2, nil
		}
	}
	// No integer gap remains: compact the destination list and retry once.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE tenant_id = ? AND project_id IS ? AND parent_id IS ? AND deleted_at IS NULL ORDER BY position, created_at`, tenantID, projectID, parentID)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = ? WHERE id = ?`, int64(i+1)*1024, id); err != nil {
			return 0, err
		}
	}
	return destinationPosition(ctx, tx, tenantID, projectID, parentID, beforeID, afterID)
}
