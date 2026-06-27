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

	"github.com/anirudhprakash/ttl/internal/client"
	"github.com/anirudhprakash/ttl/internal/config"
)

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
				"serverInfo":      map[string]string{"name": "ttl", "version": "0.1.0"},
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
	return []map[string]any{
		{
			"name":        "add_task",
			"description": "Create a new task in ttl.",
			"inputSchema": obj(map[string]any{
				"title":    propStr("Task title."),
				"notes":    propStr("Optional notes (markdown)."),
				"priority": propStr("0..3 (0 none, 1 low, 2 med, 3 high)."),
				"due_at":   propStr("Due date as ISO-8601, YYYY-MM-DD, today, or tomorrow."),
				"tags":     propStr("Comma-separated tag list."),
				"project":  propStr("Project name (created on demand)."),
			}, []string{"title"}),
		},
		{
			"name":        "list_tasks",
			"description": "List tasks. Default: open. Use status='all' to include done.",
			"inputSchema": obj(map[string]any{
				"status":  propStr("open | done | all"),
				"project": propStr("Project name filter."),
				"search":  propStr("Substring search across title and notes."),
				"overdue": propStr("true to only return overdue open tasks."),
				"limit":   propStr("Max results (default 50)."),
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
			"description": "Delete a task permanently.",
			"inputSchema": obj(map[string]any{
				"id": propStr("Task id or short prefix."),
			}, []string{"id"}),
		},
		{
			"name":        "start_timer",
			"description": "Start a timer (optionally on a task).",
			"inputSchema": obj(map[string]any{
				"task_id": propStr("Task id (optional)."),
				"note":    propStr("Optional note."),
				"kind":    propStr("work or pomodoro (default work)."),
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
		var tags []string
		if t := get("tags"); t != "" {
			for _, p := range strings.Split(t, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					tags = append(tags, p)
				}
			}
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
			ProjectID: projectID, DueAt: due, Tags: tags,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("created task %q (id=%s)", t.Title, t.ID), nil

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
		if l := getInt("limit"); l > 0 {
			opts.Limit = l
		}
		tasks, err := s.client.ListTasks(ctx, opts)
		if err != nil {
			return "", err
		}
		return jsonOrText(tasks), nil

	case "show_task":
		t, err := s.client.GetTask(ctx, get("id"))
		if err != nil {
			return "", err
		}
		return jsonOrText(t), nil

	case "complete_task":
		t, err := s.client.CompleteTask(ctx, get("id"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("completed task %q (id=%s)", t.Title, t.ID), nil

	case "delete_task":
		if err := s.client.DeleteTask(ctx, get("id")); err != nil {
			return "", err
		}
		return "deleted", nil

	case "start_timer":
		e, err := s.client.StartTimer(ctx, get("task_id"), get("kind"), get("note"))
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
