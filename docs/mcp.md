---
layout: default
title: MCP server (AI agents)
---
# MCP server (AI agents)

ttl ships a built-in [Model Context Protocol](https://modelcontextprotocol.io)
server speaking JSON-RPC 2.0 over stdio. Run it with `ttl mcp`.

## Setup

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS) or `%APPDATA%/Claude/claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "ttl": {
      "command": "/usr/local/bin/ttl",
      "args": ["mcp"]
    }
  }
}
```

Restart Claude Desktop. `ttl` will appear under the tool menu.

### Cursor

`Cursor → Settings → MCP → Add new global MCP server`:

```json
{
  "ttl": {
    "command": "ttl",
    "args": ["mcp"]
  }
}
```

### Cline / Continue / etc.

Same shape: a stdio process that speaks JSON-RPC 2.0.

## Tools exposed

| Tool | Purpose |
|---|---|
| `add_task` | Create a new task. Accepts `title`, `notes`, `priority`, `due_at`, `tags`, `project`. |
| `list_tasks` | List tasks. Defaults to `status=open`. Filters: `project`, `search`, `overdue`, `limit`. |
| `show_task` | Get a task by id or short prefix. |
| `update_task` | Edit title, notes, priority, due date, project, tags, or recurrence. |
| `complete_task` | Mark a task done. If the task is recurring, the next occurrence is created automatically. |
| `delete_task` / `restore_task` / `purge_task` | Trash, recover, or permanently purge a task. |
| `reorder_task` | Move or manually order a task or subtask. |
| `add_subtask` / `list_subtasks` | Manage task hierarchy. |
| `reminder_add` / `reminders_list` | Schedule and inspect reminders. |
| `reminder_ack` / `reminder_snooze` / `reminder_delete` | Manage reminder lifecycle. |
| `start_timer` | Start a work or pomodoro timer (optionally on a task). |
| `stop_timer` | Stop the active timer. |
| `active_timer` | Return the running timer (or "no active timer"). |
| `worklog_today` | Total tracked time today, broken down by task. |
| `search_tasks` | Substring search across title and notes. |
| `projects_list` / `project_*` | Create, update, archive, restore, and purge projects. |
| `tags_list` / `tag_*` | Create, update, merge, and remove tags. |
| `keys_list` / `key_*` | Create, rename, rotate, and revoke scoped credentials. |
| `members_list` / `member_*` | Invite and administer workspace members. |
| `notifications_list` / `notification_*` | Manage signed reminder webhook endpoints. |

## Example prompts (Claude)

> "Add a task to ship the README tomorrow, tagged docs, priority high."

> "What am I working on right now?"

> "How much time did I log on the auth refactor today?"

> "Mark the standup notes task as done and add a new one for tomorrow's demo."

## Date formats accepted

`due_at` accepts:

- ISO-8601 / RFC 3339 (`2026-06-27T18:00:00+05:30`)
- `YYYY-MM-DD` (`2026-06-27`)
- `today`, `tomorrow`

## Authentication

`ttl mcp` reads the same `~/.config/ttl/config.json` the CLI uses.
Run `ttl login` (or `ttl signup`) once on the machine that hosts the
MCP server; the persisted API key is sent as `X-API-Key` on every call.
For agents, prefer `ttl key create agent --scope tasks:read,tasks:write`
and add productivity or permanent-delete scopes only when required.

If you need to point the MCP server at a different backend, edit
`config.json` directly or set `TTL_CONFIG_DIR`.

## Example session (raw JSON-RPC)

```
→ {"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
← {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"ttl","version":"v1"}}}

→ {"jsonrpc":"2.0","id":2,"method":"tools/list"}
← {"jsonrpc":"2.0","id":2,"result":{"tools":[…]}}

→ {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add_task","arguments":{"title":"Ship README"}}}
← {"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"created task \"Ship README\" (id=…)" }]}}
```

## Limitations

- The server does not currently push live events. Tools are
  request/response only. Phase 4+ may add subscription resources.
- Tools are constrained by the configured API key's server-enforced scopes.
  The stdio process itself remains local and should still be treated as trusted.
