// Package store contains all SQL access for the business entities.
// Every method takes a *tenant.Context so cross-tenant access is
// structurally impossible.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anirudhprakash/ttl/internal/model"
	"github.com/anirudhprakash/ttl/internal/tenant"
)

// ErrNotFound is returned when a row is not present in the tenant.
var ErrNotFound = errors.New("not found")

// Store wraps a sql.DB.
type Store struct {
	DB *sql.DB
}

func New(d *sql.DB) *Store { return &Store{DB: d} }

// -------------------------- Projects --------------------------

func (s *Store) CreateProject(ctx context.Context, tc *tenant.Context, name, color string) (*model.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("project name required")
	}
	if color == "" {
		color = "#888888"
	}
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects(id, tenant_id, name, color, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, tc.TenantID, name, color, now,
	)
	if err != nil {
		return nil, err
	}
	return &model.Project{
		ID: id, TenantID: tc.TenantID, Name: name,
		Color: color, CreatedAt: time.UnixMilli(now),
	}, nil
}

func (s *Store) ListProjects(ctx context.Context, tc *tenant.Context, includeArchived bool) ([]model.Project, error) {
	q := `SELECT id, tenant_id, name, color, archived_at, created_at
	      FROM projects WHERE tenant_id = ?`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q, tc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		var archivedAt sql.NullInt64
		var createdAt int64
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Color, &archivedAt, &createdAt); err != nil {
			return nil, err
		}
		if archivedAt.Valid {
			t := time.UnixMilli(archivedAt.Int64)
			p.ArchivedAt = &t
		}
		p.CreatedAt = time.UnixMilli(createdAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// -------------------------- Tags --------------------------

func (s *Store) CreateTag(ctx context.Context, tc *tenant.Context, name, color string) (*model.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("tag name required")
	}
	if color == "" {
		color = "#888888"
	}
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO tags(id, tenant_id, name, color, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, tc.TenantID, name, color, now,
	)
	if err != nil {
		return nil, err
	}
	return &model.Tag{
		ID: id, TenantID: tc.TenantID, Name: name,
		Color: color, CreatedAt: time.UnixMilli(now),
	}, nil
}

func (s *Store) ListTags(ctx context.Context, tc *tenant.Context) ([]model.Tag, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, tenant_id, name, color, created_at
		 FROM tags WHERE tenant_id = ? ORDER BY name COLLATE NOCASE`,
		tc.TenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Tag
	for rows.Next() {
		var t model.Tag
		var ms int64
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.Color, &ms); err != nil {
			return nil, err
		}
		t.CreatedAt = time.UnixMilli(ms)
		out = append(out, t)
	}
	return out, rows.Err()
}

// -------------------------- Tasks --------------------------

// TaskFilter narrows a task listing.
type TaskFilter struct {
	Status    string // "" | "open" | "done"
	ProjectID string // "" for any
	TagID     string
	ParentID  *string // nil for any, set pointer for "root only" or specific
	Search    string
	Overdue   bool
}

func (s *Store) CreateTask(ctx context.Context, tc *tenant.Context, in *model.Task) (*model.Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, errors.New("task title required")
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Priority < 0 || in.Priority > 3 {
		return nil, errors.New("priority must be 0..3")
	}
	in.ID = uuid.NewString()
	now := time.Now()
	in.TenantID = tc.TenantID
	in.CreatedBy = tc.UserID
	in.CreatedAt = now
	in.UpdatedAt = now

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO tasks(id, tenant_id, project_id, parent_id, title, notes,
		                   status, priority, due_at, recurrence_rrule,
		                   created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ID, in.TenantID, in.ProjectID, in.ParentID,
		in.Title, in.Notes, in.Status, in.Priority,
		nullableTime(in.DueAt), in.RecurrenceRRule,
		in.CreatedBy, in.CreatedAt.UnixMilli(), in.UpdatedAt.UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	if len(in.Tags) > 0 {
		if err := s.setTaskTags(ctx, tc, in.ID, in.Tags); err != nil {
			return nil, err
		}
	}
	return in, nil
}

func (s *Store) GetTask(ctx context.Context, tc *tenant.Context, id string) (*model.Task, error) {
	t, err := s.scanOneTask(ctx, s.DB.QueryRowContext(ctx, taskSelect+`
		WHERE id = ? AND tenant_id = ?`, id, tc.TenantID))
	if err != nil {
		return nil, err
	}
	tags, err := s.taskTags(ctx, tc, t.ID)
	if err != nil {
		return nil, err
	}
	t.Tags = tags
	return t, nil
}

func (s *Store) UpdateTask(ctx context.Context, tc *tenant.Context, id string, fields map[string]any) (*model.Task, error) {
	if len(fields) == 0 {
		return s.GetTask(ctx, tc, id)
	}
	allowed := map[string]bool{
		"title": true, "notes": true, "status": true, "priority": true,
		"due_at": true, "project_id": true, "parent_id": true,
		"recurrence_rrule": true,
	}
	var setParts []string
	var args []any
	for k, v := range fields {
		if !allowed[k] {
			return nil, fmt.Errorf("field %q not editable", k)
		}
		setParts = append(setParts, k+" = ?")
		args = append(args, v)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, time.Now().UnixMilli())
	args = append(args, id, tc.TenantID)

	res, err := s.DB.ExecContext(ctx,
		`UPDATE tasks SET `+strings.Join(setParts, ", ")+
			` WHERE id = ? AND tenant_id = ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetTask(ctx, tc, id)
}

