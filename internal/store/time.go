// Time-tracking store methods. Lives in its own file to keep store.go
// readable. All methods take *tenant.Context so cross-tenant access
// is impossible.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// Errors callers can check.
var (
	ErrTimerAlreadyRunning = errors.New("a timer is already running")
	ErrNoActiveTimer       = errors.New("no active timer")
)

// StartTimeEntry begins a new entry. Returns ErrTimerAlreadyRunning
// if the user already has an open entry. If taskID is non-nil it is
// stored as the task under focus.
func (s *Store) StartTimeEntry(ctx context.Context, tc *tenant.Context, taskID *string, kind, note string) (*model.TimeEntry, error) {
	return s.StartTimedEntry(ctx, tc, taskID, kind, note, 0)
}

func (s *Store) StartTimedEntry(ctx context.Context, tc *tenant.Context, taskID *string, kind, note string, planned time.Duration) (*model.TimeEntry, error) {
	if kind == "" {
		kind = "work"
	}
	// Reject if an open entry already exists.
	var openID string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM time_entries
		 WHERE user_id = ? AND ended_at IS NULL`,
		tc.UserID,
	).Scan(&openID)
	if err == nil {
		return nil, ErrTimerAlreadyRunning
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	id := uuid.NewString()
	now := time.Now()
	var target any
	if planned > 0 {
		target = now.Add(planned).UnixMilli()
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO time_entries(id, tenant_id, user_id, task_id, kind,
		                         started_at, duration_ms, note, planned_duration_ms, target_end_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		id, tc.TenantID, tc.UserID, taskID, kind,
		now.UnixMilli(), note, planned.Milliseconds(), target,
	)
	if err != nil {
		// Race: another concurrent start won. Surface a friendly error.
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrTimerAlreadyRunning
		}
		return nil, err
	}
	t, err := s.GetTimeEntry(ctx, tc, id)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) StopExpiredTimers(ctx context.Context, now time.Time) ([]model.TimeEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, tenant_id, user_id, started_at, target_end_at FROM time_entries WHERE ended_at IS NULL AND target_end_at IS NOT NULL AND target_end_at <= ?`, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	type due struct {
		id, tenantID, userID string
		started, target      int64
	}
	var items []due
	for rows.Next() {
		var v due
		if err := rows.Scan(&v.id, &v.tenantID, &v.userID, &v.started, &v.target); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, v)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var out []model.TimeEntry
	for _, v := range items {
		res, err := s.DB.ExecContext(ctx, `UPDATE time_entries SET ended_at = ?, duration_ms = ? WHERE id = ? AND ended_at IS NULL`, v.target, v.target-v.started, v.id)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue
		}
		tc := &tenant.Context{TenantID: v.tenantID, UserID: v.userID}
		e, err := s.GetTimeEntry(ctx, tc, v.id)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, nil
}

