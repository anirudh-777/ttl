package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anirudh-777/ttl/internal/client"
)

func TestToolSchemasExposeCompleteTaskCRUD(t *testing.T) {
	want := map[string]bool{"add_task": false, "list_tasks": false, "show_task": false, "update_task": false, "complete_task": false, "delete_task": false, "restore_task": false, "purge_task": false, "reorder_task": false}
	for _, tool := range allTools() {
		name, _ := tool["name"].(string)
		if _, ok := want[name]; ok {
			want[name] = true
		}
		if name == "add_task" {
			schema := tool["inputSchema"].(map[string]any)
			props := schema["properties"].(map[string]any)
			if props["priority"].(map[string]any)["type"] != "integer" {
				t.Fatal("priority schema is not integer")
			}
			if props["tags"].(map[string]any)["type"] != "array" {
				t.Fatal("tags schema is not array")
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing tool %s", name)
		}
	}
}

func TestParseReminderMoment(t *testing.T) {
	before := time.Now()
	got, err := parseReminderMoment("+15m")
	if err != nil {
		t.Fatal(err)
	}
	if got.Before(before.Add(14*time.Minute)) || got.After(before.Add(16*time.Minute)) {
		t.Fatalf("got %v", got)
	}
	if _, err := parseReminderMoment("nonsense"); err == nil {
		t.Fatal("expected invalid time")
	}
}

func TestResolveTaskIDSupportsPrefixesAndTrash(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	var gotView string
	c := client.New("http://ttl.test", "test")
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotView = r.URL.Query().Get("view")
		body := fmt.Sprintf(`{"tasks":[{"id":%q,"title":"task","status":"open"}]}`, id)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	s := &server{client: c}
	got, err := s.resolveTaskID(context.Background(), "12345678", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != id || gotView != "trash" {
		t.Fatalf("got id=%q view=%q", got, gotView)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
