// Package mcp implements a minimal Model Context Protocol server
// over stdio. The protocol is JSON-RPC 2.0; we speak just enough of
// it to advertise tools and dispatch tool calls.
//
// We deliberately do NOT pull in the official SDK — MCP's surface for
// "tools" is small enough to implement directly. The server reads
// newline-delimited JSON-RPC requests from stdin and writes responses
// to stdout. Logging goes to stderr.
//
// Tools exposed to AI agents:
//
//	add_task(title, notes?, priority?, due_at?, tags?, project?)
//	list_tasks(status?, project?, search?, overdue?, limit?)
//	show_task(id)
//	complete_task(id)
//	delete_task(id)
//	start_timer(task_id?, note?)
//	stop_timer(note?)
//	active_timer()
//	worklog_today()
//	search_tasks(query)
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/config"
	"github.com/anirudh-777/ttl/internal/recurrence"
)

var Version = "dev"

// Run starts the MCP server on os.Stdin/Stdout. Blocks until EOF.
func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ServerURL == "" || cfg.APIKey == "" {
		return fmt.Errorf("not logged in: run `ttl login` first")
	}
	c := client.New(cfg.ServerURL, cfg.APIKey)

	s := &server{client: c, out: os.Stdout, log: os.Stderr}
	return s.serve(ctx, os.Stdin)
}

type server struct {
	client *client.Client
	out    io.Writer
	log    io.Writer
}

// json-rpc 2.0 envelope.
type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *server) serve(ctx context.Context, in io.Reader) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeResp(rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}})
			continue
		}
		s.dispatch(ctx, req)
	}
	return scanner.Err()
}

func (s *server) dispatch(ctx context.Context, req rpcReq) {
	switch req.Method {
	case "initialize":
		s.writeResp(rpcResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]string{"name": "ttl", "version": Version},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			},
		})
	case "tools/list":
		s.writeResp(rpcResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": allTools()},
		})
	case "tools/call":
		s.callTool(ctx, req)
	case "notifications/initialized":
		// no-op
	case "ping":
		s.writeResp(rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]string{"status": "ok"}})
	default:
		s.writeResp(rpcResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcErr{Code: -32601, Message: "method not found: " + req.Method},
		})
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *server) callTool(ctx context.Context, req rpcReq) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.writeResp(rpcResp{JSONRPC: "2.0", ID: req.ID, Error: &rpcErr{Code: -32602, Message: "invalid params"}})
		return
	}
	args := map[string]any{}
	if len(p.Arguments) > 0 {
		_ = json.Unmarshal(p.Arguments, &args)
	}
	result, err := s.invoke(ctx, p.Name, args)
	if err != nil {
		s.writeResp(rpcResp{JSONRPC: "2.0", ID: req.ID, Error: &rpcErr{Code: -32000, Message: err.Error()}})
		return
	}
	s.writeResp(rpcResp{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(result)}},
		},
	})
}

func (s *server) writeResp(r rpcResp) {
	b, _ := json.Marshal(r)
	b = append(b, '\n')
	_, _ = s.out.Write(b)
}

// -------------------------- tools --------------------------

