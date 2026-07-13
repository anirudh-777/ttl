package ws

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anirudh-777/ttl/internal/auth"
	"github.com/anirudh-777/ttl/internal/db"
)

func TestResolveTokenRejectsExpiredAPIKey(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := auth.Signup(context.Background(), d, "WS", "ws@test.local", "password")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	raw, key, err := auth.IssueAPIKeyWithOptions(context.Background(), d, u, "ws", []string{"tasks:read"}, &expires)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Server{DB: d}).resolveToken(context.Background(), raw); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if _, err := d.Exec(`UPDATE api_keys SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).UnixMilli(), key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Server{DB: d}).resolveToken(context.Background(), raw); err == nil {
		t.Fatal("expired API key accepted by WebSocket auth")
	}
}
