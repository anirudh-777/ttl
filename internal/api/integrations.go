// Integration HTTP handlers.

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/anirudhprakash/ttl/internal/integrations"
	"github.com/anirudhprakash/ttl/internal/model"
	"github.com/anirudhprakash/ttl/internal/store"
	"github.com/anirudhprakash/ttl/internal/tenant"
)

type integrationCreateReq struct {
	Provider string            `json:"provider"`
	Label    string            `json:"label"`
	Config   map[string]string `json:"config"`
}

func (s *Server) handleCreateIntegration(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req integrationCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, ok := providers[req.Provider]
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "unknown provider: "+req.Provider)
		return
	}
	it, err := s.Store.CreateIntegration(r.Context(), tc, req.Provider, req.Label, req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	// Verify the token works (best-effort) using a 10s deadline.
	verifyCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err := p.FetchAssigned(verifyCtx, it.Config); err != nil {
		_ = s.Store.DeleteIntegration(r.Context(), tc, it.ID)
		writeError(w, http.StatusBadGateway, "provider_check_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	its, err := s.Store.ListIntegrations(r.Context(), tc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"integrations": its})
}

func (s *Server) handleDeleteIntegration(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := s.Store.DeleteIntegration(r.Context(), tc, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "integration not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSyncIntegration pulls from the upstream provider now and
// reconciles into ttl tasks.
func (s *Server) handleSyncIntegration(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	it, err := s.Store.GetIntegration(r.Context(), tc, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "integration not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	p, ok := providers[it.Provider]
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "provider not loaded: "+it.Provider)
		return
	}
	syncer := integrations.New(s.Store)
	stats, err := syncer.Sync(r.Context(), tc, id, p)
	if err != nil {
		writeError(w, http.StatusBadGateway, "sync_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleWebhook is a public endpoint. Auth is by HMAC signature per
// provider, plus a header identifying the integration.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	p, ok := providers[providerName]
	if !ok {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}
	intID := r.Header.Get("X-Ttl-Integration")
	if intID == "" {
		http.Error(w, "missing X-Ttl-Integration header", http.StatusBadRequest)
		return
	}
	it, secret, err := s.lookupWebhookSecret(r.Context(), providerName, intID)
	if err != nil {
		http.Error(w, "integration not found", http.StatusNotFound)
		return
	}
	ev, err := p.VerifyWebhook(r, secret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	tc := &tenant.Context{TenantID: it.TenantID, UserID: it.CreatedBy, Role: "owner"}
	syncer := integrations.New(s.Store)
	if _, err := syncer.HandleWebhook(r.Context(), tc, it.ID, p, ev); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// providers is the registry of loaded Provider implementations.
var providers = map[string]integrations.Provider{
	"github": integrations.NewGitHub(),
	"linear": integrations.NewLinear(),
}

// lookupWebhookSecret finds an integration by id (admin path — no
// tenant context), returning the integration + the webhook secret
// stored in config_json under "webhook_secret".
func (s *Server) lookupWebhookSecret(ctx context.Context, provider, id string) (*model.Integration, string, error) {
	const q = `
		SELECT id, tenant_id, provider, label, config_json,
		       created_by, created_at, last_synced_at
		FROM integrations WHERE id = ? AND provider = ?`
	var (
		it         model.Integration
		cfgJSON    string
		createdBy  string
		createdAt  int64
		lastSynced sql.NullInt64
	)
	err := s.DB.QueryRowContext(ctx, q, id, provider).Scan(
		&it.ID, &it.TenantID, &it.Provider, &it.Label, &cfgJSON,
		&createdBy, &createdAt, &lastSynced,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", store.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	_ = json.Unmarshal([]byte(cfgJSON), &it.Config)
	it.CreatedBy = createdBy
	it.CreatedAt = time.UnixMilli(createdAt)
	if lastSynced.Valid {
		t := time.UnixMilli(lastSynced.Int64)
		it.LastSyncedAt = &t
	}
	return &it, it.Config["webhook_secret"], nil
}