// StopTimeEntry closes the user's open entry. Returns ErrNoActiveTimer
// if there isn't one.
func (s *Store) StopTimeEntry(ctx context.Context, tc *tenant.Context, note string) (*model.TimeEntry, error) {
	now := time.Now()
	var (
		id        string
		started   int64
		existNote string
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, started_at, note FROM time_entries
		 WHERE user_id = ? AND ended_at IS NULL`,
		tc.UserID,
	).Scan(&id, &started, &existNote)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoActiveTimer
	}
	if err != nil {
		return nil, err
	}
	dur := now.UnixMilli() - started
	finalNote := existNote
	if note != "" {
		if finalNote != "" {
			finalNote = finalNote + "\n" + note
		} else {
			finalNote = note
		}
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE time_entries
		 SET ended_at = ?, duration_ms = ?, note = ?
		 WHERE id = ?`,
		now.UnixMilli(), dur, finalNote, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetTimeEntry(ctx, tc, id)
}

// GetTimeEntry returns a single entry by id (tenant-scoped).
func (s *Store) GetTimeEntry(ctx context.Context, tc *tenant.Context, id string) (*model.TimeEntry, error) {
	row := s.DB.QueryRowContext(ctx, timeEntrySelect+`
		WHERE te.id = ? AND te.tenant_id = ?`, id, tc.TenantID)
	return scanTimeEntry(row)
}

// ActiveTimeEntry returns the user's open entry, or nil if none.
func (s *Store) ActiveTimeEntry(ctx context.Context, tc *tenant.Context) (*model.TimeEntry, error) {
	row := s.DB.QueryRowContext(ctx, timeEntrySelect+`
		WHERE te.user_id = ? AND te.tenant_id = ? AND te.ended_at IS NULL`,
		tc.UserID, tc.TenantID)
	e, err := scanTimeEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// ListTimeEntries returns entries in a time window (defaults: today).
func (s *Store) ListTimeEntries(ctx context.Context, tc *tenant.Context, from, to time.Time, limit int) ([]model.TimeEntry, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := s.DB.QueryContext(ctx, timeEntrySelect+`
		WHERE tenant_id = ? AND user_id = ? AND started_at >= ? AND started_at < ?
		ORDER BY started_at DESC
		LIMIT ?`,
		tc.TenantID, tc.UserID, from.UnixMilli(), to.UnixMilli(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TimeEntry
	for rows.Next() {
		e, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Hydrate task titles for the entries that have one.
	if err := s.hydrateTaskTitles(ctx, tc, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DailySummary aggregates total time per task for a single calendar
// day, plus a grand total. "Today" = midnight to midnight in the
// caller's location. If loc is nil, UTC is used.
func (s *Store) DailySummary(ctx context.Context, tc *tenant.Context, day time.Time, loc *time.Location) (*DailySummary, error) {
	if loc == nil {
		loc = time.UTC
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)

	// Per-task totals.
	rows, err := s.DB.QueryContext(ctx,
		`SELECT task_id, COALESCE(SUM(duration_ms), 0) AS total_ms, COUNT(*) AS n
		 FROM time_entries
		 WHERE tenant_id = ? AND user_id = ?
		   AND started_at >= ? AND started_at < ?
		   AND ended_at IS NOT NULL
		 GROUP BY task_id`,
		tc.TenantID, tc.UserID, start.UnixMilli(), end.UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summary := &DailySummary{Day: start, PerTask: []DailyTaskTotal{}}
	taskIDs := []string{}
	for rows.Next() {
		var t DailyTaskTotal
		var taskID sql.NullString
		if err := rows.Scan(&taskID, &t.TotalMs, &t.Count); err != nil {
			return nil, err
		}
		if taskID.Valid {
			t.TaskID = taskID.String
			taskIDs = append(taskIDs, taskID.String)
		}
		summary.PerTask = append(summary.PerTask, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Grand total across all entries in the day.
	var grand int64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(duration_ms), 0) FROM time_entries
		 WHERE tenant_id = ? AND user_id = ?
		   AND started_at >= ? AND started_at < ?
		   AND ended_at IS NOT NULL`,
		tc.TenantID, tc.UserID, start.UnixMilli(), end.UnixMilli(),
	).Scan(&grand); err != nil {
		return nil, err
	}
	summary.TotalMs = grand

	// Hydrate task titles.
	titleByID := map[string]string{}
	if len(taskIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(taskIDs)), ",")
		args := make([]any, 0, len(taskIDs)+1)
		args = append(args, tc.TenantID)
		for _, id := range taskIDs {
			args = append(args, id)
		}
		q := `SELECT id, title FROM tasks WHERE tenant_id = ? AND id IN (` + placeholders + `)`
		titleRows, err := s.DB.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer titleRows.Close()
		for titleRows.Next() {
			var id, title string
			if err := titleRows.Scan(&id, &title); err != nil {
				return nil, err
			}
			titleByID[id] = title
		}
	}
	for i := range summary.PerTask {
		taskID := summary.PerTask[i].TaskID
		if taskID == "" {
			summary.PerTask[i].TaskTitle = "(no task)"
			continue
		}
		if title, ok := titleByID[taskID]; ok {
			summary.PerTask[i].TaskTitle = title
		}
	}
	return summary, nil
}

// DailySummary is the aggregated view returned by DailySummary.
type DailySummary struct {
	Day     time.Time        `json:"day"`
	TotalMs int64            `json:"total_ms"`
	PerTask []DailyTaskTotal `json:"per_task"`
}

// DailyTaskTotal is one row in the per-task breakdown.
type DailyTaskTotal struct {
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title"`
	TotalMs   int64  `json:"total_ms"`
	Count     int    `json:"count"`
}

// ProductivityDay is a compact daily trend derived from existing task and
// time-entry rows. No analytics events or summary tables are persisted.
type ProductivityDay struct {
	Day       string `json:"day"`
	Completed int    `json:"completed"`
	FocusMs   int64  `json:"focus_ms"`
	Sessions  int    `json:"sessions"`
}

// ProductivityTrend returns one bucket per local calendar day, oldest first.
func (s *Store) ProductivityTrend(ctx context.Context, tc *tenant.Context, days int, loc *time.Location) ([]ProductivityDay, error) {
	if days < 1 || days > 30 {
		days = 14
	}
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from := today.AddDate(0, 0, -(days - 1))
	to := today.AddDate(0, 0, 1)
	trend := make([]ProductivityDay, days)
	byDay := make(map[string]*ProductivityDay, days)
	for i := range trend {
		trend[i].Day = from.AddDate(0, 0, i).Format("2006-01-02")
		byDay[trend[i].Day] = &trend[i]
	}

	taskRows, err := s.DB.QueryContext(ctx, `SELECT completed_at FROM tasks
		WHERE tenant_id = ? AND created_by = ? AND status = 'done'
		  AND deleted_at IS NULL AND completed_at >= ? AND completed_at < ?`,
		tc.TenantID, tc.UserID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	for taskRows.Next() {
		var completedAt int64
		if err := taskRows.Scan(&completedAt); err != nil {
			taskRows.Close()
			return nil, err
		}
		if bucket := byDay[time.UnixMilli(completedAt).In(loc).Format("2006-01-02")]; bucket != nil {
			bucket.Completed++
		}
	}
	if err := taskRows.Close(); err != nil {
		return nil, err
	}

	timeRows, err := s.DB.QueryContext(ctx, `SELECT started_at, duration_ms FROM time_entries
		WHERE tenant_id = ? AND user_id = ? AND ended_at IS NOT NULL
		  AND started_at >= ? AND started_at < ?`,
		tc.TenantID, tc.UserID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer timeRows.Close()
	for timeRows.Next() {
		var startedAt, durationMs int64
		if err := timeRows.Scan(&startedAt, &durationMs); err != nil {
			return nil, err
		}
		if bucket := byDay[time.UnixMilli(startedAt).In(loc).Format("2006-01-02")]; bucket != nil {
			bucket.FocusMs += durationMs
			bucket.Sessions++
		}
	}
	if err := timeRows.Err(); err != nil {
		return nil, err
	}
	return trend, nil
}

// -------------------------- helpers --------------------------

const timeEntrySelect = `
SELECT te.id, te.tenant_id, te.user_id, te.task_id, te.kind,
       te.started_at, te.ended_at, te.duration_ms, te.note,
       te.planned_duration_ms, te.target_end_at,
       t.title
FROM time_entries te
LEFT JOIN tasks t ON t.id = te.task_id
`

func scanTimeEntry(r rowScanner) (*model.TimeEntry, error) {
	var (
		e         model.TimeEntry
		taskID    sql.NullString
		endedAt   sql.NullInt64
		targetEnd sql.NullInt64
		started   int64
		title     sql.NullString
	)
	if err := r.Scan(
		&e.ID, &e.TenantID, &e.UserID, &taskID, &e.Kind,
		&started, &endedAt, &e.DurationMs, &e.Note, &e.PlannedDurationMs, &targetEnd, &title,
	); err != nil {
		return nil, err
	}
	if taskID.Valid {
		v := taskID.String
		e.TaskID = &v
	}
	if title.Valid {
		e.TaskTitle = title.String
	}
	e.StartedAt = time.UnixMilli(started)
	if targetEnd.Valid {
		v := time.UnixMilli(targetEnd.Int64)
		e.TargetEndAt = &v
	}
	if endedAt.Valid {
		v := time.UnixMilli(endedAt.Int64)
		e.EndedAt = &v
		e.Status = "stopped"
		// Recompute duration for open entries (safety net).
		if e.DurationMs == 0 && endedAt.Int64 > started {
			e.DurationMs = endedAt.Int64 - started
		}
	} else {
		e.Status = "open"
		e.DurationMs = time.Now().UnixMilli() - started
	}
	return &e, nil
}

func (s *Store) hydrateTaskTitles(ctx context.Context, tc *tenant.Context, entries []model.TimeEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := map[string]struct{}{}
	for _, e := range entries {
		if e.TaskID != nil {
			ids[*e.TaskID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	flat := make([]any, 0, len(ids)+1)
	args := []any{tc.TenantID}
	for id := range ids {
		args = append(args, id)
		flat = append(flat, id)
	}
	_ = flat
	placeholders := strings.TrimRight(strings.Repeat("?,", len(args)-1), ",")
	q := `SELECT id, title FROM tasks WHERE tenant_id = ? AND id IN (` + placeholders + `)`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string]string{}
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return err
		}
		byID[id] = title
	}
	for i := range entries {
		if entries[i].TaskID != nil {
			if t, ok := byID[*entries[i].TaskID]; ok {
				entries[i].TaskTitle = t
			}
		}
	}
	return nil
}

// TodayRange returns midnight..midnight tomorrow in loc.
func TodayRange(loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.Local
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return start, start.Add(24 * time.Hour)
}

// formatDuration renders a duration as "1h 23m" / "12m 5s" / "47s".
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
