package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/anirudh-777/ttl/internal/auth"
	"github.com/anirudh-777/ttl/internal/tenant"
)

type inviteCreateReq struct {
	Role      string `json:"role"`
	ExpiresAt *int64 `json:"expires_at"`
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req inviteCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	if req.ExpiresAt != nil {
		expires = time.UnixMilli(*req.ExpiresAt)
	}
	token, invite, err := auth.CreateInvite(r.Context(), s.DB, tc, req.Role, expires)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "invite": invite})
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	items, err := auth.ListInvites(r.Context(), s.DB, tc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": items})
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	items, err := auth.ListMembers(r.Context(), s.DB, tc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": items})
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := auth.SetMemberRole(r.Context(), s.DB, tc, chi.URLParam(r, "id"), req.Role); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	if err := auth.RemoveMember(r.Context(), s.DB, tc, chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
