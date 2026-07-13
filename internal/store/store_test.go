package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/anirudh-777/ttl/internal/auth"
	"github.com/anirudh-777/ttl/internal/db"
	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tenant"
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

func TestTaskTrashRestoreAndPurge(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "trash@test.local", "Trash")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	parent, err := st.CreateTask(ctx, tc, mkTask("parent"))
	if err != nil {
		t.Fatal(err)
	}
	childIn := mkTask("child")
	childIn.ParentID = &parent.ID
	child, err := st.CreateTask(ctx, tc, childIn)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TrashTask(ctx, tc, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTask(ctx, tc, parent.ID); err == nil {
		t.Fatal("trashed parent remained visible")
	}
	trash, err := st.ListTasks(ctx, tc, store.TaskFilter{Deleted: true}, 10)
	if err != nil || len(trash) != 2 {
		t.Fatalf("trash=%v err=%v", trash, err)
	}
	if err := st.RestoreTask(ctx, tc, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTask(ctx, tc, child.ID); err != nil {
		t.Fatalf("child not restored: %v", err)
	}
	if err := st.TrashTask(ctx, tc, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeTask(ctx, tc, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTaskIncludingDeleted(ctx, tc, child.ID); err == nil {
		t.Fatal("purging parent did not cascade")
	}
}

func TestTrashRetentionPurge(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "retention@test.local", "Retention")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}
	old, _ := st.CreateTask(ctx, tc, mkTask("old"))
	recent, _ := st.CreateTask(ctx, tc, mkTask("recent"))
	if err := st.TrashTask(ctx, tc, old.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.TrashTask(ctx, tc, recent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `UPDATE tasks SET deleted_at = ? WHERE id = ?`, time.Now().Add(-48*time.Hour).UnixMilli(), old.ID); err != nil {
		t.Fatal(err)
	}
	n, err := st.PurgeTrashOlderThan(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged=%d", n)
	}
	if _, err := st.GetTaskIncludingDeleted(ctx, tc, old.ID); err == nil {
		t.Fatal("old task retained")
	}
	if _, err := st.GetTaskIncludingDeleted(ctx, tc, recent.ID); err != nil {
		t.Fatal("recent task purged")
	}
}

func TestTaskTagReplacementAndManualPosition(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "order@test.local", "Order")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}

	first := mkTask("first")
	first.DueAt = nil
	first.Tags = []string{"one", "two"}
	a, err := st.CreateTask(ctx, tc, first)
	if err != nil {
		t.Fatal(err)
	}
	second := mkTask("second")
	second.DueAt = nil
	b, err := st.CreateTask(ctx, tc, second)
	if err != nil {
		t.Fatal(err)
	}
	if b.Position <= a.Position {
		t.Fatalf("positions not increasing: %d then %d", a.Position, b.Position)
	}
	updated, err := st.UpdateTask(ctx, tc, a.ID, map[string]any{"tags": []string{"three"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "three" {
		t.Fatalf("tags not replaced: %v", updated.Tags)
	}
	inbox, err := st.ListTasks(ctx, tc, store.TaskFilter{Inbox: true, Order: "manual"}, 10)
	if err != nil || len(inbox) != 2 || inbox[0].ID != a.ID {
		t.Fatalf("manual inbox=%v err=%v", inbox, err)
	}
}

func TestTaskReorderAndCycleRejection(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	u := signup(t, d, "rank@test.local", "Rank")
	tc := &tenant.Context{TenantID: u.TenantID, UserID: u.ID, Role: u.Role}
	a, _ := st.CreateTask(ctx, tc, mkTask("a"))
	b, _ := st.CreateTask(ctx, tc, mkTask("b"))
	if _, err := st.ReorderTask(ctx, tc, b.ID, nil, nil, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListTasks(ctx, tc, store.TaskFilter{Inbox: true, Order: "manual"}, 10)
	if err != nil || len(items) != 2 || items[0].ID != b.ID {
		t.Fatalf("items=%v err=%v", items, err)
	}
	parent := a.ID
	if _, err := st.ReorderTask(ctx, tc, b.ID, nil, &parent, "", ""); err != nil {
		t.Fatal(err)
	}
	child := b.ID
	if _, err := st.ReorderTask(ctx, tc, a.ID, nil, &child, "", ""); err == nil {
		t.Fatal("expected hierarchy cycle rejection")
	}
}

func TestTaskMutationsValidateTenantRelationsAndFields(t *testing.T) {
	d := openTestDB(t)
	st := store.New(d)
	ctx := context.Background()
	alice := signup(t, d, "mutation-a@test.local", "Mutation A")
	bob := signup(t, d, "mutation-b@test.local", "Mutation B")
	tcA := &tenant.Context{TenantID: alice.TenantID, UserID: alice.ID, Role: alice.Role}
	tcB := &tenant.Context{TenantID: bob.TenantID, UserID: bob.ID, Role: bob.Role}
	task, err := st.CreateTask(ctx, tcA, &model.Task{Title: "valid", Tags: []string{"keep"}})
	if err != nil {
		t.Fatal(err)
	}
	foreignProject, err := st.CreateProject(ctx, tcB, "foreign", "")
	if err != nil {
		t.Fatal(err)
	}
	foreignParent, err := st.CreateTask(ctx, tcB, mkTask("foreign parent"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReorderTask(ctx, tcA, task.ID, &foreignProject.ID, nil, "", ""); err == nil {
		t.Fatal("accepted cross-tenant project during reorder")
	}
	if _, err := st.ReorderTask(ctx, tcA, task.ID, nil, &foreignParent.ID, "", ""); err == nil {
		t.Fatal("accepted cross-tenant parent during reorder")
	}
	for _, fields := range []map[string]any{
		{"title": "  "},
		{"status": "unknown"},
		{"priority": float64(1.5)},
		{"priority": float64(4)},
	} {
		if _, err := st.UpdateTask(ctx, tcA, task.ID, fields); err == nil {
			t.Fatalf("accepted invalid fields: %#v", fields)
		}
	}
	updated, err := st.UpdateTask(ctx, tcA, task.ID, map[string]any{"tags": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tags) != 0 {
		t.Fatalf("empty tag replacement retained tags: %v", updated.Tags)
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
