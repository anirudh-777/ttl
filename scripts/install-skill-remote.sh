#!/usr/bin/env bash
# scripts/install-skill-remote.sh — fetch the ttl skill into a coding agent
# without cloning the repo.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/<owner>/ttl/main/skills/ttl/SKILL.md \
#     | scripts/install-skill.sh -
#
# Or directly:
#   bash <(curl -sSL https://raw.githubusercontent.com/<owner>/ttl/main/scripts/install-skill-remote.sh) all
#
# This is a thin wrapper that downloads skills/ttl/SKILL.md from a
# known URL into a temp file and then re-execs install-skill.sh with
# that temp file as SKILL_SRC.

set -euo pipefail

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

REPO="${TTL_REPO:-anirudh-777/ttl}"
BRANCH="${TTL_BRANCH:-main}"
RAW="https://raw.githubusercontent.com/${REPO}/${BRANCH}/skills/ttl/SKILL.md"

have_cmd curl || { echo "need curl" >&2; exit 1; }

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
