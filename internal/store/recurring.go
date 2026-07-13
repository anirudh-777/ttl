// Recurring task logic + reminder methods.

package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teambition/rrule-go"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// ErrInvalidRRule is returned when a recurrence_rrule string is malformed.
var ErrInvalidRRule = errors.New("invalid rrule")

// parseRRule returns the next occurrence strictly after `start`.
// If rruleStr is empty, returns ok=false. The function accepts either
// a bare "RRULE:..." form (DTSTART is synthesised from `start`) or a
// full "DTSTART:...\nRRULE:..." block. The library is happy with
// non-UTC DTSTART strings as long as we keep the time.Time in its
// original location; converting to UTC would shift the day-of-week
// and break BYDAY rules.
func parseRRule(rruleStr string, start time.Time) (next time.Time, ok bool, err error) {
	rruleStr = strings.TrimSpace(rruleStr)
	if rruleStr == "" {
		return time.Time{}, false, nil
	}
	dt := start
	if !strings.HasPrefix(strings.ToUpper(rruleStr), "DTSTART") {
		// Use the local time format so day-of-week matches `start`.
		dtstart := "DTSTART:" + dt.Format("20060102T150405Z")
		rule := strings.TrimSpace(rruleStr)
		if !strings.HasPrefix(strings.ToUpper(rule), "RRULE:") {
			rule = "RRULE:" + rule
		}
		rruleStr = dtstart + "\n" + rule
	}
	r, err := rrule.StrToRRule(rruleStr)
	if err != nil {
		return time.Time{}, false, err
	}
	r.DTStart(dt)
	nx := r.After(dt, false)
	if nx.IsZero() {
		return time.Time{}, true, nil
	}
	return nx, true, nil
}

// spawnNextOccurrence creates a new open task that is the next
// occurrence of t. Returns (nil, nil) if t has no recurrence. The
// new task inherits project, tags, priority, and notes.
func (s *Store) spawnNextOccurrence(ctx context.Context, tc *tenant.Context, t *model.Task) (*model.Task, error) {
	if t.RecurrenceRRule == nil || *t.RecurrenceRRule == "" {
		return nil, nil
	}
	// Compute next from due_at (preferred) else completed_at else now.
	var base time.Time
	switch {
	case t.DueAt != nil:
		base = *t.DueAt
	case t.CompletedAt != nil:
		base = *t.CompletedAt
	default:
		base = time.Now()
	}
	next, ok, err := parseRRule(*t.RecurrenceRRule, base)
	if err != nil {
		return nil, ErrInvalidRRule
	}
	if !ok {
		return nil, nil
	}
	nextCopy := next
	newTask := &model.Task{
		Title:           t.Title,
		Notes:           t.Notes,
		ProjectID:       t.ProjectID,
		ParentID:        t.ParentID,
		Priority:        t.Priority,
		DueAt:           &nextCopy,
		RecurrenceRRule: t.RecurrenceRRule,
		Tags:            t.Tags,
	}
	created, err := s.CreateTask(ctx, tc, newTask)
	if err != nil {
		return nil, err
	}
	if t.DueAt != nil && created.DueAt != nil {
		rows, err := s.DB.QueryContext(ctx, `SELECT fire_at, endpoint_id FROM reminders WHERE tenant_id = ? AND task_id = ?`, tc.TenantID, t.ID)
		if err != nil {
			return nil, err
		}
		type inherited struct {
			fire     int64
			endpoint sql.NullString
		}
		var items []inherited
		for rows.Next() {
			var v inherited
			if err := rows.Scan(&v.fire, &v.endpoint); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, v)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		for _, item := range items {
			when := created.DueAt.Add(time.Duration(item.fire-t.DueAt.UnixMilli()) * time.Millisecond)
			var endpoint *string
			if item.endpoint.Valid {
				v := item.endpoint.String
				endpoint = &v
			}
			if _, err := s.CreateReminderWithEndpoint(ctx, tc, created.ID, when, endpoint); err != nil {
				return nil, err
			}
		}
	}
	return created, nil
}

// CompleteTask is overridden (via a small adapter below) to spawn the
// next occurrence after marking complete.

// CompleteTaskAndRecur wraps CompleteTask + recurring spawn so callers
// can opt in without rewriting CompleteTask itself.
func (s *Store) CompleteTaskAndRecur(ctx context.Context, tc *tenant.Context, id string) (*model.Task, *model.Task, error) {
	now := time.Now().UnixMilli()
	res, err := s.DB.ExecContext(ctx, `UPDATE tasks SET status='done', completed_at=?, updated_at=? WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL AND status != 'done'`, now, now, id, tc.TenantID)
	if err != nil {
		return nil, nil, err
	}
	n, _ := res.RowsAffected()
	done, err := s.GetTask(ctx, tc, id)
	if err != nil {
		return nil, nil, err
	}
	if n == 0 {
		return done, nil, nil
	}
	next, err := s.spawnNextOccurrence(ctx, tc, done)
	if err != nil {
		// If RRULE was malformed, log it but return the completed task.
		if errors.Is(err, ErrInvalidRRule) {
			return done, nil, nil
		}
		return done, nil, err
	}
	return done, next, nil
}

// -------------------------- Reminders --------------------------

// CreateReminder schedules a reminder for a task at fireAt.
func (s *Store) CreateReminder(ctx context.Context, tc *tenant.Context, taskID string, fireAt time.Time) (*model.Reminder, error) {
	return s.CreateReminderWithEndpoint(ctx, tc, taskID, fireAt, nil)
}