func allTools() []map[string]any {
	propStr := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	propInt := func(desc string, min, max int) map[string]any {
		return map[string]any{"type": "integer", "description": desc, "minimum": min, "maximum": max}
	}
	propBool := func(desc string) map[string]any {
		return map[string]any{"type": "boolean", "description": desc}
	}
	propStrings := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
	}
	return []map[string]any{
		{
			"name":        "add_task",
			"description": "Create a new task in ttl.",
			"inputSchema": obj(map[string]any{
				"title":      propStr("Task title."),
				"notes":      propStr("Optional notes (markdown)."),
				"priority":   propInt("0..3 (0 none, 1 low, 2 med, 3 high).", 0, 3),
				"due_at":     propStr("Due date as ISO-8601, YYYY-MM-DD, today, or tomorrow."),
				"tags":       propStrings("Tag names."),
				"project":    propStr("Project name (created on demand)."),
				"recurrence": propStr("daily, weekdays, weekly, monthly, yearly, or rrule:<rule>."),
			}, []string{"title"}),
		},
		{
			"name":        "update_task",
			"description": "Update an existing task. Omitted fields remain unchanged.",
			"inputSchema": obj(map[string]any{
				"id": propStr("Task id."), "title": propStr("New title."),
				"notes": propStr("New notes."), "priority": propInt("New priority.", 0, 3),
				"due_at": propStr("New due date, or none."), "project": propStr("New project, or empty to clear."),
				"tags": propStrings("Replacement tag names."), "recurrence": propStr("Preset, rrule:<rule>, or none."),
			}, []string{"id"}),
		},
		{
			"name": "add_subtask", "description": "Create a subtask under an existing task.",
			"inputSchema": obj(map[string]any{"parent_id": propStr("Parent task id."), "title": propStr("Subtask title."), "notes": propStr("Optional notes."), "priority": propInt("Priority.", 0, 3)}, []string{"parent_id", "title"}),
		},
		{"name": "list_subtasks", "description": "List direct subtasks of a task.", "inputSchema": obj(map[string]any{"parent_id": propStr("Parent task id.")}, []string{"parent_id"})},
		{
			"name":        "list_tasks",
			"description": "List tasks. Default: open. Use status='all' to include done.",
			"inputSchema": obj(map[string]any{
				"status":  propStr("open | done | all"),
				"project": propStr("Project name filter."),
				"search":  propStr("Substring search across title and notes."),
				"view":    propStr("inbox | today | upcoming | overdue | next | done | trash"),
				"overdue": propBool("Only return overdue open tasks."),
				"limit":   propInt("Max results (default 50).", 1, 1000),
			}, nil),
		},
		{
			"name":        "show_task",
			"description": "Show a task by id or short prefix.",
			"inputSchema": obj(map[string]any{
				"id": propStr("Task id (full UUID or 8-char prefix)."),
			}, []string{"id"}),
		},
		{
			"name":        "complete_task",
			"description": "Mark a task as done. If the task has a recurrence_rrule, the next occurrence is auto-created.",
			"inputSchema": obj(map[string]any{
				"id": propStr("Task id or short prefix."),
			}, []string{"id"}),
		},
		{
			"name":        "delete_task",
			"description": "Move a task to recoverable trash.",
			"inputSchema": obj(map[string]any{
				"id": propStr("Task id or short prefix."),
			}, []string{"id"}),
		},
		{"name": "restore_task", "description": "Restore a task from trash.", "inputSchema": obj(map[string]any{"id": propStr("Task id.")}, []string{"id"})},
		{"name": "purge_task", "description": "Permanently delete a trashed task. This cannot be undone.", "inputSchema": obj(map[string]any{"id": propStr("Task id.")}, []string{"id"})},
		{"name": "reorder_task", "description": "Move or reorder a task relative to another task.", "inputSchema": obj(map[string]any{"id": propStr("Task id."), "project_id": propStr("Destination project id; omit or empty for Inbox."), "parent_id": propStr("Destination parent id; omit or empty for top level."), "before_id": propStr("Place before this task."), "after_id": propStr("Place after this task.")}, []string{"id"})},
		{"name": "reminder_add", "description": "Schedule a task reminder.", "inputSchema": obj(map[string]any{"task_id": propStr("Task id."), "at": propStr("RFC3339 time or +duration such as +30m."), "endpoint_id": propStr("Optional notification endpoint id.")}, []string{"task_id", "at"})},
		{"name": "reminders_list", "description": "List reminders.", "inputSchema": obj(map[string]any{"status": propStr("pending, sent, or ack.")}, nil)},
		{"name": "reminder_ack", "description": "Acknowledge a reminder.", "inputSchema": obj(map[string]any{"id": propStr("Reminder id.")}, []string{"id"})},
		{"name": "reminder_snooze", "description": "Snooze a reminder to a new time.", "inputSchema": obj(map[string]any{"id": propStr("Reminder id."), "at": propStr("RFC3339 time or +duration.")}, []string{"id", "at"})},
		{"name": "reminder_delete", "description": "Delete a reminder.", "inputSchema": obj(map[string]any{"id": propStr("Reminder id.")}, []string{"id"})},
		{"name": "projects_list", "description": "List projects.", "inputSchema": obj(map[string]any{}, nil)},
		{"name": "project_create", "description": "Create a project.", "inputSchema": obj(map[string]any{"name": propStr("Project name."), "color": propStr("Hex color.")}, []string{"name"})},
		{"name": "project_update", "description": "Rename or recolor a project.", "inputSchema": obj(map[string]any{"id": propStr("Project id."), "name": propStr("Project name."), "color": propStr("Hex color.")}, []string{"id", "name"})},
		{"name": "project_archive", "description": "Archive a project.", "inputSchema": obj(map[string]any{"id": propStr("Project id.")}, []string{"id"})},
		{"name": "project_restore", "description": "Restore an archived project.", "inputSchema": obj(map[string]any{"id": propStr("Project id.")}, []string{"id"})},
		{"name": "project_purge", "description": "Permanently remove an archived project; tasks move to Inbox.", "inputSchema": obj(map[string]any{"id": propStr("Project id.")}, []string{"id"})},
		{"name": "tags_list", "description": "List tags.", "inputSchema": obj(map[string]any{}, nil)},
		{"name": "tag_create", "description": "Create a tag.", "inputSchema": obj(map[string]any{"name": propStr("Tag name."), "color": propStr("Hex color.")}, []string{"name"})},
		{"name": "tag_update", "description": "Rename or recolor a tag.", "inputSchema": obj(map[string]any{"id": propStr("Tag id."), "name": propStr("Tag name."), "color": propStr("Hex color.")}, []string{"id", "name"})},
		{"name": "tag_merge", "description": "Merge one tag into another.", "inputSchema": obj(map[string]any{"source_id": propStr("Source tag id."), "target_id": propStr("Target tag id.")}, []string{"source_id", "target_id"})},
		{"name": "tag_delete", "description": "Delete a tag without deleting tasks.", "inputSchema": obj(map[string]any{"id": propStr("Tag id.")}, []string{"id"})},
		{"name": "keys_list", "description": "List the current user's API keys.", "inputSchema": obj(map[string]any{}, nil)},
		{"name": "key_create", "description": "Create a scoped API key. The secret is returned once.", "inputSchema": obj(map[string]any{"name": propStr("Key name."), "scopes": propStrings("Allowed scopes."), "expires_at": propStr("Optional RFC3339 expiry.")}, []string{"name", "scopes"})},
		{"name": "key_rename", "description": "Rename an API key.", "inputSchema": obj(map[string]any{"id": propStr("Key id."), "name": propStr("New name.")}, []string{"id", "name"})},
		{"name": "key_rotate", "description": "Replace an API key and return its new secret once.", "inputSchema": obj(map[string]any{"id": propStr("Key id.")}, []string{"id"})},
		{"name": "key_revoke", "description": "Immediately revoke an API key.", "inputSchema": obj(map[string]any{"id": propStr("Key id.")}, []string{"id"})},
		{"name": "members_list", "description": "List workspace members.", "inputSchema": obj(map[string]any{}, nil)},
		{"name": "member_invite", "description": "Create a single-use seven-day invite.", "inputSchema": obj(map[string]any{"role": propStr("member or admin.")}, nil)},
		{"name": "member_role", "description": "Change a member role.", "inputSchema": obj(map[string]any{"id": propStr("Member id."), "role": propStr("member, admin, or owner.")}, []string{"id", "role"})},
		{"name": "member_remove", "description": "Remove a workspace member and revoke their credentials.", "inputSchema": obj(map[string]any{"id": propStr("Member id.")}, []string{"id"})},
		{"name": "notifications_list", "description": "List reminder webhook endpoints.", "inputSchema": obj(map[string]any{}, nil)},
		{"name": "notification_create", "description": "Create an HMAC-signed reminder webhook endpoint.", "inputSchema": obj(map[string]any{"name": propStr("Endpoint name."), "url": propStr("HTTP(S) URL.")}, []string{"name", "url"})},
		{"name": "notification_set_enabled", "description": "Enable or disable a notification endpoint.", "inputSchema": obj(map[string]any{"id": propStr("Endpoint id."), "enabled": propBool("Whether delivery is enabled.")}, []string{"id", "enabled"})},
		{"name": "notification_delete", "description": "Delete a notification endpoint.", "inputSchema": obj(map[string]any{"id": propStr("Endpoint id.")}, []string{"id"})},
		{
			"name":        "start_timer",
			"description": "Start a timer (optionally on a task).",
			"inputSchema": obj(map[string]any{
				"task_id": propStr("Task id (optional)."),
				"note":    propStr("Optional note."),
				"kind":    propStr("work or pomodoro (default work)."),
				"minutes": propInt("Pomodoro duration in minutes.", 1, 1440),
			}, nil),
		},
		{
			"name":        "stop_timer",
			"description": "Stop the active timer.",
			"inputSchema": obj(map[string]any{
				"note": propStr("Optional note appended to the entry."),
			}, nil),
		},
		{
			"name":        "active_timer",
			"description": "Return the currently running timer (or null).",
			"inputSchema": obj(map[string]any{}, nil),
		},
		{
			"name":        "worklog_today",
			"description": "Return today's work log: total tracked time and breakdown by task.",
			"inputSchema": obj(map[string]any{}, nil),
		},
		{
			"name":        "search_tasks",
			"description": "Substring search across title and notes.",
			"inputSchema": obj(map[string]any{
				"query": propStr("Search term."),
			}, []string{"query"}),
		},
	}
}

