// Time tracking + work-log HTTP handlers.

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// -------------------------- Timer --------------------------

type timerStartReq struct {
	TaskID  string `json:"task_id"` // optional
	Kind    string `json:"kind"`    // work | pomodoro
	Note    string `json:"note"`
	Minutes int    `json:"minutes"`
}

func (s *Server) handleTimerStart(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req timerStartReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	var taskID *string
	if req.TaskID != "" {
		// Validate task belongs to tenant.
		if _, err := s.Store.GetTask(r.Context(), tc, req.TaskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "task not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		id := req.TaskID
		taskID = &id
	}

	var planned time.Duration
	if req.Kind == "pomodoro" {
		if req.Minutes <= 0 {
			req.Minutes = 25
		}
		planned = time.Duration(req.Minutes) * time.Minute
	}
	e, err := s.Store.StartTimedEntry(r.Context(), tc, taskID, req.Kind, req.Note, planned)
	if err != nil {
		if errors.Is(err, store.ErrTimerAlreadyRunning) {
			writeError(w, http.StatusConflict, "timer_already_running", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

type timerStopReq struct {
	Note string `json:"note"`
}

func (s *Server) handleTimerStop(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req timerStopReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	e, err := s.Store.StopTimeEntry(r.Context(), tc, req.Note)
	if err != nil {
		if errors.Is(err, store.ErrNoActiveTimer) {
			writeError(w, http.StatusNotFound, "no_active_timer", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleTimerActive(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	e, err := s.Store.ActiveTimeEntry(r.Context(), tc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if e == nil {
		writeJSON(w, http.StatusOK, map[string]any{"entry": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": e})
}

func (s *Server) handleTimerList(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	q := r.URL.Query()
	from := todayStart(time.Local)
	to := from.Add(24 * time.Hour)
	if s := q.Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			from = t
		}
	}
	if s := q.Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			to = t
		}
	}
	entries, err := s.Store.ListTimeEntries(r.Context(), tc, from, to, 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// handleWorklogToday returns today's aggregated work log.
func (s *Server) handleWorklogToday(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	loc := time.Local
	if tz := r.URL.Query().Get("tz"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	day := time.Now().In(loc)
	summary, err := s.Store.DailySummary(r.Context(), tc, day, loc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	active, _ := s.Store.ActiveTimeEntry(r.Context(), tc)
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": summary,
		"active":  active,
	})
}

func todayStart(loc *time.Location) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
}
