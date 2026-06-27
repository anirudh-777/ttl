# Installing the ttl skill in coding agents

`skills/ttl/SKILL.md` is a single-file agent skill. Most coding agents
either pick it up automatically from a well-known location, or one
command copies it into place.

## One-shot install (any agent)

```bash
# From the repo root:
./scripts/install-skill.sh              # interactive picker
./scripts/install-skill.sh all          # install for every supported agent
./scripts/install-skill.sh claude       # one agent
./scripts/install-skill.sh --uninstall  # remove from everywhere
```

The script copies `skills/ttl/SKILL.md` to the right path and prints
what it did.

## Per-agent instructions

### Factory / Droid

Already wired. The repo ships `skills/ttl/SKILL.md` and
`skills-lock.json` references it. When you load the skill in a Droid
session, the manifest hash is verified automatically.

To verify locally:

```bash
shasum -a 256 skills/ttl/SKILL.md
# expected: c18c0172f56aa12ebb5ea90b492d9fcf1d37be89b4938c7c883b5af153a54597
```

If the hash mismatches (you edited the skill), the loader will re-prompt
before trusting it.

### Claude Code

```bash
# Project-scoped (committed to the repo, shared with teammates)
mkdir -p .claude/skills/ttl
cp skills/ttl/SKILL.md .claude/skills/ttl/SKILL.md

# Or user-scoped (just for you, across projects)
mkdir -p ~/.claude/skills/ttl
cp skills/ttl/SKILL.md ~/.claude/skills/ttl/SKILL.md
```

Restart Claude Code. The skill will appear under the `/` menu.

### Cursor

Cursor reads `.cursor/rules/*.md` files as workspace rules:

```bash
mkdir -p .cursor/rules
cp skills/ttl/SKILL.md .cursor/rules/ttl.md
```

Then open Cursor → Settings → Rules and enable "ttl" for this workspace.

For per-user rules (Cursor 0.42+):

```bash
mkdir -p ~/.cursor/rules
cp skills/ttl/SKILL.md ~/.cursor/rules/ttl.md
```

### OpenAI Codex

Codex reads `AGENTS.md` from the project root or any ancestor. The
`SKILL.md` format is close enough but Codex expects `AGENTS.md`. Create
a thin wrapper:

```bash
cat > AGENTS.md <<EOF
# Agent instructions

This project uses **ttl** for task tracking. Read
\`skills/ttl/SKILL.md\` before doing any task-tracking work.

$(cat skills/ttl/SKILL.md)
EOF
```

Or symlink:

```bash
ln -s skills/ttl/SKILL.md AGENTS.md
```

### GitHub Copilot

For workspace-aware instructions:

```bash
mkdir -p .github
cp skills/ttl/SKILL.md .github/copilot-instructions.md
```

For Copilot Chat in the IDE, this file is automatically attached to
every chat in the workspace.

### Continue.dev

```bash
mkdir -p .continue/rules
cp skills/ttl/SKILL.md .continue/rules/ttl.md
```

Then in Continue → Rules, enable `ttl`.

### Windsurf

```bash
cp skills/ttl/SKILL.md .windsurfrules
```

Restart the workspace.

### Cline / Roo Code

```bash
mkdir -p .clinerules
cp skills/ttl/SKILL.md .clinerules/ttl.md
```

For Roo: `.roo/rules/ttl.md`.

### Aider

```bash
cp skills/ttl/SKILL.md CONVENTIONS.md
```

Aider auto-includes `CONVENTIONS.md` from the repo root in every chat.

### Any agent that doesn't have a skill system

If the agent reads `README.md` or a free-form context file, append the
skill content:

```bash
# README.md
echo "" >> README.md
echo "## Agent instructions" >> README.md
echo "" >> README.md
cat skills/ttl/SKILL.md >> README.md
```

Or point the agent at the file directly with a one-shot prompt:
"Read `skills/ttl/SKILL.md` before answering any task-tracking question."

## Verifying it works

After installing, ask the agent something only the skill could answer:

- "Use the ttl MCP server to add a task called 'Test from <agent>'."
- "Show me today's worklog using ttl."
- "Mark the most recent ttl task as done."

If the agent knows about `ttl mcp`, the date conventions (Unix ms),
and the priority encoding (0–3), the skill is loaded.

## Updating the skill

The skill lives at `skills/ttl/SKILL.md` in the repo. After editing:

```bash
./scripts/install-skill.sh all       # push to all installed locations
shasum -a 256 skills/ttl/SKILL.md   # update skills-lock.json
```

The factory/droid loader will re-verify the hash on next load.

## Uninstalling

```bash
./scripts/install-skill.sh --uninstall
```

Removes the skill from every supported location.
