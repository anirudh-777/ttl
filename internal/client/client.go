// Package client is the HTTP client used by the CLI and TUI to talk
// to a ttl server. It transparently injects the X-API-Key header and
// persists session cookies via an in-memory jar so signup->apikey
// flows work without the caller having to manage cookies manually.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/anirudh-777/ttl/internal/model"
)

// Client is a tiny typed wrapper over net/http.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Jar     *cookiejar.Jar
}

// New returns a Client with sensible defaults.
func New(baseURL, apiKey string) *Client {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 15 * time.Second, Jar: jar},
		Jar:     jar,
	}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	// Split any query string out of path so net/url doesn't encode "?".
	if i := strings.Index(path, "?"); i >= 0 {
		u.Path = path[:i]
		u.RawQuery = path[i+1:]
	} else {
		u.Path = path
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Surface "can't reach server" more helpfully than the raw
		// "dial tcp ... connection refused".
		if isConnRefused(err) {
			return fmt.Errorf("can't reach ttl server at %s — is `ttl serve` running?", c.BaseURL)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// Try to surface a structured error message.
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error.Message != "" {
			return fmt.Errorf("%s: %s", e.Error.Code, e.Error.Message)
		}
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// -------------------------- Auth --------------------------

func (c *Client) Signup(ctx context.Context, tenantName, email, password string) (*model.User, error) {
	return c.SignupWithInvite(ctx, tenantName, email, password, "")
}

func (c *Client) SignupWithInvite(ctx context.Context, tenantName, email, password, inviteToken string) (*model.User, error) {
	var resp struct {
		User model.User `json:"user"`
	}
	err := c.do(ctx, "POST", "/api/v1/auth/signup",
		map[string]string{
			"tenant_name":  tenantName,
			"email":        email,
			"password":     password,
			"invite_token": inviteToken,
		}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.User, nil
}

func (c *Client) Login(ctx context.Context, email, password string) error {
	var resp struct {
		User model.User `json:"user"`
	}
	return c.do(ctx, "POST", "/api/v1/auth/login",
		map[string]string{"email": email, "password": password}, &resp)
}

// -------------------------- Projects --------------------------

func (c *Client) ListProjects(ctx context.Context) ([]model.Project, error) {
	var resp struct {
		Projects []model.Project `json:"projects"`
	}
	if err := c.do(ctx, "GET", "/api/v1/projects", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Projects, nil
}

func (c *Client) CreateProject(ctx context.Context, name, color string) (*model.Project, error) {
	var p model.Project
	err := c.do(ctx, "POST", "/api/v1/projects",
		map[string]string{"name": name, "color": color}, &p)
	return &p, err
}
func (c *Client) UpdateProject(ctx context.Context, id, name, color string) error {
	return c.do(ctx, "PATCH", "/api/v1/projects/"+id, map[string]string{"name": name, "color": color}, nil)
}
func (c *Client) ArchiveProject(ctx context.Context, id string) error {
	return c.do(ctx, "POST", "/api/v1/projects/"+id+"/archive", nil, nil)
}
func (c *Client) RestoreProject(ctx context.Context, id string) error {
	return c.do(ctx, "POST", "/api/v1/projects/"+id+"/restore", nil, nil)
}
func (c *Client) PurgeProject(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/projects/"+id+"/purge", nil, nil)
}

// -------------------------- Tags --------------------------

func (c *Client) ListTags(ctx context.Context) ([]model.Tag, error) {
	var resp struct {
		Tags []model.Tag `json:"tags"`
	}
	if err := c.do(ctx, "GET", "/api/v1/tags", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tags, nil
}

func (c *Client) CreateTag(ctx context.Context, name, color string) (*model.Tag, error) {
	var t model.Tag
	err := c.do(ctx, "POST", "/api/v1/tags",
		map[string]string{"name": name, "color": color}, &t)
	return &t, err
}
func (c *Client) UpdateTag(ctx context.Context, id, name, color string) error {
	return c.do(ctx, "PATCH", "/api/v1/tags/"+id, map[string]string{"name": name, "color": color}, nil)
}
func (c *Client) MergeTag(ctx context.Context, sourceID, targetID string) error {
	return c.do(ctx, "POST", "/api/v1/tags/"+sourceID+"/merge", map[string]string{"target_id": targetID}, nil)
}
func (c *Client) DeleteTag(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/tags/"+id, nil, nil)
}

// -------------------------- Tasks --------------------------

type ListOpts struct {
	Status    string
	ProjectID string
	TagID     string
	Search    string
	Overdue   bool
	ParentID  string // "" = any, "root" = top-level
	Limit     int
	View      string
	Deleted   bool
}

func (c *Client) ListTasks(ctx context.Context, o ListOpts) ([]model.Task, error) {
	q := url.Values{}
	if o.Status != "" {
		q.Set("status", o.Status)
	}
	if o.ProjectID != "" {
		q.Set("project_id", o.ProjectID)
	}
	if o.TagID != "" {
		q.Set("tag_id", o.TagID)
	}
	if o.Search != "" {
		q.Set("q", o.Search)
	}
	if o.Overdue {
		q.Set("overdue", "1")
	}
	if o.ParentID != "" {
		q.Set("parent_id", o.ParentID)
	}
	if o.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", o.Limit))
	}
	if o.View != "" {
		q.Set("view", o.View)
	}
	if o.Deleted {
		q.Set("deleted", "1")
	}
	var resp struct {
		Tasks []model.Task `json:"tasks"`
	}
	if err := c.do(ctx, "GET", "/api/v1/tasks?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

type CreateTaskOpts struct {
	Title           string
	Notes           string
	Priority        int
	ProjectID       string
	ParentID        string
	DueAt           *time.Time
	Tags            []string
	RecurrenceRRule string
}

func (c *Client) CreateTask(ctx context.Context, o CreateTaskOpts) (*model.Task, error) {
	body := map[string]any{
		"title":    o.Title,
		"notes":    o.Notes,
		"priority": o.Priority,
	}
	if o.ProjectID != "" {
		body["project_id"] = o.ProjectID
	}
	if o.ParentID != "" {
		body["parent_id"] = o.ParentID
	}
	if o.DueAt != nil {
		body["due_at"] = o.DueAt.UnixMilli()
	}
	if len(o.Tags) > 0 {
		body["tags"] = o.Tags
	}
	if o.RecurrenceRRule != "" {
		body["recurrence_rrule"] = o.RecurrenceRRule
	}
	var t model.Task
	if err := c.do(ctx, "POST", "/api/v1/tasks", body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) GetTask(ctx context.Context, id string) (*model.Task, error) {
	var t model.Task
	if err := c.do(ctx, "GET", "/api/v1/tasks/"+id, nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) CompleteTask(ctx context.Context, id string) (*model.Task, error) {
	completed, _, err := c.CompleteTaskWithRecur(ctx, id)
	return completed, err
}

// CompleteTaskWithRecur returns the completed task plus an optional
// next occurrence if the task had a recurrence_rrule.
func (c *Client) CompleteTaskWithRecur(ctx context.Context, id string) (*model.Task, *model.Task, error) {
	var resp struct {
		Task *model.Task `json:"task"`
		Next *model.Task `json:"next_occurred"`
	}
	if err := c.do(ctx, "POST", "/api/v1/tasks/"+id+"/complete", nil, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Task, resp.Next, nil
}

// -------------------------- Integrations --------------------------

// SyncStats mirrors integrations.SyncStats in the public API.
type SyncStats struct {
	Created     int    `json:"created"`
	Updated     int    `json:"updated"`
	Closed      int    `json:"closed"`
	Unchanged   int    `json:"unchanged"`
	Integration string `json:"integration"`
}

func (c *Client) ListIntegrations(ctx context.Context) ([]model.Integration, error) {
	var resp struct {
		Integrations []model.Integration `json:"integrations"`
	}
	if err := c.do(ctx, "GET", "/api/v1/integrations", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Integrations, nil
}

func (c *Client) CreateIntegration(ctx context.Context, provider, label string, config map[string]string) (*model.Integration, error) {
	var it model.Integration
	body := map[string]any{
		"provider": provider,
		"label":    label,
		"config":   config,
	}
	if err := c.do(ctx, "POST", "/api/v1/integrations", body, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

func (c *Client) DeleteIntegration(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/integrations/"+id, nil, nil)
}

func (c *Client) SyncIntegration(ctx context.Context, id string) (*SyncStats, error) {
	var stats SyncStats
	if err := c.do(ctx, "POST", "/api/v1/integrations/"+id+"/sync", nil, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (c *Client) DeleteTask(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/tasks/"+id, nil, nil)
}

func (c *Client) RestoreTask(ctx context.Context, id string) (*model.Task, error) {
	var t model.Task
	if err := c.do(ctx, "POST", "/api/v1/tasks/"+id+"/restore", nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) PurgeTask(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/tasks/"+id+"/purge", nil, nil)
}

func (c *Client) ReorderTask(ctx context.Context, id string, projectID, parentID *string, beforeID, afterID string) (*model.Task, error) {
	var t model.Task
	body := map[string]any{"project_id": projectID, "parent_id": parentID, "before_id": beforeID, "after_id": afterID}
	if err := c.do(ctx, "POST", "/api/v1/tasks/"+id+"/reorder", body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) UpdateTask(ctx context.Context, id string, fields map[string]any) (*model.Task, error) {
	var t model.Task
	if err := c.do(ctx, "PATCH", "/api/v1/tasks/"+id, fields, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) IssueAPIKey(ctx context.Context, name string) (string, error) {
	key, _, err := c.CreateAPIKey(ctx, name, nil, nil)
	return key, err
}

func (c *Client) CreateAPIKey(ctx context.Context, name string, scopes []string, expiresAt *time.Time) (string, *model.APIKey, error) {
	var resp struct {
		Key    string        `json:"key"`
		APIKey *model.APIKey `json:"api_key"`
	}
	body := map[string]any{"name": name, "scopes": scopes}
	if expiresAt != nil {
		body["expires_at"] = expiresAt.UnixMilli()
	}
	if err := c.do(ctx, "POST", "/api/v1/api-keys", body, &resp); err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(resp.Key), resp.APIKey, nil
}

func (c *Client) ListAPIKeys(ctx context.Context) ([]model.APIKey, error) {
	var resp struct {
		APIKeys []model.APIKey `json:"api_keys"`
	}
	if err := c.do(ctx, "GET", "/api/v1/api-keys", nil, &resp); err != nil {
		return nil, err
	}
	return resp.APIKeys, nil
}
func (c *Client) RevokeAPIKey(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/api-keys/"+id, nil, nil)
}

func (c *Client) RenameAPIKey(ctx context.Context, id, name string) error {
	return c.do(ctx, "PATCH", "/api/v1/api-keys/"+id, map[string]string{"name": name}, nil)
}

func (c *Client) RotateAPIKey(ctx context.Context, id string) (string, *model.APIKey, error) {
	var resp struct {
		Key    string        `json:"key"`
		APIKey *model.APIKey `json:"api_key"`
	}
	if err := c.do(ctx, "POST", "/api/v1/api-keys/"+id+"/rotate", nil, &resp); err != nil {
		return "", nil, err
	}
	return resp.Key, resp.APIKey, nil
}
func (c *Client) CreateInvite(ctx context.Context, role string, expiresAt *time.Time) (string, *model.Invite, error) {
	var resp struct {
		Token  string        `json:"token"`
		Invite *model.Invite `json:"invite"`
	}
	body := map[string]any{"role": role}
	if expiresAt != nil {
		body["expires_at"] = expiresAt.UnixMilli()
	}
	if err := c.do(ctx, "POST", "/api/v1/invites", body, &resp); err != nil {
		return "", nil, err
	}
	return resp.Token, resp.Invite, nil
}
func (c *Client) ListMembers(ctx context.Context) ([]model.User, error) {
	var resp struct {
		Members []model.User `json:"members"`
	}
	if err := c.do(ctx, "GET", "/api/v1/members", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Members, nil
}
func (c *Client) SetMemberRole(ctx context.Context, id, role string) error {
	return c.do(ctx, "PATCH", "/api/v1/members/"+id, map[string]string{"role": role}, nil)
}
func (c *Client) RemoveMember(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/members/"+id, nil, nil)
}

// -------------------------- Time tracking --------------------------

func (c *Client) StartTimer(ctx context.Context, taskID, kind, note string) (*model.TimeEntry, error) {
	return c.StartTimerPlanned(ctx, taskID, kind, note, 0)
}

func (c *Client) StartTimerPlanned(ctx context.Context, taskID, kind, note string, minutes int) (*model.TimeEntry, error) {
	var e model.TimeEntry
	body := map[string]any{}
	if taskID != "" {
		body["task_id"] = taskID
	}
	if kind != "" {
		body["kind"] = kind
	}
	if note != "" {
		body["note"] = note
	}
	if minutes > 0 {
		body["minutes"] = minutes
	}
	if err := c.do(ctx, "POST", "/api/v1/timer/start", body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (c *Client) StopTimer(ctx context.Context, note string) (*model.TimeEntry, error) {
	var e model.TimeEntry
	body := map[string]any{}
	if note != "" {
		body["note"] = note
	}
	if err := c.do(ctx, "POST", "/api/v1/timer/stop", body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (c *Client) ActiveTimer(ctx context.Context) (*model.TimeEntry, error) {
	var resp struct {
		Entry *model.TimeEntry `json:"entry"`
	}
	if err := c.do(ctx, "GET", "/api/v1/timer/active", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Entry, nil
}

func (c *Client) TimerEntries(ctx context.Context, from, to time.Time) ([]model.TimeEntry, error) {
	q := url.Values{}
	if !from.IsZero() {
		q.Set("from", from.UTC().Format(time.RFC3339))
	}
	if !to.IsZero() {
		q.Set("to", to.UTC().Format(time.RFC3339))
	}
	var resp struct {
		Entries []model.TimeEntry `json:"entries"`
	}
	if err := c.do(ctx, "GET", "/api/v1/timer/entries?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

type DailySummary struct {
	Day     time.Time        `json:"day"`
	TotalMs int64            `json:"total_ms"`
	PerTask []DailyTaskTotal `json:"per_task"`
}

type DailyTaskTotal struct {
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title"`
	TotalMs   int64  `json:"total_ms"`
	Count     int    `json:"count"`
}

func (c *Client) WorklogToday(ctx context.Context, tz string) (*DailySummary, *model.TimeEntry, error) {
	q := url.Values{}
	if tz != "" {
		q.Set("tz", tz)
	}
	var resp struct {
		Summary *DailySummary    `json:"summary"`
		Active  *model.TimeEntry `json:"active"`
	}
	if err := c.do(ctx, "GET", "/api/v1/worklog/today?"+q.Encode(), nil, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Summary, resp.Active, nil
}

func (c *Client) CreateReminder(ctx context.Context, taskID string, fireAt time.Time) (*model.Reminder, error) {
	return c.CreateReminderWithEndpoint(ctx, taskID, fireAt, "")
}

func (c *Client) CreateReminderWithEndpoint(ctx context.Context, taskID string, fireAt time.Time, endpointID string) (*model.Reminder, error) {
	var r model.Reminder
	body := map[string]any{"task_id": taskID, "fire_at": fireAt.UnixMilli()}
	if endpointID != "" {
		body["endpoint_id"] = endpointID
	}
	if err := c.do(ctx, "POST", "/api/v1/reminders", body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) ListReminders(ctx context.Context, status string) ([]model.Reminder, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	var resp struct {
		Reminders []model.Reminder `json:"reminders"`
	}
	if err := c.do(ctx, "GET", "/api/v1/reminders?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Reminders, nil
}

func (c *Client) UpdateReminder(ctx context.Context, id string, fireAt time.Time) (*model.Reminder, error) {
	var r model.Reminder
	if err := c.do(ctx, "PATCH", "/api/v1/reminders/"+id, map[string]any{"fire_at": fireAt.UnixMilli()}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) AcknowledgeReminder(ctx context.Context, id string) (*model.Reminder, error) {
	var r model.Reminder
	if err := c.do(ctx, "POST", "/api/v1/reminders/"+id+"/ack", nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) SnoozeReminder(ctx context.Context, id string, fireAt time.Time) (*model.Reminder, error) {
	var r model.Reminder
	if err := c.do(ctx, "POST", "/api/v1/reminders/"+id+"/snooze", map[string]any{"fire_at": fireAt.UnixMilli()}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) DeleteReminder(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/reminders/"+id, nil, nil)
}

func (c *Client) CreateNotificationEndpoint(ctx context.Context, name, rawURL string) (string, *model.NotificationEndpoint, error) {
	var resp struct {
		Secret   string                      `json:"secret"`
		Endpoint *model.NotificationEndpoint `json:"endpoint"`
	}
	if err := c.do(ctx, "POST", "/api/v1/notifications", map[string]string{"name": name, "url": rawURL}, &resp); err != nil {
		return "", nil, err
	}
	return resp.Secret, resp.Endpoint, nil
}
func (c *Client) ListNotificationEndpoints(ctx context.Context) ([]model.NotificationEndpoint, error) {
	var resp struct {
		Endpoints []model.NotificationEndpoint `json:"endpoints"`
	}
	if err := c.do(ctx, "GET", "/api/v1/notifications", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Endpoints, nil
}
func (c *Client) SetNotificationEndpointEnabled(ctx context.Context, id string, enabled bool) error {
	return c.do(ctx, "PATCH", "/api/v1/notifications/"+id, map[string]bool{"enabled": enabled}, nil)
}
func (c *Client) DeleteNotificationEndpoint(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/notifications/"+id, nil, nil)
}

// isConnRefused returns true when err looks like "nothing is listening
// on that port" — unwraps the stdlib net error types where possible.
func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connectex: No connection could be made") ||
		strings.Contains(s, "No route to host")
}
