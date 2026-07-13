package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tenant"
)

func TestStartStopTimer(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "t@t.test", "Acme")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	// Start.
	e, err := st.StartTimeEntry(ctx, tc, nil, "work", "first")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != "open" {
		t.Errorf("status = %q, want open", e.Status)
	}

	// Starting a second one must fail.
	if _, err := st.StartTimeEntry(ctx, tc, nil, "work", ""); !errors.Is(err, store.ErrTimerAlreadyRunning) {
		t.Errorf("expected ErrTimerAlreadyRunning, got %v", err)
	}

	// Wait long enough that duration is measurable.
	time.Sleep(15 * time.Millisecond)

	// Stop it.
	stopped, err := st.StopTimeEntry(ctx, tc, "done")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Errorf("status = %q, want stopped", stopped.Status)
	}
	if stopped.DurationMs <= 0 {
		t.Errorf("duration = %d, want >0", stopped.DurationMs)
	}
	if stopped.Note != "first\ndone" {
		t.Errorf("note = %q, want %q", stopped.Note, "first\ndone")
	}

	// After stop, we can start again.
	if _, err := st.StartTimeEntry(ctx, tc, nil, "pomodoro", ""); err != nil {
		t.Errorf("re-start: %v", err)
	}
	// Stop the pomodoro before testing the "no active timer" path.
	if _, err := st.StopTimeEntry(ctx, tc, ""); err != nil {
		t.Fatal(err)
	}

	// Stopping with no active timer fails.
	if _, err := st.StopTimeEntry(ctx, tc, ""); !errors.Is(err, store.ErrNoActiveTimer) {
		t.Errorf("expected ErrNoActiveTimer, got %v", err)
	}
}

func TestTimerScopingAcrossTenants(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	alice := signup(t, d, "a@t.test", "AcmeA")
	bob := signup(t, d, "b@t.test", "AcmeB")
	tcA := &tenant.Context{TenantID: alice.TenantID, UserID: alice.ID, Role: alice.Role}
	tcB := &tenant.Context{TenantID: bob.TenantID, UserID: bob.ID, Role: bob.Role}

	if _, err := st.StartTimeEntry(ctx, tcA, nil, "work", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StopTimeEntry(ctx, tcA, ""); err != nil {
		t.Fatal(err)
	}

	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(24 * time.Hour)
	bobEntries, _ := st.ListTimeEntries(ctx, tcB, from, to, 100)
	for _, e := range bobEntries {
		if e.UserID == alice.ID {
			t.Errorf("Bob saw Alice's entry: %+v", e)
		}
	}
}

func TestDailySummary(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "s@s.test", "AcmeS")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	t1, err := st.CreateTask(ctx, tc, mkTask("Write docs"))
	if err != nil {
		t.Fatal(err)
	}
	pid := t1.ID
	for _, kind := range []string{"work", "work"} {
		_, err := st.StartTimeEntry(ctx, tc, &pid, kind, "")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if _, err := st.StopTimeEntry(ctx, tc, ""); err != nil {
			t.Fatal(err)
		}
	}

	summary, err := st.DailySummary(ctx, tc, time.Now(), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalMs <= 0 {
		t.Errorf("total = %d, want >0", summary.TotalMs)
	}
	if len(summary.PerTask) == 0 || summary.PerTask[0].TaskTitle != "Write docs" {
		t.Errorf("per-task breakdown missing title: %+v", summary.PerTask)
	}
}

func TestActiveTimerLookup(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "a@a.test", "Acme")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	// No active timer.
	if got, err := st.ActiveTimeEntry(ctx, tc); err != nil || got != nil {
		t.Errorf("expected nil entry, got %+v err=%v", got, err)
	}

	// Start one.
	e, err := st.StartTimeEntry(ctx, tc, nil, "work", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.ActiveTimeEntry(ctx, tc)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != e.ID {
		t.Errorf("active = %+v, want id %s", got, e.ID)
	}
}

func TestPomodoroAutoStopsAtDeadline(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	u := signup(t, d, "pomodoro@test.local", "Pomodoro")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}
	entry, err := st.StartTimedEntry(context.Background(), tc, nil, "pomodoro", "focus", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := st.StopExpiredTimers(context.Background(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0].ID != entry.ID || stopped[0].EndedAt == nil {
		t.Fatalf("stopped=%+v", stopped)
	}
	active, err := st.ActiveTimeEntry(context.Background(), tc)
	if err != nil || active != nil {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}
