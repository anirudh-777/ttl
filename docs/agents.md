# Installing ttl for coding agents

`ttl agents` installs two complementary pieces:

- the embedded ttl skill, which teaches an agent when and how to manage work;
- the local `ttl mcp` server, which gives supported agents structured tools.

The MCP process uses the server URL and API key already stored by `ttl login`.
Keys are never copied into agent configuration files.

## Quick start

```bash
ttl login --server https://tasks.example.com
ttl agents install
```

With no flags, installed coding-agent CLIs are detected automatically. Restart
the configured agents, then ask: "Use ttl to show my open tasks."

## Commands

```bash
ttl agents install                         # configure detected agents
ttl agents install --agent codex           # one agent
ttl agents install --agent claude --agent cursor
ttl agents install --all                   # every supported user-level target
ttl agents install --skills-only           # do not change MCP configuration
ttl agents install --dry-run               # preview changes
ttl agents status
ttl agents update
ttl agents uninstall                       # remove only ttl-owned files/entries
```

Supported skill targets are Claude Code, OpenAI Codex, Cursor, Continue.dev,
Cline / Roo-Cline, and Roo Code. MCP registration is automatic for Claude Code,
Codex, and Cursor. Other clients can add the same stdio server manually:

```json
{
  "mcpServers": {
    "ttl": {
      "command": "ttl",
      "args": ["mcp"]
    }
  }
}
```

## Safety

- Installs are user-scoped; project repositories are not modified.
- Existing files and MCP entries named `ttl` are preserved, not overwritten.
- `~/.config/ttl/agents.json` records only resources created by ttl.
- Updates replace an older skill only when it still matches the recorded hash.
- Uninstall refuses to remove a skill the user modified after installation.
- Cursor JSON is merged atomically and unrelated MCP servers are preserved.

## Legacy remote installer

For older ttl releases, the remote helper remains available:

```bash
curl -sSL https://raw.githubusercontent.com/anirudh-777/ttl/main/scripts/install-skill-remote.sh \
  | bash -s -- all
```

When a current ttl binary is present, this helper delegates to
`ttl agents install --all`.

## Verify

```bash
ttl agents status
ttl config show
```

Then ask the agent to add a disposable task, list it, and move it to trash.
