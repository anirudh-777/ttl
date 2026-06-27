# ttl — terminal-first task tracker

A single-binary, multi-tenant task tracker built for speed, agents, and
the keyboard. One static ~13 MB binary that ships a CLI, a Bubble Tea
TUI, a REST API, a WebSocket live-update channel, an MCP server for AI
agents, and a web UI.

```
$ ttl add "Ship the README" --due today -p 3
$ ttl list
$ ttl today                          # interactive TUI
$ ttl mcp                            # expose tools to Claude / Cursor / Cline
```

## Why ttl exists

Super Productivity is brilliant but heavy: Electron, Angular, IndexedDB,
plugins, integrations, themes. Most of that surface area is unused for
90% of users. ttl keeps the useful subset and drops the rest:

- **One static binary.** No Node, no Electron, no Docker required to
  run it. SQLite + Go = 13 MB stripped.
- **Three first-class surfaces.** Terminal for keyboard warriors, web
  for remote devices, MCP for AI agents.
- **Multi-tenant from day one.** Every row is scoped by `tenant_id` at
  the storage layer. Cross-tenant access is structurally impossible.
- **AI-native.** Stable REST API, NDJSON streaming, MCP server,
  programmable from any language.

## Feature status

| Phase | Status | What shipped |
|---|---|---|
| 1. MVP | done | CLI, REST, TUI (today/inbox), web UI, multi-tenant auth, cross-platform builds |
| 2. Focus | done | Start/stop timer, Pomodoro, daily work-log, active-timer banner |
| 3. AI + live | done | Recurring tasks (RRULE), reminders, WebSocket live updates, MCP server (10 tools) |
| 4. Integrations | done | GitHub + Linear providers, webhook receiver with HMAC verification, two-way sync |

## Quick start

```bash
# 1. Build
make build         # produces ./bin/ttl (mac/linux) — or grab a release

# 2. Start the server (one terminal)
./bin/ttl serve                       # listens on :8093 by default

# 3. Sign up (another terminal)
./bin/ttl signup                      # creates workspace + user + API key

# 4. Use it
./bin/ttl add "Buy milk" -p 2 --due today -t shopping
./bin/ttl list
./bin/ttl today                       # interactive TUI
```

Open <http://localhost:8093/login> for the web UI.

> **Ports.** The default is `:8093` to avoid clashing with dev servers
> on `:8093`. Override with `ttl serve --addr :NNNN` for the server and
> `TTL_SERVER_URL=http://host:port` for the CLI.

## Surfaces

| Surface | Try |
|---|---|
| CLI | `ttl add`, `ttl list`, `ttl done`, `ttl start`, `ttl stop`, `ttl log`, `ttl pomodoro` |
| TUI | `ttl today`, `ttl inbox` — vim-style keys (j/k, space, n, d, r, q) |
| Web | <http://localhost:8093/today> — Today / Inbox / Projects / Settings |
| API | `curl -H "X-API-Key: ttk_..." http://localhost:8093/api/v1/tasks` |
| MCP | `ttl mcp` — 10 tools for Claude / Cursor / Cline |
| WebSocket | `ws://localhost:8093/api/v1/ws` — live events |

## Stack

- **Go 1.22+** — single static binary, no CGO (`modernc.org/sqlite`)
- **SQLite** — one file per deployment, WAL mode, partial indexes
- **charm.sh/bubbletea** — TUI
- **go-chi/chi** — HTTP router
- **spf13/cobra** — CLI
- **coder/websocket** — live updates
- **teambition/rrule-go** — recurring tasks
- **vanilla JS** — web UI (~12 KB, no build step)

## Install

```bash
# from source
git clone https://github.com/anirudh-777/ttl
cd ttl
make build

# or download a release
curl -L https://github.com/anirudh-777/ttl/releases/latest/download/ttl-darwin-arm64 -o ttl
chmod +x ttl

# homebrew (when published)
brew install ttl
```

## Documentation

| Doc | Covers |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Storage model, multi-tenancy, layering |
| [docs/cli.md](docs/cli.md) | Every command, every flag |
| [docs/api.md](docs/api.md) | REST API reference + curl examples |
| [docs/mcp.md](docs/mcp.md) | Tool catalogue + Claude/Cursor/Cline setup |
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

## License

MIT.
