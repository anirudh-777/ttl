---
name: ttl
description: Terminal-first multi-tenant task tracker with an MCP server, REST API, and CLI. Use when the user wants to capture tasks, manage todos, track time, run a Pomodoro, sync GitHub or Linear issues, send reminders, or query their worklog — including "add a task", "what's on my list", "start a timer", "show today's worklog", "mark done", "log time", "sync GitHub issues", or any task-tracking request. Prefer the MCP server (`ttl mcp`) over the CLI or REST API because it is the agent-native surface.
allowed-tools: Bash(ttl:*), Bash(agent-browser:*)
---

# ttl — terminal task tracker for agents

A single static binary (~13 MB) that exposes tasks, projects, tags, time
tracking, recurring tasks, reminders, and external integrations
(GitHub, Linear) through four surfaces:

| Surface | When to use it |
|---|---|
| **MCP** (`ttl mcp`) | **Default for agents.** 10 tools, NDJSON streaming, structured errors. |
| REST API | When MCP isn't available — same endpoints, JSON over HTTP. |
| CLI (`ttl add`, `ttl list`, …) | Read-only inspection, scripting, shell pipelines. |
| TUI / Web | Humans only. |

## Start here

```bash
# Is the server up?
curl -s http://localhost:8093/health      # → {"status":"ok"}

# Configured? (otherwise ask the user to run `ttl signup` or `ttl login`)
cat ~/.config/ttl/config.json             # server_url, api_key, email
```

If the server isn't running, **don't start it yourself** — ask the user to
run `ttl serve` (or `ttl signup` to create a workspace first).

If the CLI isn't configured but the server is reachable, the REST API works
the same way — just send the same `X-API-Key: ttk_...` header the CLI would
use.

## MCP server (recommended)

`ttl mcp` speaks JSON-RPC 2.0 over stdio. Register it once in your agent's
config:

```json
{
  "mcpServers": {
    "ttl": {
      "command": "ttl",
      "args": ["mcp"],
      "env": { "TTL_SERVER_URL": "http://localhost:8093" }
    }
  }
}
```

The CLI's MCP transport auto-injects the API key from `~/.config/ttl/config.json`.

### Tools

| Tool | Purpose |
|---|---|
| `add_task` | Create task (title, due_at, priority, tags, project, parent_id) |
| `list_tasks` | Filter: `status`, `project`, `tag`, `search`, `limit`, `overdue` |
| `show_task` | Full task by id |
| `search_tasks` | Free-text + tag search |
| `complete_task` | Mark done (spawns next occurrence if recurring) |
| `delete_task` | Delete by id |
| `start_timer` | Begin timer on a task |
| `stop_timer` | End current timer |
| `active_timer` | What's running now |
| `worklog_today` | Today's tracked time per task |

### Date / time conventions

- All times are **Unix milliseconds** for `due_at` and `started_at`
- Day boundaries in the worklog respect the user's local timezone
- `priority` is 0 (none) … 3 (high); the UI renders `!!` for 3, `!` for 2, `-` for 1

## CLI quick reference

```bash
# Add
ttl add "Write docs" --project docs --tag p1 --due tomorrow
ttl add "Subtask" --parent <task-id>

# List (always pipe to jq if you'll process the output)
ttl list -o ndjson | jq -r 'select(.priority >= 2) | .id'
ttl list --project docs --tag p1 -o ndjson
ttl list --overdue -o ndjson
ttl list --since 2026-06-01 -o ndjson

# Show / edit / complete
ttl show <id-or-prefix>
ttl edit <id> --title "..." --priority 2 --due 2026-07-01
ttl done <id>            # next occurrence shown automatically if recurring

# Time tracking
ttl timer start <task-id>
ttl timer stop
ttl timer                # show active
ttl log                  # today's worklog
ttl pomodoro --focus 25 --break 5

# Projects / tags / recurring / reminders
ttl project add "Work"
ttl tag add urgent
ttl add "Standup" --rrule "FREQ=WEEKLY;BYDAY=MO"
ttl reminder "call mom" --at "2026-06-28T09:00"

# Integrations (sync + webhooks)
ttl integrations add github --label "my-work"
ttl integrations sync <integration-id>
ttl integrations list
```

