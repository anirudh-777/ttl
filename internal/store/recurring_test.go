package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhprakash/ttl/internal/model"
	"github.com/anirudhprakash/ttl/internal/store"
	"github.com/anirudhprakash/ttl/internal/tenant"
)

func TestRecurringTaskDaily(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "r@r.test", "RecurCo")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	due := time.Now().Add(2 * time.Hour)
	rrule := "FREQ=DAILY;INTERVAL=1"
	in := &model.Task{Title: "Daily standup", DueAt: &due, RecurrenceRRule: &rrule}
	created, err := st.CreateTask(ctx, tc, in)
	if err != nil {
		t.Fatal(err)
	}

	// Complete; should spawn tomorrow's occurrence.
	completed, next, err := st.CompleteTaskAndRecur(ctx, tc, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "done" {
		t.Errorf("completed status = %q", completed.Status)
	}
	if next == nil {
		t.Fatal("expected next occurrence")
	}
	if next.Title != "Daily standup" {
		t.Errorf("next title = %q", next.Title)
	}
	if next.Status != "open" {
		t.Errorf("next status = %q, want open", next.Status)
	}
	if next.DueAt == nil {
		t.Fatal("next due_at is nil")
	}
	delta := next.DueAt.Sub(*completed.DueAt)
	if delta < 23*time.Hour || delta > 25*time.Hour {
		t.Errorf("next due_at delta = %v, want ~24h", delta)
	}
}

func TestRecurringTaskWeekly(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "w@w.test", "WeeklyCo")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	// Pick the next Monday as the seed so the next weekly occurrence
	// is always 7 days later regardless of when the test runs.
	now := time.Now()
	daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	if daysUntilMon == 0 {
		daysUntilMon = 7
	}
	due := now.Add(time.Duration(daysUntilMon) * 24 * time.Hour)
	rrule := "FREQ=WEEKLY;INTERVAL=1;BYDAY=MO"
	in := &model.Task{Title: "Weekly review", DueAt: &due, RecurrenceRRule: &rrule}
	created, err := st.CreateTask(ctx, tc, in)
	if err != nil {
		t.Fatal(err)
	}
	completed, next, err := st.CompleteTaskAndRecur(ctx, tc, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected next weekly occurrence")
	}
	delta := next.DueAt.Sub(*completed.DueAt)
	if delta < 6*24*time.Hour || delta > 8*24*time.Hour {
		t.Errorf("weekly delta = %v, want ~7d", delta)
	}
}

func TestRecurringTaskNoneWhenNoRRule(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "n@n.test", "NoCo")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	created, err := st.CreateTask(ctx, tc, mkTask("One-off"))
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := st.CompleteTaskAndRecur(ctx, tc, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Errorf("did not expect next, got %+v", next)
	}
}

func TestReminderCreateAndFire(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "rem@rem.test", "RemCo")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	task, err := st.CreateTask(ctx, tc, mkTask("Buy milk"))
	if err != nil {
		t.Fatal(err)
	}
	// Schedule for 5 seconds ago so it's already due.
	fireAt := time.Now().Add(-5 * time.Second)
	rm, err := st.CreateReminder(ctx, tc, task.ID, fireAt)
	if err != nil {
		t.Fatal(err)
	}
	if rm.Status != "pending" {
		t.Errorf("status = %q, want pending", rm.Status)
	}

	due, err := st.FetchDueReminders(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != rm.ID {
		t.Errorf("expected 1 due reminder, got %+v", due)
	}
	for _, r := range due {
		if r.Status != "sent" {
			t.Errorf("after fetch, status = %q, want sent", r.Status)
		}
	}

	// A second fetch should now return none (they're marked sent).
	due2, _ := st.FetchDueReminders(ctx, time.Now())
	if len(due2) != 0 {
		t.Errorf("expected 0 due after fetch, got %d", len(due2))
	}
}
