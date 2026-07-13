#!/usr/bin/env bash
# scripts/install-skill-remote.sh — fetch the ttl skill into a coding agent
# without cloning the repo.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/<owner>/ttl/main/scripts/install-skill-remote.sh \
#     | bash -s -- all
#   ... | bash -s -- claude
#   ... | bash -s -- --repo my-fork/ttl all
#
# Downloads skills/ttl/SKILL.md from the repo and re-execs
# scripts/install-skill.sh with it.

set -euo pipefail

have_cmd() { command -v "$1" >/dev/null 2>&1; }

REPO="${TTL_REPO:-anirudh-777/ttl}"
BRANCH="${TTL_BRANCH:-main}"
RAW="https://raw.githubusercontent.com/${REPO}/${BRANCH}/skills/ttl/SKILL.md"

have_cmd curl || { echo "need curl" >&2; exit 1; }

# v1.0.1+ embeds the skill and owns agent configuration directly.
if have_cmd ttl && ttl agents --help >/dev/null 2>&1; then
  case "${1:-}" in
    ""|all) exec ttl agents install --all ;;
    --uninstall|uninstall) exec ttl agents uninstall ;;
    --list|list) exec ttl agents status ;;
    *) exec ttl agents install --agent "$1" ;;
  esac
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SKILL_PATH="$TMP/SKILL.md"

echo "downloading $RAW"
curl -fsSL --retry 3 "$RAW" > "$SKILL_PATH"
[[ -s "$SKILL_PATH" ]] || { echo "empty skill file" >&2; exit 1; }

# Locate install-skill.sh in the same repo. We don't have it locally,
# so fetch it too.
SCRIPT_PATH="$TMP/install-skill.sh"
curl -fsSL --retry 3 \
  "https://raw.githubusercontent.com/${REPO}/${BRANCH}/scripts/install-skill.sh" \
  > "$SCRIPT_PATH"
chmod +x "$SCRIPT_PATH"

# Patch SKILL_SRC in a copy so it points at our downloaded file.
cp "$SCRIPT_PATH" "$SCRIPT_PATH.run"
sed -i '' "s|^SKILL_SRC=.*|SKILL_SRC=\"$SKILL_PATH\"|" "$SCRIPT_PATH.run" 2>/dev/null \
  || sed -i "s|^SKILL_SRC=.*|SKILL_SRC=\"$SKILL_PATH\"|" "$SCRIPT_PATH.run"

exec "$SCRIPT_PATH.run" "$@"
