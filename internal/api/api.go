// Package api wires HTTP handlers for the ttl server.
//
// Auth model:
//
//	Web UI    -> "ttl_session" cookie   -> auth.LookupSession
//	CLI/MCP   -> "X-API-Key: ttk_..."   -> auth.LookupAPIKey
//
// All handlers behind RequireAuth inject a tenant.Context into the
// request context; nothing below the middleware can read or write
// tenant data without one.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"database/sql"

	"github.com/anirudhprakash/ttl/internal/auth"
	"github.com/anirudhprakash/ttl/internal/events"
	"github.com/anirudhprakash/ttl/internal/model"
	"github.com/anirudhprakash/ttl/internal/store"
	"github.com/anirudhprakash/ttl/internal/tenant"
	"github.com/anirudhprakash/ttl/internal/ws"
)

// Server bundles dependencies for HTTP handlers.
type Server struct {
	DB    *sql.DB
	Store *store.Store
	Hub   *events.Hub
}

// New returns a chi router with all routes mounted.
func New(d *sql.DB, st *store.Store, hub *events.Hub) http.Handler {
	s := &Server{DB: d, Store: st, Hub: hub}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-API-Key"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// WebSocket upgrade (auth via ?token= or cookie). Mounted outside
	// the RequireAuth group because the upgrade handler does its own
	// token validation.
	wsSrv := &ws.Server{DB: d, Hub: hub}
	r.HandleFunc("/api/v1/ws", wsSrv.ServeHTTP)

	r.Post("/api/v1/auth/signup", s.handleSignup)
	r.Post("/api/v1/auth/login", s.handleLogin)

	// Webhook receiver (no session auth; HMAC verified per provider).
	// Registered before the auth-protected Route so chi picks this
	// specific path over the wildcard.
	r.Route("/api/v1/webhooks", func(r chi.Router) {
		r.Post("/{provider}", s.handleWebhook)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.RequireAuth)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/me", s.handleMe)

		r.Get("/projects", s.handleListProjects)
		r.Post("/projects", s.handleCreateProject)

		r.Get("/tags", s.handleListTags)
		r.Post("/tags", s.handleCreateTag)

		r.Get("/tasks", s.handleListTasks)
		r.Post("/tasks", s.handleCreateTask)
		r.Get("/tasks/{id}", s.handleGetTask)
		r.Patch("/tasks/{id}", s.handleUpdateTask)
		r.Post("/tasks/{id}/complete", s.handleCompleteTask)
		r.Delete("/tasks/{id}", s.handleDeleteTask)

		// Time tracking + work log.
		r.Post("/timer/start", s.handleTimerStart)
		r.Post("/timer/stop", s.handleTimerStop)
		r.Get("/timer/active", s.handleTimerActive)
		r.Get("/timer/entries", s.handleTimerList)
		r.Get("/worklog/today", s.handleWorklogToday)

		// Reminders.
		r.Post("/reminders", s.handleCreateReminder)
		r.Get("/reminders", s.handleListReminders)
		r.Delete("/reminders/{id}", s.handleDeleteReminder)

		// Integrations.
		r.Get("/integrations", s.handleListIntegrations)
		r.Post("/integrations", s.handleCreateIntegration)
		r.Delete("/integrations/{id}", s.handleDeleteIntegration)
		r.Post("/integrations/{id}/sync", s.handleSyncIntegration)

		r.Post("/api-keys", s.handleIssueAPIKey)
	})

	return r
}

// -------------------------- Middleware --------------------------

func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
			return
		}
		ctx := tenant.With(r.Context(), tc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authenticate(r *http.Request) (*tenant.Context, error) {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return auth.LookupAPIKey(r.Context(), s.DB, key)
	}
	if c, err := r.Cookie("ttl_session"); err == nil {
		return auth.LookupSession(r.Context(), s.DB, c.Value)
	}
	return nil, errors.New("no credentials provided")
}

// -------------------------- Auth handlers --------------------------

type signupReq struct {
	TenantName string `json:"tenant_name"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	u, err := auth.Signup(r.Context(), s.DB, req.TenantName, req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrEmailTaken):
			writeError(w, http.StatusConflict, "email_taken", err.Error())
		default:
			if strings.Contains(err.Error(), "required") {
				writeError(w, http.StatusBadRequest, "validation", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return
	}
	tok, exp, err := auth.CreateSession(r.Context(), s.DB, u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "ttl_session", Value: tok, Path: "/",
		Expires: exp, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	u, err := auth.Login(r.Context(), s.DB, req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_login", "invalid email or password")
		return
	}
	tok, exp, err := auth.CreateSession(r.Context(), s.DB, u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "ttl_session", Value: tok, Path: "/",
		Expires: exp, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("ttl_session"); err == nil {
		_ = auth.DestroySession(r.Context(), s.DB, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "ttl_session", Value: "", Path: "/",
		Expires: time.Unix(0, 0), MaxAge: -1, HttpOnly: true,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var email, role string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT email, role FROM users WHERE id = ?`, tc.UserID,
	).Scan(&email, &role); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":   tc.UserID,
		"tenant_id": tc.TenantID,
		"role":      role,
		"email":     email,
	})
}

