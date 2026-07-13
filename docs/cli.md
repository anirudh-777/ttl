---
layout: default
title: CLI reference
---
# CLI reference

Every command accepts `--format text|json|ndjson` for machine-readable
output and `--server URL` to override the configured server.

## Authentication

```bash
ttl signup                          # first workspace + owner + API key
ttl signup --invite TOKEN           # join an existing workspace
ttl login                           # sign in to existing workspace
ttl logout                          # clear local credentials
ttl config show                     # print local config
ttl config server https://…         # point at a remote server
```

Local config lives at `~/.config/ttl/config.json` (mode 0600).
Override with `TTL_CONFIG_DIR=/path/to/dir`.

## Tasks

```bash
# create
ttl add "title" [-p 0..3] [-P project] [-t tag1,tag2] [-d today|tomorrow|YYYY-MM-DD]
ttl add "Ship MVP" -p 3 --due tomorrow -t docs,writing --notes "first cut"

# list
ttl list                            # open tasks
ttl list --all                      # open + done
ttl list --done                     # only done
ttl list --overdue                  # only overdue open
ttl list --search milk              # substring search
ttl list --project Home             # filter by project name
ttl list --view inbox|today|upcoming|overdue|next|done|trash

# inspect
ttl show <id-or-prefix>             # full task by id or 8-char prefix

# modify
ttl done <id>                       # mark done (spawns next if recurring)
ttl edit <id> --title "..." --priority 3 --due none
ttl move <id> --project Work --before <id>
ttl rm <id>                         # move to recoverable trash
ttl restore <id>
ttl purge <id> --yes                # permanently delete a trashed task

# output formats
ttl list --format json | jq '.[].title'
ttl list --format ndjson | jq -r '.id' | xargs -I{} ttl done {}
```

## Projects and tags

```bash
ttl project add "Home" --color "#ff8800"
ttl project list
ttl project edit|archive|restore|purge ...
ttl tag add "urgent"
ttl tag list
ttl tag edit|merge|delete ...
```

## Recurring tasks

```bash
ttl edit 9d000462 --repeat daily
ttl edit 9d000462 --repeat weekdays
ttl edit 9d000462 --repeat 'rrule:FREQ=WEEKLY;BYDAY=MO,WE,FR'
```

When you mark a recurring task done, the next occurrence is created
automatically and due at the next RRULE-computed date.

## Time tracking

```bash
ttl start [task-id]                 # start a work timer (optionally on a task)
ttl start --kind pomodoro           # start a pomodoro timer (use ttl pomodoro)
ttl stop [--note "..."]             # stop the active timer
ttl log                             # show today's work-log
ttl pomodoro [task-id] [--minutes 25]   # start a pomodoro session
```

Only one timer per user. `ttl stop` always stops the active one.

## Reminders

```bash
ttl reminder add <task-id> --at +30m [--endpoint <id>]
ttl reminder list [--status pending|sent|ack]
ttl reminder edit|snooze <id> --at TIME
ttl reminder ack|delete <id>
```

Workspace administration is also terminal-first: `ttl key`, `ttl member`,
and `ttl notification` cover scoped credential lifecycle, invites/roles, and
signed reminder webhook endpoints.

## TUI

```bash
ttl today                           # today view
ttl inbox                           # inbox (root-level open) view
ttl view upcoming                   # any smart view
```

Keys:

| Key | Action |
|---|---|
| `j` / `k` / arrows | move selection |
| `space` | toggle complete on the selected task |
| `n` | new task (type, Enter to save, Esc to cancel) |
| `e` | edit selected task title |
| `d` then `y` | delete selected |
| `r` | refresh from server |
| `1` … `7` | switch smart view |
| `u` | restore selected task in Trash |
| `q` / Ctrl-C | quit |

The TUI shows the active timer at the top in yellow while one is
running.

## MCP server (AI agents)

```bash
ttl mcp                             # run MCP server on stdio
```

See [docs/mcp.md](mcp.html) for setup with Claude, Cursor, or Cline.

## Server

```bash
ttl serve --addr :8093 \
         --db ~/.local/share/ttl/ttl.db \
         --reminder-interval 60s \
         --trash-retention 720h
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `--addr` | `:8093` | listen address |
| `--db` | `~/.local/share/ttl/ttl.db` | SQLite path |
| `--reminder-interval` | `60s` | how often to scan for due reminders |
| `--trash-retention` | `720h` | permanently purge older trash; `0` disables |

## Environment variables

| Var | Effect |
|---|---|
| `TTL_CONFIG_DIR` | override config directory (default `~/.config/ttl`) |
| `TTL_DATA_DIR` | default location for `serve --db` |
| `TTL_ALLOW_OPEN_SIGNUP` | allow public workspace creation; default is invite-only after bootstrap |
| `TTL_ALLOWED_ORIGINS` | explicit cross-origin allowlist |
| `TTL_SECURE_COOKIES` | require HTTPS-only session cookies |
