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
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"database/sql"

	"github.com/anirudh-777/ttl/internal/auth"
	"github.com/anirudh-777/ttl/internal/events"
	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/recurrence"
	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tenant"
	"github.com/anirudh-777/ttl/internal/ws"
)

// Server bundles dependencies for HTTP handlers.
type Server struct {
	DB           *sql.DB
	Store        *store.Store
	Hub          *events.Hub
	authMu       sync.Mutex
	authAttempts map[string][]time.Time
}

// New returns a chi router with all routes mounted.
func New(d *sql.DB, st *store.Store, hub *events.Hub) http.Handler {
	s := &Server{DB: d, Store: st, Hub: hub, authAttempts: map[string][]time.Time{}}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	if raw := strings.TrimSpace(os.Getenv("TTL_ALLOWED_ORIGINS")); raw != "" {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins: strings.Split(raw, ","), AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{"Content-Type", "X-API-Key"}, AllowCredentials: true, MaxAge: 300,
		}))
	}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Body != nil {
				req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
			}
			next.ServeHTTP(w, req)
		})
	})

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
		r.Patch("/projects/{id}", s.handleUpdateProject)
		r.Post("/projects/{id}/archive", s.handleArchiveProject)
		r.Post("/projects/{id}/restore", s.handleRestoreProject)
		r.Delete("/projects/{id}/purge", s.handlePurgeProject)

		r.Get("/tags", s.handleListTags)
		r.Post("/tags", s.handleCreateTag)
		r.Patch("/tags/{id}", s.handleUpdateTag)
		r.Post("/tags/{id}/merge", s.handleMergeTag)
		r.Delete("/tags/{id}", s.handleDeleteTag)

		r.Get("/tasks", s.handleListTasks)
		r.Post("/tasks", s.handleCreateTask)
		r.Get("/tasks/{id}", s.handleGetTask)
		r.Patch("/tasks/{id}", s.handleUpdateTask)
		r.Post("/tasks/{id}/complete", s.handleCompleteTask)
		r.Delete("/tasks/{id}", s.handleDeleteTask)
		r.Post("/tasks/{id}/restore", s.handleRestoreTask)
		r.Post("/tasks/{id}/reorder", s.handleReorderTask)
		r.Delete("/tasks/{id}/purge", s.handlePurgeTask)

		// Time tracking + work log.
		r.Post("/timer/start", s.handleTimerStart)
		r.Post("/timer/stop", s.handleTimerStop)
		r.Get("/timer/active", s.handleTimerActive)
		r.Get("/timer/entries", s.handleTimerList)
		r.Get("/worklog/today", s.handleWorklogToday)
		r.Get("/analytics/productivity", s.handleProductivityTrend)

		// Reminders.
		r.Post("/reminders", s.handleCreateReminder)
		r.Get("/reminders", s.handleListReminders)
		r.Patch("/reminders/{id}", s.handleUpdateReminder)
		r.Post("/reminders/{id}/ack", s.handleAcknowledgeReminder)
		r.Post("/reminders/{id}/snooze", s.handleSnoozeReminder)
		r.Delete("/reminders/{id}", s.handleDeleteReminder)
		r.Get("/notifications", s.handleListNotificationEndpoints)
		r.Post("/notifications", s.handleCreateNotificationEndpoint)
		r.Patch("/notifications/{id}", s.handleSetNotificationEndpoint)
		r.Delete("/notifications/{id}", s.handleDeleteNotificationEndpoint)

		// Integrations.
		r.Get("/integrations", s.handleListIntegrations)
		r.Post("/integrations", s.handleCreateIntegration)
		r.Delete("/integrations/{id}", s.handleDeleteIntegration)
		r.Post("/integrations/{id}/sync", s.handleSyncIntegration)

		r.Post("/api-keys", s.handleIssueAPIKey)
		r.Get("/api-keys", s.handleListAPIKeys)
		r.Patch("/api-keys/{id}", s.handleRenameAPIKey)
		r.Post("/api-keys/{id}/rotate", s.handleRotateAPIKey)
		r.Delete("/api-keys/{id}", s.handleRevokeAPIKey)
		r.Get("/invites", s.handleListInvites)
		r.Post("/invites", s.handleCreateInvite)
		r.Get("/members", s.handleListMembers)
		r.Patch("/members/{id}", s.handleSetMemberRole)
		r.Delete("/members/{id}", s.handleRemoveMember)
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
		if r.Header.Get("X-API-Key") == "" && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				writeError(w, http.StatusForbidden, "origin_rejected", "cross-origin cookie mutation rejected")
				return
			}
		}
		if scope := requiredScope(r); scope != "" && !tc.HasScope(scope) {
			writeError(w, http.StatusForbidden, "insufficient_scope", "credential lacks "+scope)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requiredScope(r *http.Request) string {
	p := r.URL.Path
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case strings.Contains(p, "/tasks"):
		if r.Method == http.MethodDelete && strings.HasSuffix(p, "/purge") {
			return "tasks:delete"
		}
		if read {
			return "tasks:read"
		}
		return "tasks:write"
	case strings.Contains(p, "/timer"), strings.Contains(p, "/worklog"), strings.Contains(p, "/analytics"), strings.Contains(p, "/reminders"), strings.Contains(p, "/notifications"):
		if read {
			return "productivity:read"
		}
		return "productivity:write"
	case strings.Contains(p, "/integrations"):
		return "integrations:manage"
	case strings.Contains(p, "/projects"), strings.Contains(p, "/tags"):
		if read {
			return "workspace:read"
		}
		return "workspace:write"
	case strings.Contains(p, "/api-keys"), strings.Contains(p, "/invites"), strings.Contains(p, "/members"):
		return "admin"
	default:
		return ""
	}
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
	TenantName  string `json:"tenant_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	InviteToken string `json:"invite_token"`
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuthAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
		return
	}
	var req signupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	hasUsers, err := auth.HasUsers(r.Context(), s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var u *model.User
	allowOpenSignup := strings.EqualFold(strings.TrimSpace(os.Getenv("TTL_ALLOW_OPEN_SIGNUP")), "true")
	if hasUsers && req.InviteToken != "" {
		u, err = auth.JoinWithInvite(r.Context(), s.DB, req.InviteToken, req.Email, req.Password)
	} else if hasUsers && !allowOpenSignup {
		if req.InviteToken == "" {
			writeError(w, http.StatusForbidden, "invite_required", "signup requires an invite")
			return
		}
	} else {
		if hasUsers {
			u, err = auth.Signup(r.Context(), s.DB, req.TenantName, req.Email, req.Password)
		} else {
			u, err = auth.SignupBootstrap(r.Context(), s.DB, req.TenantName, req.Email, req.Password)
		}
	}
	if err != nil {
		if hasUsers && req.InviteToken != "" {
			writeError(w, http.StatusBadRequest, "invalid_invite", err.Error())
			return
		}
		switch {
		case errors.Is(err, auth.ErrEmailTaken):
			writeError(w, http.StatusConflict, "email_taken", err.Error())
		case errors.Is(err, auth.ErrBootstrapDone):
			writeError(w, http.StatusConflict, "bootstrap_complete", err.Error())
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
		Expires: exp, HttpOnly: true, Secure: secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuthAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
		return
	}
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
		Expires: exp, HttpOnly: true, Secure: secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) allowAuthAttempt(r *http.Request) bool {
	now, cutoff := time.Now(), time.Now().Add(-time.Minute)
	key := r.RemoteAddr
	if host, _, err := net.SplitHostPort(key); err == nil {
		key = host
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TTL_TRUST_PROXY")), "true") {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
			key = forwarded
		}
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	items := s.authAttempts[key][:0]
	for _, at := range s.authAttempts[key] {
		if at.After(cutoff) {
			items = append(items, at)
		}
	}
	if len(items) >= 10 {
		s.authAttempts[key] = items
		return false
	}
	s.authAttempts[key] = append(items, now)
	return true
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("ttl_session"); err == nil {
		_ = auth.DestroySession(r.Context(), s.DB, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "ttl_session", Value: "", Path: "/",
		Expires: time.Unix(0, 0), MaxAge: -1, HttpOnly: true, Secure: secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func secureCookies() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TTL_SECURE_COOKIES")))
	return v == "1" || v == "true" || v == "yes"
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

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req projectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.Store.UpdateProject(r.Context(), tc, chi.URLParam(r, "id"), req.Name, req.Color); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleArchiveProject(w http.ResponseWriter, r *http.Request) {
	s.setProjectArchived(w, r, true)
}
func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	s.setProjectArchived(w, r, false)
}
func (s *Server) setProjectArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	if err := s.Store.ArchiveProject(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id"), archived); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handlePurgeProject(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.PurgeProject(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	var req tagReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.Store.UpdateTag(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id"), req.Name, req.Color); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteTag(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleMergeTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetID string `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.Store.MergeTag(r.Context(), tenant.MustFrom(r.Context()), chi.URLParam(r, "id"), req.TargetID); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	} else {
		now := time.Now()
		v := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
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
	if req.RecurrenceRRule != nil {
		start := time.Now()
		if req.DueAt != nil {
			start = time.UnixMilli(*req.DueAt)
		}
		rule, err := recurrence.Normalize(*req.RecurrenceRRule, start)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", err.Error())
			return
		}
		if rule == "" {
			req.RecurrenceRRule = nil
		} else {
			req.RecurrenceRRule = &rule
		}
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
	now := time.Now()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endToday := startToday.AddDate(0, 0, 1).Add(-time.Millisecond)
	switch q.Get("view") {
	case "inbox":
		f.Status, f.Inbox, f.Order = "open", true, "manual"
	case "today":
		f.Status, f.DueTo = "open", &endToday
	case "upcoming":
		from, to := endToday.Add(time.Millisecond), endToday.AddDate(0, 0, 14)
		f.Status, f.DueFrom, f.DueTo = "open", &from, &to
	case "overdue":
		f.Status, f.Overdue = "open", true
	case "next":
		f.Status = "open"
	case "done":
		f.Status = "done"
	case "trash":
		f.Status, f.Deleted = "", true
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
	var t *model.Task
	var err error
	if r.URL.Query().Get("deleted") == "1" {
		t, err = s.Store.GetTaskIncludingDeleted(r.Context(), tc, id)
	} else {
		t, err = s.Store.GetTask(r.Context(), tc, id)
	}
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
	if v, ok := fields["recurrence_rrule"]; ok && v != nil {
		raw, ok := v.(string)
		if !ok {
			writeError(w, http.StatusBadRequest, "validation", "recurrence_rrule must be a string or null")
			return
		}
		rule, err := recurrence.Normalize(raw, time.Now())
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", err.Error())
			return
		}
		if rule == "" {
			fields["recurrence_rrule"] = nil
		} else {
			fields["recurrence_rrule"] = rule
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

func (s *Server) handleRestoreTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := s.Store.RestoreTask(r.Context(), tc, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "trashed task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	t, err := s.Store.GetTask(r.Context(), tc, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handlePurgeTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := s.Store.PurgeTask(r.Context(), tc, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "trashed task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderTask(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req struct {
		ProjectID *string `json:"project_id"`
		ParentID  *string `json:"parent_id"`
		BeforeID  string  `json:"before_id"`
		AfterID   string  `json:"after_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	t, err := s.Store.ReorderTask(r.Context(), tc, chi.URLParam(r, "id"), req.ProjectID, req.ParentID, req.BeforeID, req.AfterID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// -------------------------- API key handlers --------------------------

type apiKeyReq struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *int64   `json:"expires_at"`
}

func (s *Server) handleIssueAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req apiKeyReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	u := &model.User{ID: tc.UserID, TenantID: tc.TenantID}
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		v := time.UnixMilli(*req.ExpiresAt)
		if !v.After(time.Now()) {
			writeError(w, http.StatusBadRequest, "validation", "expires_at must be in the future")
			return
		}
		expiresAt = &v
	}
	allowed := map[string]bool{"tasks:read": true, "tasks:write": true, "tasks:delete": true,
		"productivity:read": true, "productivity:write": true, "workspace:read": true,
		"workspace:write": true, "integrations:manage": true, "admin": true}
	if req.Scopes == nil {
		req.Scopes = []string{"tasks:read", "tasks:write", "tasks:delete", "productivity:read", "productivity:write", "workspace:read"}
		if tc.Role == "owner" {
			req.Scopes = append(req.Scopes, "workspace:write", "integrations:manage", "admin")
		}
	}
	for _, scope := range req.Scopes {
		if !allowed[scope] {
			writeError(w, http.StatusBadRequest, "validation", "unknown scope "+scope)
			return
		}
		if tc.Role != "owner" && (scope == "admin" || scope == "integrations:manage" || scope == "workspace:write") {
			writeError(w, http.StatusForbidden, "forbidden", "role cannot grant "+scope)
			return
		}
	}
	plain, k, err := auth.IssueAPIKeyWithOptions(r.Context(), s.DB, u, req.Name, req.Scopes, expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     plain,
		"api_key": k,
	})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	keys, err := auth.ListAPIKeys(r.Context(), s.DB, tc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": keys})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	if err := auth.RevokeAPIKey(r.Context(), s.DB, tc, chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRenameAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := auth.RenameAPIKey(r.Context(), s.DB, tc, chi.URLParam(r, "id"), req.Name); err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := tenant.MustFrom(r.Context())
	raw, key, err := auth.RotateAPIKey(r.Context(), s.DB, tc, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": raw, "api_key": key})
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