func obj(props map[string]any, required []string) map[string]any {
	schemaProps := map[string]any{}
	for k, v := range props {
		schemaProps[k] = v
	}
	s := map[string]any{
		"type":       "object",
		"properties": schemaProps,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// -------------------------- invoke --------------------------

func (s *server) invoke(ctx context.Context, name string, args map[string]any) (string, error) {
	get := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	getInt := func(k string) int {
		f, _ := args[k].(float64)
		return int(f)
	}
	getBool := func(k string) bool {
		b, _ := args[k].(bool)
		return b
	}
	getStrings := func(k string) []string {
		var out []string
		switch v := args[k].(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
		case string: // backwards compatibility with the original MCP schema
			for _, item := range strings.Split(v, ",") {
				if item = strings.TrimSpace(item); item != "" {
					out = append(out, item)
				}
			}
		}
		return out
	}

	switch name {
	case "add_task":
		title := get("title")
		if title == "" {
			return "", fmt.Errorf("title is required")
		}
		notes := get("notes")
		priority := getInt("priority")
		var due *time.Time
		if s := get("due_at"); s != "" {
			t, err := parseFlexDate(s)
			if err != nil {
				return "", err
			}
			due = &t
		}
		tags := getStrings("tags")
		rrule, err := recurrence.Normalize(get("recurrence"), time.Now())
		if err != nil {
			return "", err
		}
		var projectID string
		if pn := get("project"); pn != "" {
			ps, err := s.client.ListProjects(ctx)
			if err != nil {
				return "", err
			}
			for _, p := range ps {
				if strings.EqualFold(p.Name, pn) {
					projectID = p.ID
					break
				}
			}
			if projectID == "" {
				p, err := s.client.CreateProject(ctx, pn, "")
				if err != nil {
					return "", err
				}
				projectID = p.ID
			}
		}
		t, err := s.client.CreateTask(ctx, client.CreateTaskOpts{
			Title: title, Notes: notes, Priority: priority,
			ProjectID: projectID, DueAt: due, Tags: tags, RecurrenceRRule: rrule,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("created task %q (id=%s)", t.Title, t.ID), nil

	case "update_task":
		id := get("id")
		if id == "" {
			return "", fmt.Errorf("id is required")
		}
		id, err := s.resolveTaskID(ctx, id, false)
		if err != nil {
			return "", err
		}
		fields := map[string]any{}
		for _, key := range []string{"title", "notes"} {
			if _, ok := args[key]; ok {
				fields[key] = get(key)
			}
		}
		if _, ok := args["priority"]; ok {
			fields["priority"] = getInt("priority")
		}
		if _, ok := args["tags"]; ok {
			fields["tags"] = getStrings("tags")
		}
		if _, ok := args["due_at"]; ok {
			if get("due_at") == "none" || get("due_at") == "" {
				fields["due_at"] = nil
			} else {
				due, err := parseFlexDate(get("due_at"))
				if err != nil {
					return "", err
				}
				fields["due_at"] = due.UnixMilli()
			}
		}
		if _, ok := args["recurrence"]; ok {
			rule, err := recurrence.Normalize(get("recurrence"), time.Now())
			if err != nil {
				return "", err
			}
			if rule == "" {
				fields["recurrence_rrule"] = nil
			} else {
				fields["recurrence_rrule"] = rule
			}
		}
		if _, ok := args["project"]; ok {
			name := get("project")
			if name == "" {
				fields["project_id"] = nil
			} else {
				var pid string
				projects, err := s.client.ListProjects(ctx)
				if err != nil {
					return "", err
				}
				for _, p := range projects {
					if strings.EqualFold(p.Name, name) {
						pid = p.ID
						break
					}
				}
				if pid == "" {
					p, err := s.client.CreateProject(ctx, name, "")
					if err != nil {
						return "", err
					}
					pid = p.ID
				}
				fields["project_id"] = pid
			}
		}
		t, err := s.client.UpdateTask(ctx, id, fields)
		if err != nil {
			return "", err
		}
		return jsonOrText(t), nil

	case "add_subtask":
		if get("parent_id") == "" || get("title") == "" {
			return "", fmt.Errorf("parent_id and title are required")
		}
		parentID, err := s.resolveTaskID(ctx, get("parent_id"), false)
		if err != nil {
			return "", err
		}
		t, err := s.client.CreateTask(ctx, client.CreateTaskOpts{ParentID: parentID, Title: get("title"), Notes: get("notes"), Priority: getInt("priority")})
		if err != nil {
			return "", err
		}
		return jsonOrText(t), nil

	case "list_subtasks":
		parentID, err := s.resolveTaskID(ctx, get("parent_id"), false)
		if err != nil {
			return "", err
		}
		t, err := s.client.GetTask(ctx, parentID)
		if err != nil {
			return "", err
		}
		return jsonOrText(t.Subtasks), nil

	case "list_tasks":
		opts := client.ListOpts{Limit: 50}
		switch strings.ToLower(get("status")) {
		case "open", "":
			opts.Status = "open"
		case "done":
			opts.Status = "done"
		case "all":
			opts.Status = ""
		}
		if pn := get("project"); pn != "" {
			ps, _ := s.client.ListProjects(ctx)
			for _, p := range ps {
				if strings.EqualFold(p.Name, pn) {
					opts.ProjectID = p.ID
					break
				}
			}
		}
		opts.Search = get("search")
		opts.Overdue = getBool("overdue")
		opts.View = get("view")
		if l := getInt("limit"); l > 0 {
			opts.Limit = l
		}
		tasks, err := s.client.ListTasks(ctx, opts)
		if err != nil {
			return "", err
		}
		return jsonOrText(tasks), nil

	case "show_task":
		id, err := s.resolveTaskID(ctx, get("id"), false)
		if err != nil {
			return "", err
		}
		t, err := s.client.GetTask(ctx, id)
		if err != nil {
			return "", err
		}
		return jsonOrText(t), nil

	case "complete_task":
		id, err := s.resolveTaskID(ctx, get("id"), false)
		if err != nil {
			return "", err
		}
		t, err := s.client.CompleteTask(ctx, id)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("completed task %q (id=%s)", t.Title, t.ID), nil

	case "delete_task":
		id, err := s.resolveTaskID(ctx, get("id"), false)
		if err != nil {
			return "", err
		}
		if err := s.client.DeleteTask(ctx, id); err != nil {
			return "", err
		}
		return "moved to trash", nil

	case "restore_task":
		id, err := s.resolveTaskID(ctx, get("id"), true)
		if err != nil {
			return "", err
		}
		t, err := s.client.RestoreTask(ctx, id)
		if err != nil {
			return "", err
		}
		return jsonOrText(t), nil

	case "purge_task":
		id, err := s.resolveTaskID(ctx, get("id"), true)
		if err != nil {
			return "", err
		}
		if err := s.client.PurgeTask(ctx, id); err != nil {
			return "", err
		}
		return "permanently deleted", nil

	case "reorder_task":
		id, err := s.resolveTaskID(ctx, get("id"), false)
		if err != nil {
			return "", err
		}
		ptr := func(v string) *string {
			if v == "" {
				return nil
			}
			return &v
		}
		t, err := s.client.ReorderTask(ctx, id, ptr(get("project_id")), ptr(get("parent_id")), get("before_id"), get("after_id"))
		if err != nil {
			return "", err
		}
		return jsonOrText(t), nil

	case "reminder_add":
		taskID, err := s.resolveTaskID(ctx, get("task_id"), false)
		if err != nil {
			return "", err
		}
		when, err := parseReminderMoment(get("at"))
		if err != nil {
			return "", err
		}
		r, err := s.client.CreateReminderWithEndpoint(ctx, taskID, when, get("endpoint_id"))
		if err != nil {
			return "", err
		}
		return jsonOrText(r), nil
	case "reminders_list":
		items, err := s.client.ListReminders(ctx, get("status"))
		if err != nil {
			return "", err
		}
		return jsonOrText(items), nil
	case "reminder_ack":
		r, err := s.client.AcknowledgeReminder(ctx, get("id"))
		if err != nil {
			return "", err
		}
		return jsonOrText(r), nil
	case "reminder_snooze":
		when, err := parseReminderMoment(get("at"))
		if err != nil {
			return "", err
		}
		r, err := s.client.SnoozeReminder(ctx, get("id"), when)
		if err != nil {
			return "", err
		}
		return jsonOrText(r), nil
	case "reminder_delete":
		if err := s.client.DeleteReminder(ctx, get("id")); err != nil {
			return "", err
		}
		return "deleted", nil

	case "projects_list":
		items, err := s.client.ListProjects(ctx)
		if err != nil {
			return "", err
		}
		return jsonOrText(items), nil
	case "project_create":
		item, err := s.client.CreateProject(ctx, get("name"), get("color"))
		if err != nil {
			return "", err
		}
		return jsonOrText(item), nil
	case "project_update":
		name, color := get("name"), get("color")
		if color == "" {
			items, err := s.client.ListProjects(ctx)
			if err != nil {
				return "", err
			}
			for _, item := range items {
				if item.ID == get("id") {
					color = item.Color
					break
				}
			}
		}
		if err := s.client.UpdateProject(ctx, get("id"), name, color); err != nil {
			return "", err
		}
		return "updated", nil
	case "project_archive":
		if err := s.client.ArchiveProject(ctx, get("id")); err != nil {
			return "", err
		}
		return "archived", nil
	case "project_restore":
		if err := s.client.RestoreProject(ctx, get("id")); err != nil {
			return "", err
		}
		return "restored", nil
	case "project_purge":
		if err := s.client.PurgeProject(ctx, get("id")); err != nil {
			return "", err
		}
		return "permanently removed", nil
	case "tags_list":
		items, err := s.client.ListTags(ctx)
		if err != nil {
			return "", err
		}
		return jsonOrText(items), nil
	case "tag_create":
		item, err := s.client.CreateTag(ctx, get("name"), get("color"))
		if err != nil {
			return "", err
		}
		return jsonOrText(item), nil
	case "tag_update":
		name, color := get("name"), get("color")
		if color == "" {
			items, err := s.client.ListTags(ctx)
			if err != nil {
				return "", err
			}
			for _, item := range items {
				if item.ID == get("id") {
					color = item.Color
					break
				}
			}
		}
		if err := s.client.UpdateTag(ctx, get("id"), name, color); err != nil {
			return "", err
		}
		return "updated", nil
	case "tag_merge":
		if err := s.client.MergeTag(ctx, get("source_id"), get("target_id")); err != nil {
			return "", err
		}
		return "merged", nil
	case "tag_delete":
		if err := s.client.DeleteTag(ctx, get("id")); err != nil {
			return "", err
		}
		return "deleted", nil
	case "keys_list":
		items, err := s.client.ListAPIKeys(ctx)
		if err != nil {
			return "", err
		}
		return jsonOrText(items), nil
	case "key_create":
		var expiry *time.Time
		if get("expires_at") != "" {
			v, err := time.Parse(time.RFC3339, get("expires_at"))
			if err != nil {
				return "", err
			}
			expiry = &v
		}
		raw, item, err := s.client.CreateAPIKey(ctx, get("name"), getStrings("scopes"), expiry)
		if err != nil {
			return "", err
		}
		return jsonOrText(map[string]any{"key": raw, "api_key": item}), nil
	case "key_revoke":
		if err := s.client.RevokeAPIKey(ctx, get("id")); err != nil {
			return "", err
		}
		return "revoked", nil
	case "key_rename":
		if err := s.client.RenameAPIKey(ctx, get("id"), get("name")); err != nil {
			return "", err
		}
		return "renamed", nil
	case "key_rotate":
		raw, item, err := s.client.RotateAPIKey(ctx, get("id"))
		if err != nil {
			return "", err
		}
		return jsonOrText(map[string]any{"key": raw, "api_key": item}), nil
	case "members_list":
		items, err := s.client.ListMembers(ctx)
		if err != nil {
			return "", err
		}
		return jsonOrText(items), nil
	case "member_invite":
		role := get("role")
		if role == "" {
			role = "member"
		}
		expires := time.Now().Add(7 * 24 * time.Hour)
		raw, item, err := s.client.CreateInvite(ctx, role, &expires)
		if err != nil {
			return "", err
		}
		return jsonOrText(map[string]any{"token": raw, "invite": item}), nil
	case "member_role":
		if err := s.client.SetMemberRole(ctx, get("id"), get("role")); err != nil {
			return "", err
		}
		return "updated", nil
	case "member_remove":
		if err := s.client.RemoveMember(ctx, get("id")); err != nil {
			return "", err
		}
		return "removed", nil
	case "notifications_list":
		items, err := s.client.ListNotificationEndpoints(ctx)
		if err != nil {
			return "", err
		}
		return jsonOrText(items), nil
	case "notification_create":
		raw, item, err := s.client.CreateNotificationEndpoint(ctx, get("name"), get("url"))
		if err != nil {
			return "", err
		}
		return jsonOrText(map[string]any{"secret": raw, "endpoint": item}), nil
	case "notification_set_enabled":
		if err := s.client.SetNotificationEndpointEnabled(ctx, get("id"), getBool("enabled")); err != nil {
			return "", err
		}
		return "updated", nil
	case "notification_delete":
		if err := s.client.DeleteNotificationEndpoint(ctx, get("id")); err != nil {
			return "", err
		}
		return "deleted", nil

	case "start_timer":
		e, err := s.client.StartTimerPlanned(ctx, get("task_id"), get("kind"), get("note"), getInt("minutes"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("timer started (entry=%s)", e.ID), nil

	case "stop_timer":
		e, err := s.client.StopTimer(ctx, get("note"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("timer stopped (%s)", e.Note), nil

	case "active_timer":
		e, err := s.client.ActiveTimer(ctx)
		if err != nil {
			return "", err
		}
		if e == nil {
			return "no active timer", nil
		}
		return jsonOrText(e), nil

	case "worklog_today":
		sum, active, err := s.client.WorklogToday(ctx, "")
		if err != nil {
			return "", err
		}
		out := map[string]any{"summary": sum, "active": active}
		return jsonOrText(out), nil

	case "search_tasks":
		tasks, err := s.client.ListTasks(ctx, client.ListOpts{Search: get("query"), Limit: 50})
		if err != nil {
			return "", err
		}
		return jsonOrText(tasks), nil

	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func jsonOrText(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func (s *server) resolveTaskID(ctx context.Context, ref string, trash bool) (string, error) {
	if len(ref) == 36 {
		return ref, nil
	}
	view := ""
	if trash {
		view = "trash"
	}
	tasks, err := s.client.ListTasks(ctx, client.ListOpts{View: view, Limit: 1000})
	if err != nil {
		return "", err
	}
	var matches []string
	for _, task := range tasks {
		if strings.HasPrefix(task.ID, ref) {
			matches = append(matches, task.ID)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no task with id prefix %q", ref)
	default:
		return "", fmt.Errorf("ambiguous task id prefix %q matches %d tasks", ref, len(matches))
	}
}

// parseFlexDate accepts ISO-8601, RFC3339, YYYY-MM-DD, or relative
// words ("today", "tomorrow").
func parseFlexDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	now := time.Now()
	switch strings.ToLower(s) {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location()), nil
	case "tomorrow":
		t := now.Add(24 * time.Hour)
		return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 0, 0, t.Location()), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", s)
}

func parseReminderMoment(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "+") {
		d, err := time.ParseDuration(strings.TrimPrefix(s, "+"))
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised reminder time %q", s)
}
