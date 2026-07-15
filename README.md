# ttl — the agents-first task tracker

**One task system for you and your coding agents.** ttl is a fast,
self-hostable task tracker where humans work from the terminal or web and
agents manage the same tasks through structured MCP tools. It ships as one
static ~13 MB binary with SQLite, a CLI, TUI, installable web app, REST API, WebSocket
updates, and a built-in MCP interface. There is no separate agent service to
deploy: your coding agent launches `ttl mcp` locally when it needs a tool.

**Project site: <https://anirudh-777.github.io/ttl/>**

```
$ ttl add "Ship the README" --due today -p 3
$ ttl list
$ ttl today                          # interactive TUI
$ ttl mcp                            # expose tools to Claude / Cursor / Cline
$ ttl agents install                 # install the skill + register MCP
```

## Why ttl exists

Task trackers were designed for humans clicking through interfaces. Agent
workflows are usually bolted on later, forcing coding agents to parse output,
drive a browser, or work from a separate source of truth. ttl treats humans
and agents as first-class users of the same task system:

- **One static binary.** No Node, no Electron, no Docker required to
  run it. SQLite + Go = 13 MB stripped.
- **Agents are first-class users.** Structured MCP tools cover task CRUD,
  subtasks, reminders, timers, and smart views.
- **One command onboards coding agents.** `ttl agents install` detects
  supported agents, installs the ttl skill, and registers MCP safely.
- **Humans keep excellent interfaces.** Use the fast CLI and TUI at your
  desk, or install the embedded web app on a phone or desktop.
- **Multi-tenant from day one.** Every row is scoped by `tenant_id` at
  the storage layer. Cross-tenant access is structurally impossible.
- **Easy to own.** Self-host one process and one SQLite database, or point
  the CLI and your agents at a shared deployment.

## Feature status

| Phase | Status | What shipped |
|---|---|---|
| 1. MVP | done | CLI, REST, TUI (today/inbox), web UI, multi-tenant auth, cross-platform builds |
| 2. Focus | done | Start/stop timer, Pomodoro, daily work-log, active-timer banner |
| 3. AI + live | done | Recurring tasks (RRULE), reminders, WebSocket live updates, built-in MCP tools |
| 4. Integrations | done | GitHub + Linear providers, webhook receiver with HMAC verification, two-way sync |
| v1 readiness | done | Recoverable trash, smart views, complete agent CRUD, recurrence/reminders, scoped keys, team invites |
| 1.1 Web productivity | done | Today-by-default capture, daily completions, Pomodoro controls, productivity metrics and 14-day trends, installable PWA |

## Install

```bash
# macOS / Linux / WSL — downloads the latest release and verifies SHA256
curl -sSL https://raw.githubusercontent.com/anirudh-777/ttl/main/scripts/install.sh | bash

# verify
ttl version
```

The installer uses `/usr/local/bin` when writable and otherwise installs to
`~/.local/bin`. To choose a location or pin a release:

```bash
curl -sSL https://raw.githubusercontent.com/anirudh-777/ttl/main/scripts/install.sh \
  | bash -s -- --to ~/.local/bin --version v1.1.0
```

With Go 1.25+ you can instead run:

```bash
go install github.com/anirudh-777/ttl/cmd/ttl@latest
```

See [docs/install.md](docs/install.md) for Docker, updates, uninstall, and
manual release downloads.

## Quick start

```bash
# 1. Start your server
ttl serve                              # listens on :8093 by default

# 2. Create a workspace in another terminal
ttl signup                             # creates user + API key

# 3. Capture and manage work
ttl add "Buy milk" -p 2 --due today -t shopping
ttl list --view upcoming
ttl today                              # interactive TUI

# 4. Give your installed coding agents access
ttl agents install
```

Open <http://localhost:8093/login> for the web UI. On a phone, use the
browser's **Install app** or **Add to Home Screen** action to run ttl like a
standalone app. Task and account data are never stored in the offline cache.

> **Ports.** The default is `:8093`. Override with `ttl serve --addr :NNNN` for the server and
> `TTL_SERVER_URL=http://host:port` for the CLI.

## Surfaces

| Surface | Try |
|---|---|
| CLI | `ttl add`, `ttl list`, `ttl progress`, `ttl pause`, `ttl done`, `ttl start`, `ttl stop`, `ttl log`, `ttl pomodoro` |
| TUI | `ttl view <inbox|today|upcoming|overdue|next|done|trash>` — vim-style keys |
| Web | <http://localhost:8093/today> — installable PWA, Today insights, focus controls, smart views, projects, team and keys |
| API | `curl -H "X-API-Key: ttk_..." http://localhost:8093/api/v1/tasks` |
| MCP interface | `ttl mcp` — structured task CRUD, subtasks, reminders, timers, and smart views for agents |
| Agent setup | `ttl agents install` — detect coding agents, install the skill, register MCP |
| WebSocket | `ws://localhost:8093/api/v1/ws` — live events |

## Stack

- **Go 1.25+** — single static binary, no CGO (`modernc.org/sqlite`)
- **SQLite** — one file per deployment, WAL mode, partial indexes
- **charm.sh/bubbletea** — TUI
- **go-chi/chi** — HTTP router
- **spf13/cobra** — CLI
- **coder/websocket** — live updates
- **teambition/rrule-go** — recurring tasks
- **vanilla JS** — installable web UI with no framework or build step

## Documentation

| Doc | Covers |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Storage model, multi-tenancy, layering |
| [docs/cli.md](docs/cli.md) | Every command, every flag |
| [docs/api.md](docs/api.md) | REST API reference + curl examples |
| [docs/mcp.md](docs/mcp.md) | Tool catalogue + Claude/Cursor/Cline setup |
| [docs/agents.md](docs/agents.md) | One-command coding-agent setup and safety model |
| [docs/install.md](docs/install.md) | Install, update, and uninstall options |
| [docs/integrations.md](docs/integrations.md) | GitHub, Linear, webhook setup, security |
| [docs/deploy.md](docs/deploy.md) | Single host, Docker, production checklist |

## Development

```bash
make test          # runs all tests
make vet           # go vet ./...
make build         # current OS
make build-all     # linux/darwin/windows × amd64/arm64
make docker        # distroless image (~20 MB)
```

## v1 productivity workflow

```bash
ttl add "Weekly review" --due friday --repeat weekly -t planning
ttl edit <id> --tag planning,deep-work --due tomorrow
ttl reminder add <id> --at +2h
ttl pomodoro <id> --minutes 25
ttl list --view next
ttl rm <id>                 # recoverable trash
ttl restore <id>
ttl purge <id> --yes        # permanent
```

The same task lifecycle is exposed through REST and MCP. Agent keys are named,
scoped, optionally expiring, renameable, safely rotatable, and immediately revocable.

By default, only the first account may create a workspace; later signups require
a single-use invite. Set `TTL_ALLOW_OPEN_SIGNUP=true` only when intentionally
offering public workspace creation. Trashed tasks are purged after 30 days by
default; configure this with `ttl serve --trash-retention`.

## License

MIT.
