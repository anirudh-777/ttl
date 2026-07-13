// Package model defines the core domain types shared across packages.
package model

import "time"

// Tenant is a workspace that owns users and tasks.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User belongs to exactly one tenant.
type User struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"` // owner | admin | member
	CreatedAt time.Time `json:"created_at"`
}

// APIKey is a long-lived credential used by the CLI and MCP clients.
type APIKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	TenantID   string     `json:"tenant_id"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// Project groups tasks.
type Project struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Name       string     `json:"name"`
	Color      string     `json:"color"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Tag labels tasks. Multiple tags per task are allowed.
type Tag struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

// Task is the central unit of work. status: open | done.
// priority: 0 none, 1 low, 2 medium, 3 high.
type Task struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	ProjectID       *string    `json:"project_id,omitempty"`
	ParentID        *string    `json:"parent_id,omitempty"`
	Title           string     `json:"title"`
	Notes           string     `json:"notes"`
	Status          string     `json:"status"`
	Priority        int        `json:"priority"`
	DueAt           *time.Time `json:"due_at,omitempty"`
	RecurrenceRRule *string    `json:"recurrence_rrule,omitempty"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
	Position        int64      `json:"position"`

	// Hydrated (not stored on the row).
	Tags     []string `json:"tags,omitempty"`
	Subtasks []Task   `json:"subtasks,omitempty"`
}

// TimeEntry is a single work or pomodoro session on a task.
// status: open while ended_at is nil; otherwise "stopped".
type TimeEntry struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	UserID            string     `json:"user_id"`
	TaskID            *string    `json:"task_id,omitempty"`
	Kind              string     `json:"kind"` // work | pomodoro
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	DurationMs        int64      `json:"duration_ms"`
	Note              string     `json:"note"`
	PlannedDurationMs int64      `json:"planned_duration_ms,omitempty"`
	TargetEndAt       *time.Time `json:"target_end_at,omitempty"`

	// Hydrated.
	TaskTitle string `json:"task_title,omitempty"`
	Status    string `json:"status"`
}

// Reminder fires at fire_at and notifies the user. status: pending|sent|ack.
type Reminder struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	TaskID         string     `json:"task_id"`
	UserID         string     `json:"user_id"`
	FireAt         time.Time  `json:"fire_at"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	EndpointID     *string    `json:"endpoint_id,omitempty"`
	DeliveryStatus string     `json:"delivery_status"`
	DeliveryError  *string    `json:"delivery_error,omitempty"`

	// Hydrated for convenience.
	TaskTitle string `json:"task_title,omitempty"`
}

type Invite struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Role      string     `json:"role"`
	CreatedBy string     `json:"created_by"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type NotificationEndpoint struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// Integration is a tenant-scoped connection to one external provider.
type Integration struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	Provider     string     `json:"provider"` // github | linear
	Label        string     `json:"label"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`

	// Config holds provider-specific opaque configuration. The PAT or
	// API key lives here, encrypted at rest when TIER>=2. Callers
	// must NOT echo this field back over the API.
	Config map[string]string `json:"-"`
}

// IssueLink ties a task to an external issue. Used to mirror state
// changes in both directions.
type IssueLink struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	TaskID        string    `json:"task_id"`
	IntegrationID string    `json:"integration_id"`
	Provider      string    `json:"provider"`
	ExternalID    string    `json:"external_id"`
	ExternalURL   string    `json:"external_url"`
	ExternalState string    `json:"external_state"`
	LastSyncedAt  time.Time `json:"last_synced_at"`
}