func (s *Store) CreateReminderWithEndpoint(ctx context.Context, tc *tenant.Context, taskID string, fireAt time.Time, endpointID *string) (*model.Reminder, error) {
	if _, err := s.GetTask(ctx, tc, taskID); err != nil {
		return nil, err
	}
	if endpointID != nil && *endpointID != "" {
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_endpoints WHERE id = ? AND tenant_id = ?`, *endpointID, tc.TenantID).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errors.New("notification endpoint not found in tenant")
		}
	}
	id := uuid.NewString()
	now := time.Now()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO reminders(id, tenant_id, task_id, user_id, fire_at, status, created_at, endpoint_id)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`,
		id, tc.TenantID, taskID, tc.UserID, fireAt.UnixMilli(), now.UnixMilli(), endpointID,
	)
	if err != nil {
		return nil, err
	}
	return s.GetReminder(ctx, tc, id)
}

func (s *Store) GetReminder(ctx context.Context, tc *tenant.Context, id string) (*model.Reminder, error) {
	const sel = `
		SELECT r.id, r.tenant_id, r.task_id, r.user_id, r.fire_at,
		       r.status, r.created_at, r.sent_at, r.endpoint_id, r.delivery_status, r.delivery_error, t.title
		FROM reminders r
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE r.id = ? AND r.tenant_id = ?`
	row := s.DB.QueryRowContext(ctx, sel, id, tc.TenantID)
	return scanReminder(row)
}

// ListReminders returns reminders for the user, optionally filtered by
// status. Newest first.
func (s *Store) ListReminders(ctx context.Context, tc *tenant.Context, status string, limit int) ([]model.Reminder, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `
		SELECT r.id, r.tenant_id, r.task_id, r.user_id, r.fire_at,
		       r.status, r.created_at, r.sent_at, r.endpoint_id, r.delivery_status, r.delivery_error, t.title
		FROM reminders r
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE r.tenant_id = ? AND r.user_id = ?`
	args := []any{tc.TenantID, tc.UserID}
	if status != "" {
		q += ` AND r.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY r.fire_at ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Reminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// DeleteReminder removes a reminder (owner-scoped).
func (s *Store) DeleteReminder(ctx context.Context, tc *tenant.Context, id string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM reminders WHERE id = ? AND tenant_id = ? AND user_id = ?`,
		id, tc.TenantID, tc.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateReminder(ctx context.Context, tc *tenant.Context, id string, fireAt time.Time) (*model.Reminder, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE reminders SET fire_at = ?, status = 'pending', sent_at = NULL, delivery_status = 'pending', delivery_error = NULL WHERE id = ? AND tenant_id = ? AND user_id = ?`, fireAt.UnixMilli(), id, tc.TenantID, tc.UserID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetReminder(ctx, tc, id)
}

func (s *Store) AcknowledgeReminder(ctx context.Context, tc *tenant.Context, id string) (*model.Reminder, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE reminders SET status = 'ack' WHERE id = ? AND tenant_id = ? AND user_id = ?`, id, tc.TenantID, tc.UserID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetReminder(ctx, tc, id)
}

func (s *Store) SnoozeReminder(ctx context.Context, tc *tenant.Context, id string, fireAt time.Time) (*model.Reminder, error) {
	return s.UpdateReminder(ctx, tc, id, fireAt)
}

// FetchDueReminders returns all pending reminders whose fire_at has
// passed, marks them sent, and returns the list. Safe to call from a
// background ticker.
func (s *Store) FetchDueReminders(ctx context.Context, now time.Time) ([]model.Reminder, error) {
	const sel = `
		SELECT r.id, r.tenant_id, r.task_id, r.user_id, r.fire_at,
		       r.status, r.created_at, r.sent_at, r.endpoint_id, r.delivery_status, r.delivery_error, t.title
		FROM reminders r
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE r.status = 'pending' AND r.fire_at <= ?
		ORDER BY r.fire_at ASC`
	rows, err := s.DB.QueryContext(ctx, sel, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Reminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Claim them in one transaction. The status predicate prevents two
	// overlapping workers from delivering the same reminder.
	if len(out) == 0 {
		return out, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	claimed := out[:0]
	for _, r := range out {
		res, err := tx.ExecContext(ctx,
			`UPDATE reminders SET status = 'sent', sent_at = ? WHERE id = ? AND status = 'pending'`,
			now.UnixMilli(), r.ID,
		)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 1 {
			claimed = append(claimed, r)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range claimed {
		claimed[i].Status = "sent"
		t := now
		claimed[i].SentAt = &t
	}
	return claimed, nil
}

func scanReminder(r rowScanner) (*model.Reminder, error) {
	var (
		rm                        model.Reminder
		sentAt                    sql.NullInt64
		endpointID, deliveryError sql.NullString
		title                     sql.NullString
	)
	var fire, created int64
	if err := r.Scan(
		&rm.ID, &rm.TenantID, &rm.TaskID, &rm.UserID,
		&fire, &rm.Status, &created, &sentAt, &endpointID, &rm.DeliveryStatus, &deliveryError, &title,
	); err != nil {
		return nil, err
	}
	rm.FireAt = time.UnixMilli(fire)
	rm.CreatedAt = time.UnixMilli(created)
	if sentAt.Valid {
		t := time.UnixMilli(sentAt.Int64)
		rm.SentAt = &t
	}
	if title.Valid {
		rm.TaskTitle = title.String
	}
	if endpointID.Valid {
		v := endpointID.String
		rm.EndpointID = &v
	}
	if deliveryError.Valid {
		v := deliveryError.String
		rm.DeliveryError = &v
	}
	return &rm, nil
}
