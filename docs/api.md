---
layout: default
title: REST API reference
---
# REST API reference

Base URL: `http://localhost:8093` (configurable via `ttl serve --addr`).

All routes are JSON. Two auth options, picked by the request:

| Auth | Use |
|---|---|
| `X-API-Key: ttk_…` | CLI, scripts, MCP |
| `Cookie: ttl_session=…` | web UI (set automatically on login) |

Errors look like:

```json
{ "error": { "code": "not_found", "message": "task not found" } }
```

## Endpoints

### Health

```
GET /health
→ {"status":"ok"}
```

### Auth

```
POST /api/v1/auth/signup    { tenant_name, email, password }
POST /api/v1/auth/login     { email, password }
POST /api/v1/auth/logout
GET  /api/v1/me
```

`signup` and `login` return `200/201` with `{ user }` and set the
`ttl_session` cookie. `me` returns the authenticated user.

### API keys

```
POST /api/v1/api-keys    { name }
→ { key: "ttk_…", api_key: { id, … } }
```

Keys accept `scopes[]` and optional `expires_at`; use `GET /api/v1/api-keys`
to list, `PATCH /api/v1/api-keys/{id}` to rename,
`POST /api/v1/api-keys/{id}/rotate` to replace, and
`DELETE /api/v1/api-keys/{id}` to revoke.

The plaintext `key` is shown **exactly once**. Store it locally; the
server only has the SHA-256 hash.

### Projects

```
GET  /api/v1/projects?archived=1
POST /api/v1/projects    { name, color }
PATCH /api/v1/projects/{id} { name, color }
POST /api/v1/projects/{id}/archive
POST /api/v1/projects/{id}/restore
DELETE /api/v1/projects/{id}/purge
```

### Tags

```
GET  /api/v1/tags
POST /api/v1/tags         { name, color }
PATCH /api/v1/tags/{id}   { name, color }
POST /api/v1/tags/{id}/merge { target_id }
DELETE /api/v1/tags/{id}
```

### Tasks

```
GET    /api/v1/tasks?status=active|open|in_progress|done&project_id=&tag_id=&q=&overdue=1&parent_id=root&limit=200
POST   /api/v1/tasks      { title, notes, priority, project_id, parent_id,
                            due_at (unix ms), recurrence_rrule, tags[] }
GET    /api/v1/tasks/{id}
PATCH  /api/v1/tasks/{id} { any of the above fields }
POST   /api/v1/tasks/{id}/start
POST   /api/v1/tasks/{id}/pause
POST   /api/v1/tasks/{id}/complete
DELETE /api/v1/tasks/{id}
POST   /api/v1/tasks/{id}/restore
DELETE /api/v1/tasks/{id}/purge
POST   /api/v1/tasks/{id}/reorder { project_id?, parent_id?, before_id?, after_id? }
```

`DELETE /tasks/{id}` moves a task subtree to recoverable trash. List smart
views with `?view=inbox|today|upcoming|overdue|next|in_progress|done|trash`.

`complete` returns `{ task, next_occurred }`; `next_occurred` is set
when the completed task had a `recurrence_rrule` and the next
occurrence was auto-created.

### Time tracking

```
POST /api/v1/timer/start   { task_id?, kind?: "work"|"pomodoro", note? }
POST /api/v1/timer/stop    { note? }
GET  /api/v1/timer/active
GET  /api/v1/timer/entries?from=RFC3339&to=RFC3339
GET  /api/v1/worklog/today?tz=America/New_York
```

### Reminders

```
POST   /api/v1/reminders     { task_id, fire_at (unix ms), endpoint_id? }
GET    /api/v1/reminders?status=pending
DELETE /api/v1/reminders/{id}
PATCH  /api/v1/reminders/{id}       { fire_at }
POST   /api/v1/reminders/{id}/ack
POST   /api/v1/reminders/{id}/snooze { fire_at }
```

### Teams and notification endpoints

```
GET/POST /api/v1/invites
GET       /api/v1/members
PATCH     /api/v1/members/{id}       { role }
DELETE    /api/v1/members/{id}
GET/POST  /api/v1/notifications
PATCH     /api/v1/notifications/{id} { enabled }
DELETE    /api/v1/notifications/{id}
```

After the first workspace owner is created, signup requires a single-use invite.
Reminder webhooks carry `X-TTL-Signature: sha256=<hmac>`.

### WebSocket

```
GET /api/v1/ws?token=ttk_…    (or session cookie)
```

Upgrades to a WebSocket and streams JSON events:

```json
{ "kind": "task.created",   "tenant_id": "…", "payload": { "id": "…", "title": "…" } }
{ "kind": "task.completed", "tenant_id": "…", "payload": { "id": "…", "title": "…" } }
{ "kind": "task.updated",   "tenant_id": "…", "payload": { "id": "…" } }
{ "kind": "task.deleted",   "tenant_id": "…", "payload": { "id": "…" } }
{ "kind": "timer.started",  "tenant_id": "…", "payload": { … } }
{ "kind": "timer.stopped",  "tenant_id": "…", "payload": { … } }
{ "kind": "reminder.fired", "tenant_id": "…", "payload": { "id", "task_id", "task_title" } }
```

The server sends `{"kind":"hello"}` immediately on connect, then
30-second pings. Reconnect with exponential backoff if the connection
drops.

## Examples

### Create a task

```bash
curl -X POST http://localhost:8093/api/v1/tasks \
  -H "X-API-Key: ttk_..." \
  -H "Content-Type: application/json" \
  -d '{"title":"Write docs","priority":2,"due_at":1735689600000,"tags":["docs"]}'
```

### List overdue

```bash
curl "http://localhost:8093/api/v1/tasks?status=open&overdue=1" \
  -H "X-API-Key: ttk_..."
```

### Subscribe to live events with `websocat`

```bash
websocat "ws://localhost:8093/api/v1/ws?token=ttk_..."
```

### With `wscat` (Node.js)

```bash
wscat -c "ws://localhost:8093/api/v1/ws?token=ttk_..."
```

## Status codes

| Code | Meaning |
|---|---|
| 200 / 201 | success |
| 204 | success with no body (delete) |
| 400 | validation error |
| 401 | missing or invalid credentials |
| 404 | not found in this tenant |
| 409 | conflict (e.g. timer already running) |
| 500 | unexpected server error |
