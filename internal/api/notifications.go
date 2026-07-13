package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/anirudh-777/ttl/internal/tenant"
)

func (s *Server) handleCreateNotificationEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	secret, endpoint, err := s.Store.CreateNotificationEndpoint(r.Context(), tenant.MustFrom(r.Context()), req.Name, req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"secret": secret, "endpoint": endpoint})
}
func (s *Server) handleListNotificationEndpoints(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListNotificationEndpoints(r.Context(), tenant.MustFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoints": items})
}
func (s *Server) handleSetNotificationEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.Store.SetNotificationEndpointEnabled(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id"), req.Enabled); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleDeleteNotificationEndpoint(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteNotificationEndpoint(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
