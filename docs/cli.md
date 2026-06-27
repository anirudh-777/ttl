# CLI reference

Every command accepts `--format text|json|ndjson` for machine-readable
output and `--server URL` to override the configured server.

## Authentication

```bash
ttl signup                          # workspace + user + API key
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
ttl list                            # today's open tasks
ttl list --all                      # open + done
ttl list --done                     # only done
ttl list --overdue                  # only overdue open
ttl list --search milk              # substring search
ttl list --project Home             # filter by project name

# inspect
ttl show <id-or-prefix>             # full task by id or 8-char prefix

# modify
ttl done <id>                       # mark done (spawns next if recurring)
ttl edit <id> --title "..." --priority 3 --due none
ttl rm <id>                         # delete

# output formats
ttl list --format json | jq '.[].title'
ttl list --format ndjson | jq -r '.id' | xargs -I{} ttl done {}
```

## Projects and tags

```bash
ttl project add "Home" --color "#ff8800"
ttl project list
ttl tag add "urgent"
ttl tag list
```

## Recurring tasks

```bash
# Edit the recurrence_rrule field. RRULE uses iCalendar syntax.
ttl edit 9d000462 --rrule "FREQ=DAILY;INTERVAL=1"
ttl edit 9d000462 --rrule "FREQ=WEEKLY;INTERVAL=1;BYDAY=MO,WE,FR"
ttl edit 9d000462 --rrule "FREQ=MONTHLY;INTERVAL=1"
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

Reminders are managed via the API/CLI in Phase 3. CLI wrapper
commands will land in Phase 4 alongside integrations.

## TUI

```bash
ttl today                           # today view
ttl inbox                           # inbox (root-level open) view
```

Keys:

| Key | Action |
|---|---|
| `j` / `k` / arrows | move selection |
| `space` | toggle complete on the selected task |
| `n` | new task (type, Enter to save, Esc to cancel) |
| `d` then `y` | delete selected |
| `r` | refresh from server |
| `q` / Ctrl-C | quit |

The TUI shows the active timer at the top in yellow while one is
running.

## MCP server (AI agents)

```bash
ttl mcp                             # run MCP server on stdio
```

See [docs/mcp.md](mcp.md) for setup with Claude, Cursor, or Cline.

## Server

```bash
ttl serve --addr :8093 \
         --db ~/.local/share/ttl/ttl.db \
         --reminder-interval 60s
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `--addr` | `:8093` | listen address |
| `--db` | `~/.local/share/ttl/ttl.db` | SQLite path |
| `--reminder-interval` | `60s` | how often to scan for due reminders |

## Environment variables

| Var | Effect |
|---|---|
| `TTL_CONFIG_DIR` | override config directory (default `~/.config/ttl`) |
| `TTL_DATA_DIR` | default location for `serve --db` |