// -------------------------- Project handlers --------------------------

type projectReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	ps, err := s.Store.ListProjects(r.Context(), tc, r.URL.Query().Get("archived") == "1")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req projectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := s.Store.CreateProject(r.Context(), tc, req.Name, req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// -------------------------- Tag handlers --------------------------

type tagReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	ts, err := s.Store.ListTags(r.Context(), tc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": ts})
}

func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req tagReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	t, err := s.Store.CreateTag(r.Context(), tc, req.Name, req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// -------------------------- Task handlers --------------------------

// taskCreateReq is the wire DTO. We accept due_at as a unix-ms number
// for portability with the CLI and JSON consumers.
type taskCreateReq struct {
	Title           string   `json:"title"`
	Notes           string   `json:"notes"`
	Priority        int      `json:"priority"`
	ProjectID       *string  `json:"project_id"`
	ParentID        *string  `json:"parent_id"`
	DueAt           *int64   `json:"due_at"`
	RecurrenceRRule *string  `json:"recurrence_rrule"`
	Tags            []string `json:"tags"`
}

func (req taskCreateReq) toModel() *model.Task {
	t := &model.Task{
		Title:           req.Title,
		Notes:           req.Notes,
		Priority:        req.Priority,
		ProjectID:       req.ProjectID,
		ParentID:        req.ParentID,
		RecurrenceRRule: req.RecurrenceRRule,
		Tags:            req.Tags,
		Status:          "open",
	}
	if req.DueAt != nil {
		v := time.UnixMilli(*req.DueAt)
		t.DueAt = &v
	}
	return t
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req taskCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	t, err := s.Store.CreateTask(r.Context(), tc, req.toModel())
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	if s.Hub != nil {
		s.Hub.Publish(events.Event{
			Kind:     events.KindTaskCreated,
			TenantID: tc.TenantID,
			UserID:   tc.UserID,
			Payload:  map[string]any{"id": t.ID, "title": t.Title},
		})
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	q := r.URL.Query()
	f := store.TaskFilter{
		Status:    q.Get("status"),
		ProjectID: q.Get("project_id"),
		TagID:     q.Get("tag_id"),
		Search:    q.Get("q"),
		Overdue:   q.Get("overdue") == "1",
	}
	if p := q.Get("parent_id"); p != "" {
		v := p
		if p == "root" {
			empty := ""
			f.ParentID = &empty
		} else {
			f.ParentID = &v
		}
	}
	limit := 200
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	tasks, err := s.Store.ListTasks(r.Context(), tc, f, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	t, err := s.Store.GetTask(r.Context(), tc, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if subs, err := s.Store.Subtasks(r.Context(), tc, t.ID); err == nil {
		t.Subtasks = subs
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	var fields map[string]any
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if v, ok := fields["due_at"]; ok && v != nil {
		if n, ok := v.(float64); ok {
			fields["due_at"] = int64(n)
		}
	}
	t, err := s.Store.UpdateTask(r.Context(), tc, id, fields)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
			return
		}
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	completed, next, err := s.Store.CompleteTaskAndRecur(r.Context(), tc, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if s.Hub != nil {
		s.Hub.Publish(events.Event{
			Kind:     events.KindTaskCompleted,
			TenantID: tc.TenantID,
			UserID:   tc.UserID,
			Payload:  map[string]any{"id": completed.ID, "title": completed.Title},
		})
		if next != nil {
			s.Hub.Publish(events.Event{
				Kind:     events.KindTaskCreated,
				TenantID: tc.TenantID,
				UserID:   tc.UserID,
				Payload:  map[string]any{"id": next.ID, "title": next.Title, "from_recurrence": true},
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":          completed,
		"next_occurred": next,
	})
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := s.Store.DeleteTask(r.Context(), tc, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if s.Hub != nil {
		s.Hub.Publish(events.Event{
			Kind:     events.KindTaskDeleted,
			TenantID: tc.TenantID,
			Payload:  map[string]any{"id": id},
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// -------------------------- API key handlers --------------------------

type apiKeyReq struct {
	Name string `json:"name"`
}

func (s *Server) handleIssueAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req apiKeyReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	u := &model.User{ID: tc.UserID, TenantID: tc.TenantID}
	plain, k, err := auth.IssueAPIKey(r.Context(), s.DB, u, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     plain,
		"api_key": k,
	})
}

// -------------------------- Helpers --------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

var _ = context.Background
var _ = fmt.Sprintf