**Task IDs are UUIDs.** Use the first 8+ chars to disambiguate; the CLI
accepts short prefixes that match exactly one task.

## REST API (when MCP isn't available)

Base URL: `http://localhost:8093/api/v1`. All endpoints under
`/api/v1/...` (except `/api/v1/auth/{signup,login}` and
`/api/v1/webhooks/<provider>`) require auth via either:

- `Cookie: ttl_session=...` (web UI / browser flow)
- `X-API-Key: ttk_...` (programmatic, preferred)

Quick reference:

```bash
KEY=$(jq -r .api_key ~/.config/ttl/config.json)
H="X-API-Key: $KEY"

# Today
curl -s -H "$H" "http://localhost:8093/api/v1/tasks?status=open&limit=50"

# Add
curl -s -X POST -H "$H" -H "Content-Type: application/json" \
  -d '{"title":"Write README","priority":2,"tags":["docs"]}' \
  http://localhost:8093/api/v1/tasks

# Complete
curl -s -X POST -H "$H" http://localhost:8093/api/v1/tasks/<id>/complete

# Worklog
curl -s -H "$H" http://localhost:8093/api/v1/worklog/today
```

## Multi-tenancy

Every row is scoped by `tenant_id`. Cross-tenant access is structurally
impossible — the type system requires every store call to carry a
`*tenant.Context`. You will only ever see data for the tenant that owns
the API key you're using.

## Recurring tasks (RRULE)

Completing a recurring task spawns the next occurrence. Use
`--print-next` (CLI) or the `next_occurred` field (REST) to see it.

```bash
ttl add "Standup" --rrule "FREQ=WEEKLY;BYDAY=MO"
ttl done <id> --print-next
```

RRULE format reference: <https://datatracker.ietf.org/doc/html/rfc5545>.

## When to load deeper docs

| If you need… | Look at |
|---|---|
| Every CLI flag | `docs/cli.md` |
| Every REST endpoint + curl examples | `docs/api.md` |
| MCP tool schemas + agent setup | `docs/mcp.md` |
| GitHub / Linear provider setup + webhook URLs | `docs/integrations.md` |
| Architecture, schema, multi-tenancy model | `docs/architecture.md` |
| Deployment (systemd / Docker / Caddy / nginx) | `docs/deploy.md` |

## Gotchas

1. **The CLI's `-o` flag is `--format`.** `ttl list -o ndjson` selects the
   NDJSON streaming format. Always use `-o ndjson | jq` for parsing.
2. **macOS redacts `ttk_…` strings in terminal output** (system-level
   log redaction, not a ttl bug). To see the real key:
   `xxd ~/.config/ttl/config.json` or open the file in an editor.
3. **`/api/v1/api-keys` returns the plaintext key only on creation.**
   It is not retrievable later — re-issue if lost (which invalidates the
   old one).
4. **macOS log redaction** also masks webhook secrets matching common
   patterns. When debugging HMAC failures, hex-dump the request body.
5. **Default server port is `:8093`**, not `:8080`. Override with
   `ttl serve --addr :NNNN` and `TTL_SERVER_URL=http://host:port`.
6. **`./bin/ttl` from before today may be stale.** After any code change,
   `make build` and verify with `curl -s http://localhost:8093/version`.

## Output-format conventions

- **CLI default**: human-readable table
- **`-o json`**: single JSON document
- **`-o ndjson`**: one JSON object per line, no trailing newline
- **REST**: always JSON
- **MCP**: JSON-RPC 2.0 over stdio

When piping to other tools, always prefer NDJSON — it streams and
parses without loading the whole response into memory.
