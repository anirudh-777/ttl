package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/anirudhprakash/ttl/internal/auth"
	"github.com/anirudhprakash/ttl/internal/db"
	"github.com/anirudhprakash/ttl/internal/model"
	"github.com/anirudhprakash/ttl/internal/store"
	"github.com/anirudhprakash/ttl/internal/tenant"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func signup(t *testing.T, d *sql.DB, email, tenantName string) *model.User {
	t.Helper()
	u, err := auth.Signup(context.Background(), d, tenantName, email, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mkTask(title string) *model.Task {
	due := time.Now().Add(24 * time.Hour)
	return &model.Task{
		Title:  title,
		Status: "open",
		Notes:  "",
		DueAt:  &due,
	}
}

func TestSignupAndLogin(t *testing.T) {
	d := openTestDB(t)

	if _, err := auth.Signup(context.Background(), d, "Acme", "alice@acme.test", "hunter2"); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if _, err := auth.Login(context.Background(), d, "alice@acme.test", "hunter2"); err != nil {
		t.Errorf("login with correct password: %v", err)
	}
	if _, err := auth.Login(context.Background(), d, "alice@acme.test", "wrong"); err == nil {
		t.Error("expected login to fail with wrong password")
	}
}

func TestCrossTenantIsolation(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()

	alice := signup(t, d, "alice@a.test", "AliceCo")
	bob := signup(t, d, "bob@b.test", "BobCo")
	tcAlice := &tenant.Context{TenantID: alice.TenantID, UserID: alice.ID, Role: alice.Role}
	tcBob := &tenant.Context{TenantID: bob.TenantID, UserID: bob.ID, Role: bob.Role}

	a, err := st.CreateTask(ctx, tcAlice, mkTask("Alice task"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateTask(ctx, tcBob, mkTask("Bob task"))
	if err != nil {
		t.Fatal(err)
	}

	// Bob must not read Alice's task.
	if _, err := st.GetTask(ctx, tcBob, a.ID); err == nil {
		t.Error("expected cross-tenant GetTask to fail")
	}
	// Alice must not update Bob's task.
	if _, err := st.UpdateTask(ctx, tcAlice, b.ID, map[string]any{"title": "hacked"}); err == nil {
		t.Error("expected cross-tenant UpdateTask to fail")
	}
	// Alice must not delete Bob's task.
	if err := st.DeleteTask(ctx, tcAlice, b.ID); err == nil {
		t.Error("expected cross-tenant DeleteTask to fail")
	}
	// Lists are scoped.
	alist, _ := st.ListTasks(ctx, tcAlice, store.TaskFilter{}, 100)
	blist, _ := st.ListTasks(ctx, tcBob, store.TaskFilter{}, 100)
	if len(alist) != 1 || alist[0].ID != a.ID {
		t.Errorf("alice list wrong: %+v", alist)
	}
	if len(blist) != 1 || blist[0].ID != b.ID {
		t.Errorf("bob list wrong: %+v", blist)
	}
}

func TestTaskCRUD(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "a@a.test", "Acme")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	p, err := st.CreateProject(ctx, tc, "home", "#ff0000")
	if err != nil {
		t.Fatal(err)
	}
	pid := p.ID
	in := mkTask("Buy milk")
	in.ProjectID = &pid
	in.Tags = []string{"urgent", "shopping"}
	created, err := st.CreateTask(ctx, tc, in)
	if err != nil {
		t.Fatal(err)
	}
	if created.Title != "Buy milk" {
		t.Errorf("title roundtrip lost: %q", created.Title)
	}
	if len(created.Tags) != 2 {
		t.Errorf("tags roundtrip lost: %v", created.Tags)
	}

	out, err := st.ListTasks(ctx, tc, store.TaskFilter{Search: "milk"}, 10)
	if err != nil || len(out) != 1 {
		t.Errorf("search: out=%v err=%v", out, err)
	}

	done, err := st.CompleteTask(ctx, tc, created.ID)
	if err != nil || done.Status != "done" {
		t.Errorf("complete: status=%q err=%v", done.Status, err)
	}
	if done.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}

	if err := st.DeleteTask(ctx, tc, created.ID); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := st.GetTask(ctx, tc, created.ID); err == nil {
		t.Error("expected not-found after delete")
	}
}

func TestAPIKeyLookup(t *testing.T) {
	d := openTestDB(t)
	u := signup(t, d, "k@k.test", "Acme")
	plain, _, err := auth.IssueAPIKey(context.Background(), d, u, "cli")
	if err != nil {
		t.Fatal(err)
	}
	tc, err := auth.LookupAPIKey(context.Background(), d, plain)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if tc.TenantID != u.TenantID || tc.UserID != u.ID {
		t.Errorf("api key did not resolve to user: %+v", tc)
	}
	if _, err := auth.LookupAPIKey(context.Background(), d, "ttk_bogus"); err == nil {
		t.Error("expected bogus key to fail")
	}
}
