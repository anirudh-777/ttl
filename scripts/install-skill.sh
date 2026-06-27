#!/usr/bin/env bash
# install-skill.sh — install or remove the ttl skill in coding agents.
#
# Usage:
#   scripts/install-skill.sh              # interactive picker
#   scripts/install-skill.sh all          # install for every supported agent
#   scripts/install-skill.sh claude       # install for one named agent
#   scripts/install-skill.sh --uninstall  # remove from all supported locations
#   scripts/install-skill.sh --list       # show where the skill is currently installed
#
# The skill is the single file skills/ttl/SKILL.md. Each agent reads from a
# different well-known path; this script copies it there.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SKILL_SRC="$REPO_ROOT/skills/ttl/SKILL.md"

# The factory/droid target IS the repo source. We never remove it on
# --uninstall (it's the source of truth), and we never copy it on
# install (it's already there).
SOURCE_SKILL_PATH="$REPO_ROOT/skills/ttl/SKILL.md"

# Format: "agent|install_path|description"
TARGETS=(
  "claude|$HOME/.claude/skills/ttl/SKILL.md|Claude Code (user-scoped)"
  "claude-project|$REPO_ROOT/.claude/skills/ttl/SKILL.md|Claude Code (project-scoped)"
  "cursor|$HOME/.cursor/rules/ttl.md|Cursor (user)"
  "cursor-project|$REPO_ROOT/.cursor/rules/ttl.md|Cursor (project)"
  "copilot|$REPO_ROOT/.github/copilot-instructions.md|GitHub Copilot (workspace)"
  "continue|$REPO_ROOT/.continue/rules/ttl.md|Continue.dev (project)"
  "continue-user|$HOME/.continue/rules/ttl.md|Continue.dev (user)"
  "windsurf|$REPO_ROOT/.windsurfrules|Windsurf (project)"
  "cline|$REPO_ROOT/.clinerules/ttl.md|Cline / Roo-Cline"
  "roo|$REPO_ROOT/.roo/rules/ttl.md|Roo Code"
  "aider|$REPO_ROOT/CONVENTIONS.md|Aider"
  "factory|$REPO_ROOT/skills/ttl/SKILL.md|Factory / Droid (source — kept in sync via git)"
)

# Codex is special — it wants AGENTS.md, not a raw skill file.
CODEX_TARGETS=(
  "$REPO_ROOT/AGENTS.md|OpenAI Codex (project)"
  "$HOME/AGENTS.md|OpenAI Codex (user)"
)

# Universal fallback: append to README.md.
README_TARGETS=(
  "$REPO_ROOT/README.md|README.md (universal fallback)"
)

usage() {
  sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
}

list_installed() {
  echo "ttl skill install paths:"
  echo ""
  found=0
  for entry in "${TARGETS[@]}" "${CODEX_TARGETS[@]}" "${README_TARGETS[@]}"; do
    IFS='|' read -r name path desc <<< "$entry"
    if [[ -f "$path" ]]; then
      echo "  [installed] $desc"
      echo "               $path"
      found=$((found+1))
    fi
  done
  if [[ $found -eq 0 ]]; then
    echo "  (not installed anywhere yet)"
  fi
}

install_to() {
  local entry="$1"
  IFS='|' read -r name path desc <<< "$entry"
  # The factory/droid target IS the repo source — don't copy onto itself.
  if [[ "$path" == "$SOURCE_SKILL_PATH" ]]; then
    echo "  source:  $desc"
    echo "           $path"
    return
  fi
  if [[ "$path" == *.md && "$(basename "$path")" == "AGENTS.md" ]]; then
    install_codex "$path" "$desc"
    return
  fi
  if [[ "$desc" == *"universal fallback"* ]]; then
    install_readme "$path" "$desc"
    return
  fi
  mkdir -p "$(dirname "$path")"
  cp "$SKILL_SRC" "$path"
  echo "  installed: $desc"
  echo "             $path"
}

install_codex() {
  local path="$1" desc="$2"
  mkdir -p "$(dirname "$path")"
  # Codex wants AGENTS.md with a thin wrapper around the skill.
  if [[ -f "$path" ]]; then
    echo "  skip (already exists): $path"
    return
  fi
  cat > "$path" <<EOF
# Agent instructions

This project uses **ttl** for task tracking. Read
\`skills/ttl/SKILL.md\` before doing any task-tracking work.

---
EOF
  cat "$SKILL_SRC" >> "$path"
  echo "  installed: $desc"
  echo "             $path"
}

install_readme() {
  local path="$1" desc="$2"
  if ! [[ -f "$path" ]]; then
    echo "  skip (no README.md to extend): $path"
    return
  fi
  if grep -q "skills/ttl/SKILL.md" "$path"; then
    echo "  skip (already references skill): $path"
    return
  fi
  cat >> "$path" <<EOF

---

## Agent instructions

This project uses **ttl** for task tracking. Read
\`skills/ttl/SKILL.md\` before doing any task-tracking work.

---
EOF
  cat "$SKILL_SRC" >> "$path"
  echo "  installed: $desc"
  echo "             $path"
}

uninstall() {
  echo "Removing ttl skill from every known location..."
  for entry in "${TARGETS[@]}" "${CODEX_TARGETS[@]}" "${README_TARGETS[@]}"; do
    IFS='|' read -r name path desc <<< "$entry"
    # Never remove the repo source.
    if [[ "$path" == "$SOURCE_SKILL_PATH" ]]; then
      continue
    fi
    if [[ -f "$path" ]]; then
      # Only remove if the file contains our skill's signature.
      if grep -q "ttl — terminal-first\|ttl -- Terminal-first\|TTL Skill\|ttl — terminal task tracker" "$path" 2>/dev/null; then
        rm "$path"
        echo "  removed: $desc ($path)"
      else
        echo "  skip (file does not look like ttl skill): $path"
      fi
    fi
  done
}

interactive_pick() {
  echo "Pick an agent to install for:"
  echo ""
  i=1
  for entry in "${TARGETS[@]}"; do
    IFS='|' read -r name path desc <<< "$entry"
    printf "  %2d) %s\n" "$i" "$desc"
    i=$((i+1))
  done
  echo ""
  echo "  a) install for ALL"
  echo "  q) quit"
  echo ""
  read -r -p "Choice: " choice
  case "$choice" in
    q|Q) exit 0 ;;
    a|A) install_all ;;
    *) if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= ${#TARGETS[@]} )); then
         install_to "${TARGETS[$((choice-1))]}"
       else
         echo "invalid choice"; exit 1
       fi ;;
  esac
}

install_all() {
  for entry in "${TARGETS[@]}"; do
    install_to "$entry"
  done
  for path_desc in "${CODEX_TARGETS[@]}" "${README_TARGETS[@]}"; do
    install_to "$path_desc"
  done
}

case "${1:-}" in
  "") interactive_pick ;;
  --help|-h) usage ;;
  --list|list) list_installed ;;
  --uninstall|uninstall) uninstall ;;
  all) install_all ;;
  *) install_to "$(printf '%s\n' "${TARGETS[@]}" "${CODEX_TARGETS[@]}" "${README_TARGETS[@]}" | grep -E "^${1}|[|]")" || {
       echo "unknown agent: $1"
       echo "try: $0 --list"
       exit 1
     } ;;
esac