func (s *Store) CompleteTask(ctx context.Context, tc *tenant.Context, id string) (*model.Task, error) {
	now := time.Now().UnixMilli()
	res, err := s.DB.ExecContext(ctx,
		`UPDATE tasks SET status='done', completed_at=?, updated_at=?
		 WHERE id = ? AND tenant_id = ? AND status != 'done'`,
		now, now, id, tc.TenantID,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Either missing or already done — return current state.
		return s.GetTask(ctx, tc, id)
	}
	return s.GetTask(ctx, tc, id)
}

func (s *Store) DeleteTask(ctx context.Context, tc *tenant.Context, id string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND tenant_id = ?`,
		id, tc.TenantID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTasks returns top-level tasks (no subtasks) matching f. Subtasks
// for each task are NOT hydrated here to keep listings cheap — fetch a
// single task via GetTask for the full tree.
func (s *Store) ListTasks(ctx context.Context, tc *tenant.Context, f TaskFilter, limit int) ([]model.Task, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var (
		conds = []string{"tenant_id = ?"}
		args  = []any{tc.TenantID}
	)
	if f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, f.Status)
	}
	if f.ProjectID != "" {
		conds = append(conds, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.ParentID != nil {
		if *f.ParentID == "" {
			conds = append(conds, "parent_id IS NULL")
		} else {
			conds = append(conds, "parent_id = ?")
			args = append(args, *f.ParentID)
		}
	}
	if f.Search != "" {
		conds = append(conds, "(title LIKE ? OR notes LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like)
	}
	if f.Overdue {
		conds = append(conds, "due_at IS NOT NULL AND due_at < ? AND status = 'open'")
		args = append(args, time.Now().UnixMilli())
	}

	q := taskSelect + " WHERE " + strings.Join(conds, " AND ") +
		" ORDER BY " +
		"  CASE WHEN due_at IS NULL THEN 1 ELSE 0 END, " +
		"  due_at ASC, " +
		"  priority DESC, " +
		"  updated_at DESC " +
		" LIMIT ?"
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Hydrate tags in a single follow-up query if there are tasks.
	if len(out) > 0 {
		ids := make([]any, len(out))
		for i, t := range out {
			ids[i] = t.ID
		}
		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1]
		tagRows, err := s.DB.QueryContext(ctx,
			`SELECT task_id, t.name FROM task_tags tt
			 JOIN tags t ON t.id = tt.tag_id
			 WHERE tt.tenant_id = ? AND tt.task_id IN (`+placeholders+`)
			 ORDER BY t.name COLLATE NOCASE`,
			append([]any{tc.TenantID}, ids...)...,
		)
		if err != nil {
			return nil, err
		}
		defer tagRows.Close()
		tagMap := map[string][]string{}
		for tagRows.Next() {
			var tid, name string
			if err := tagRows.Scan(&tid, &name); err != nil {
				return nil, err
			}
			tagMap[tid] = append(tagMap[tid], name)
		}
		for i := range out {
			out[i].Tags = tagMap[out[i].ID]
		}
	}
	return out, nil
}

// Subtasks returns direct children of a parent task, recursively up to
// a few levels deep.
func (s *Store) Subtasks(ctx context.Context, tc *tenant.Context, parentID string) ([]model.Task, error) {
	rows, err := s.DB.QueryContext(ctx, taskSelect+`
		WHERE tenant_id = ? AND parent_id = ? ORDER BY created_at ASC`,
		tc.TenantID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// -------------------------- helpers --------------------------

const taskSelect = `
SELECT id, tenant_id, project_id, parent_id, title, notes,
       status, priority, due_at, recurrence_rrule,
       created_by, created_at, updated_at, completed_at
FROM tasks `

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanOneTask(ctx context.Context, r rowScanner) (*model.Task, error) {
	t, err := s.scanTask(r)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) scanTask(r rowScanner) (*model.Task, error) {
	var t model.Task
	var projectID, parentID sql.NullString
	var dueAt, completedAt sql.NullInt64
	var recurrence sql.NullString
	var createdAt, updatedAt int64
	if err := r.Scan(
		&t.ID, &t.TenantID, &projectID, &parentID,
		&t.Title, &t.Notes, &t.Status, &t.Priority,
		&dueAt, &recurrence,
		&t.CreatedBy, &createdAt, &updatedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	if projectID.Valid {
		v := projectID.String
		t.ProjectID = &v
	}
	if parentID.Valid {
		v := parentID.String
		t.ParentID = &v
	}
	if dueAt.Valid {
		v := time.UnixMilli(dueAt.Int64)
		t.DueAt = &v
	}
	if completedAt.Valid {
		v := time.UnixMilli(completedAt.Int64)
		t.CompletedAt = &v
	}
	if recurrence.Valid {
		v := recurrence.String
		t.RecurrenceRRule = &v
	}
	t.CreatedAt = time.UnixMilli(createdAt)
	t.UpdatedAt = time.UnixMilli(updatedAt)
	return &t, nil
}

func (s *Store) setTaskTags(ctx context.Context, tc *tenant.Context, taskID string, tagNames []string) error {
	// Resolve tag names -> ids, creating tags on demand (common UX).
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var tagID string
		err := s.DB.QueryRowContext(ctx,
			`SELECT id FROM tags WHERE tenant_id = ? AND name = ?`,
			tc.TenantID, name,
		).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tag, err := s.CreateTag(ctx, tc, name, "")
			if err != nil {
				return err
			}
			tagID = tag.ID
		} else if err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx,
			`INSERT OR IGNORE INTO task_tags(task_id, tag_id, tenant_id)
			 VALUES (?, ?, ?)`,
			taskID, tagID, tc.TenantID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) taskTags(ctx context.Context, tc *tenant.Context, taskID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT t.name FROM task_tags tt
		 JOIN tags t ON t.id = tt.tag_id
		 WHERE tt.tenant_id = ? AND tt.task_id = ?
		 ORDER BY t.name COLLATE NOCASE`,
		tc.TenantID, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
