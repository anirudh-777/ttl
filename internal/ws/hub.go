// Package ws handles WebSocket upgrades and forwards events from the
// in-process events hub to connected clients. Clients connect to
// /api/v1/ws and authenticate with a query string token:
//
//   /api/v1/ws?token=ttk_xxx      (API key)
//
// The server derives the tenant context from the token and tags every
// outbound event with the matching tenant_id; events from other
// tenants are dropped on the floor (defence in depth on top of the
// store's row-level scoping).

package ws

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/anirudhprakash/ttl/internal/auth"
	"github.com/anirudhprakash/ttl/internal/events"
)

// Server wires the events hub into HTTP upgrades.
type Server struct {
	DB   *sql.DB
	Hub  *events.Hub
	HTTP *http.Client
}

// ServeHTTP upgrades the connection and pumps events.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		// Allow cookie auth too.
		if c, err := r.Cookie("ttl_session"); err == nil {
			tok = c.Value
		}
	}
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	tc, err := s.resolveToken(r.Context(), tok)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // dev: serve UI over http
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "bye")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub, unsub := s.Hub.Subscribe(64)
	defer unsub()

	// Ping loop in another goroutine to keep the connection alive.
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = c.Ping(ctx)
			}
		}
	}()

	// Send a hello so the client knows it's connected.
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"kind":"hello"}`))

	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub:
			if !ok {
				return
			}
			// Tenant filter (defence in depth).
			if e.TenantID != tc.TenantID {
				continue
			}
			b, err := jsonEncode(e)
			if err != nil {
				continue
			}
			if err := c.Write(ctx, websocket.MessageText, b); err != nil {
				return
			}
		}
	}
}

func (s *Server) resolveToken(ctx context.Context, tok string) (*tenantCtx, error) {
	if strings.HasPrefix(tok, "ttk_") {
		return lookupAPIKey(ctx, s.DB, tok)
	}
	return lookupSession(ctx, s.DB, tok)
}

// shims so we don't import the auth/sql packages here at the top.
type tenantCtx = struct {
	TenantID string
	UserID   string
	Role     string
}

func lookupAPIKey(ctx context.Context, db *sql.DB, plain string) (*tenantCtx, error) {
	hash := auth.HashAPIKey(plain)
	row := db.QueryRowContext(ctx,
		`SELECT ak.tenant_id, ak.user_id, u.role
		 FROM api_keys ak JOIN users u ON u.id = ak.user_id
		 WHERE ak.key_hash = ?`, hash)
	var tc tenantCtx
	if err := row.Scan(&tc.TenantID, &tc.UserID, &tc.Role); err != nil {
		return nil, errors.New("invalid api key")
	}
	return &tc, nil
}

func lookupSession(ctx context.Context, db *sql.DB, tok string) (*tenantCtx, error) {
	row := db.QueryRowContext(ctx,
		`SELECT s.tenant_id, s.user_id, u.role
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.id = ? AND s.expires_at > ?`, tok, time.Now().UnixMilli())
	var tc tenantCtx
	if err := row.Scan(&tc.TenantID, &tc.UserID, &tc.Role); err != nil {
		return nil, errors.New("invalid session")
	}
	return &tc, nil
}

// jsonEncode is a tiny shim that lets us avoid importing encoding/json
// at the top of the file (we use it from one place).
func jsonEncode(e events.Event) ([]byte, error) {
	return jsonMarshal(e)
}
