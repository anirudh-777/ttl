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
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/anirudh-777/ttl/internal/auth"
	"github.com/anirudh-777/ttl/internal/events"
	"github.com/anirudh-777/ttl/internal/tenant"
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

	c, err := websocket.Accept(w, r, nil)
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
		return auth.LookupAPIKey(ctx, s.DB, tok)
	}
	return auth.LookupSession(ctx, s.DB, tok)
}

type tenantCtx = tenant.Context

// jsonEncode is a tiny shim that lets us avoid importing encoding/json
// at the top of the file (we use it from one place).
func jsonEncode(e events.Event) ([]byte, error) {
	return jsonMarshal(e)
}
