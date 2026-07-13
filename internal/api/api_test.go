package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/anirudh-777/ttl/internal/api"
	"github.com/anirudh-777/ttl/internal/auth"
	"github.com/anirudh-777/ttl/internal/db"
	"github.com/anirudh-777/ttl/internal/events"
	"github.com/anirudh-777/ttl/internal/store"
)

type apiFixture struct {
	db      *sql.DB
	handler http.Handler
	key     string
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	u, err := auth.Signup(context.Background(), d, "API", "api@test.local", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := auth.IssueAPIKey(context.Background(), d, u, "test")
	if err != nil {
		t.Fatal(err)
	}
	return &apiFixture{db: d, handler: api.New(d, store.New(d), events.New()), key: key}
}

func (f *apiFixture) request(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-API-Key", f.key)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	return rr
}

func TestTaskAPIUpdateTrashRestorePurge(t *testing.T) {
	f := newAPIFixture(t)
	created := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "API task", "tags": []string{"one"},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var task struct {
		ID    string  `json:"id"`
		DueAt *string `json:"due_at"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.DueAt == nil {
		t.Fatal("expected tasks created without a due date to default to today")
	}

	updated := f.request(t, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"title": "updated", "tags": []string{"two"},
	})
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"two"`)) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	trashed := f.request(t, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil)
	if trashed.Code != http.StatusNoContent {
		t.Fatalf("trash status=%d body=%s", trashed.Code, trashed.Body.String())
	}
	trash := f.request(t, http.MethodGet, "/api/v1/tasks?view=trash", nil)
	if trash.Code != http.StatusOK || !bytes.Contains(trash.Body.Bytes(), []byte(task.ID)) {
		t.Fatalf("trash list status=%d body=%s", trash.Code, trash.Body.String())
	}
	restored := f.request(t, http.MethodPost, "/api/v1/tasks/"+task.ID+"/restore", nil)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restored.Code, restored.Body.String())
	}
	_ = f.request(t, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil)
	purged := f.request(t, http.MethodDelete, "/api/v1/tasks/"+task.ID+"/purge", nil)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("purge status=%d body=%s", purged.Code, purged.Body.String())
	}
}

func TestTaskAPIRequiresAuthentication(t *testing.T) {
	f := newAPIFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
