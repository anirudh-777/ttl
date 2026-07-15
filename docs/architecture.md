---
layout: default
title: Architecture
---
# Architecture

## Layers

```
cmd/ttl/           CLI subcommands, main entrypoint, embedded web assets
internal/
  api/             HTTP handlers (REST + WebSocket upgrade)
  auth/            bcrypt, sessions, API keys
  client/          HTTP client used by CLI / TUI / MCP
  config/          local CLI config (server URL + API key)
  db/              SQLite open + embedded migrations
  events/          in-process pub/sub (fan-out for WS)
  fmtcmd/          text / JSON / NDJSON renderers
  mcp/             Model Context Protocol server (stdio, JSON-RPC 2.0)
  model/           domain types (Task, Project, TimeEntry, Reminder)
  store/           every SQL query (every method takes *tenant.Context)
  tenant/          per-request tenant/user context
  recurrence/      preset and RFC 5545 recurrence normalization
  tui/             Bubble Tea smart views
  ws/              WebSocket upgrade handler + tenant filter
migrations/        SQL files (also embedded into internal/db/migrations)
```

Every layer depends only on layers below it. The store never imports
the API; the CLI never imports the store. All persistence goes through
`internal/store`, all transport goes through `internal/api`,
`internal/ws`, or `internal/mcp`.

## Storage model

One SQLite database per deployment (`~/.local/share/ttl/ttl.db` by
default). The file can be copied, backed up, or replicated with `cp`.

WAL mode is enabled at open, plus `busy_timeout=5000` so writes don't
fail under modest contention. Foreign keys are on; cascade rules keep
deletes consistent.

### Tables (v1)

```
tenants            id, name, created_at
users              id, tenant_id, email, password_hash, role, created_at
api_keys           id, user_id, tenant_id, key_hash, name, scopes_json,
                   expires_at, last_used_at, created_at
sessions           id, user_id, tenant_id, expires_at, created_at
projects           id, tenant_id, name, color, archived_at, created_at
tags               id, tenant_id, name, color, created_at
tasks              id, tenant_id, project_id, parent_id, title, notes,
                   status, priority, due_at, recurrence_rrule,
                   position, created_by, created_at, updated_at, started_at, completed_at, deleted_at
task_tags          task_id, tag_id, tenant_id
time_entries       id, tenant_id, user_id, task_id, kind,
                   started_at, ended_at, duration_ms, planned_duration_ms, deadline_at, note
reminders          id, tenant_id, task_id, user_id, fire_at,
                   endpoint_id, status, delivery_status, created_at, sent_at
notification_endpoints id, tenant_id, name, kind, url, secret_enc, enabled
invites            id, tenant_id, role, token_hash, expires_at, used_at
integrations       id, tenant_id, provider, label, config_json, created_at
issue_links        id, tenant_id, task_id, provider, external_id,
                   external_url, last_synced_at
```

### Indexes

Every business query path is covered by a composite index whose
leading column is `tenant_id`. SQLite's query planner uses these
directly, and `tenant_id` first ensures the planner can never scan
across tenants even without the `WHERE` clause.

A few partial indexes:

```
UNIQUE INDEX idx_time_entries_open_per_user
  ON time_entries(user_id) WHERE ended_at IS NULL;

INDEX idx_reminders_pending
  ON reminders(tenant_id, fire_at) WHERE status = 'pending';
```

The first enforces "at most one open timer per user" at the DB level.
The second keeps the reminder ticker's scan cheap (only pending rows).

### Migrations

Migrations live in `internal/db/migrations/*.sql` and are embedded into
the binary via `//go:embed`. On `db.Open`, the runner applies any
migration whose version is greater than the recorded `schema_meta.version`
inside a single transaction. Adding a migration is: drop a new
`NNNN_*.sql` file, bump nothing else.

## Multi-tenancy

Multi-tenancy is row-level: a single SQLite database holds every
workspace. The invariant is enforced two ways:

1. **Type system.** Every `store` method takes a
   `*tenant.Context{TenantID, UserID, Role}` as its first argument.
   Cross-tenant data is impossible to fetch without that context.
2. **Database queries.** Every SELECT, UPDATE, and DELETE carries
   `WHERE tenant_id = ?` (and `WHERE id = ? AND tenant_id = ?` for
   single-row lookups).

The `internal/store/store_test.go` suite has a `TestCrossTenantIsolation`
test that proves even with valid auth tokens, the store refuses to
read or write another tenant's rows.

## Authentication

Three auth paths, all backing onto the same `tenant.Context`:

| Path | Used by | Storage |
|---|---|---|
| Session cookie (`ttl_session=...`) | Web UI | `sessions` table, 30-day TTL |
| API key (`X-API-Key: ttk_...`) | CLI, MCP, scripts | `api_keys` table, SHA-256 hash stored |
| WebSocket upgrade (`/api/v1/ws?token=ttk_...` or session cookie) | Live updates | resolved on upgrade |

API keys are `ttk_` followed by 32 random base64-url chars. The
plaintext is shown exactly once on creation; only the SHA-256 hash is
stored. Keys carry explicit scopes and optional expiry and can be renamed,
rotated, or revoked. The plaintext never leaves the user's machine except over
HTTPS in transit.

## Pub/sub

`internal/events.Hub` is a tiny in-process broker used for two things:

1. **WebSocket fan-out.** When `POST /api/v1/tasks/{id}/complete`
   succeeds, the handler calls `hub.Publish(KindTaskCompleted, ...)`.
   Every connected WebSocket whose tenant matches gets the event.
2. **Reminder ticker.** `runReminderTicker` runs every 60s
   (configurable via `--reminder-interval`), fetches due reminders,
   and publishes them.

Subscribers are buffered channels. If a subscriber's buffer fills up,
events are dropped for that subscriber (counted by `hub.Dropped()`)
rather than blocking the publisher.

## Concurrency

SQLite serialises writers via WAL mode + `MaxOpenConns(1)`. Readers
are concurrent. This is fine for a single-user or small-team
workload; for high-write workloads you'd want to switch to Postgres
later (the data model is portable).

The WebSocket hub is goroutine-safe (RWMutex). Each client runs in
its own goroutine; the publish path is O(subscribers).

## Cross-platform builds

```bash
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build ...
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build ...
```

CGO is disabled so the binary doesn't depend on system libraries.
`modernc.org/sqlite` is a pure-Go re-implementation of SQLite. The
binary runs on any amd64/arm64 linux, macOS, or Windows.

`scripts/build.sh` produces all five target binaries plus
`SHA256SUMS`.
