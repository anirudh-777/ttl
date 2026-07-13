// Reminder HTTP handlers.

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tenant"
)

type reminderReq struct {
	TaskID     string  `json:"task_id"`
	FireAt     int64   `json:"fire_at"` // unix ms
	EndpointID *string `json:"endpoint_id"`
}

func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req reminderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.TaskID == "" || req.FireAt <= 0 {
		writeError(w, http.StatusBadRequest, "validation", "task_id and fire_at required")
		return
	}
	rm, err := s.Store.CreateReminderWithEndpoint(r.Context(), tc, req.TaskID, time.UnixMilli(req.FireAt), req.EndpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rm)
}

func (s *Server) handleListReminders(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	rs, err := s.Store.ListReminders(r.Context(), tc, r.URL.Query().Get("status"), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": rs})
}

func (s *Server) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := s.Store.DeleteReminder(r.Context(), tc, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "reminder not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateReminder(w http.ResponseWriter, r *http.Request) {
	s.updateReminderTime(w, r, false)
}

func (s *Server) handleSnoozeReminder(w http.ResponseWriter, r *http.Request) {
	s.updateReminderTime(w, r, true)
}

func (s *Server) updateReminderTime(w http.ResponseWriter, r *http.Request, snooze bool) {
	tc := tenant.MustFrom(r.Context())
	var req reminderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.FireAt <= 0 {
		writeError(w, http.StatusBadRequest, "validation", "fire_at required")
		return
	}
	when := time.UnixMilli(req.FireAt)
	var rm *model.Reminder
	var err error
	if snooze {
		rm, err = s.Store.SnoozeReminder(r.Context(), tc, chi.URLParam(r, "id"), when)
	} else {
		rm, err = s.Store.UpdateReminder(r.Context(), tc, chi.URLParam(r, "id"), when)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rm)
}

func (s *Server) handleAcknowledgeReminder(w http.ResponseWriter, r *http.Request) {
	rm, err := s.Store.AcknowledgeReminder(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rm)
}
