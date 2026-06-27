#!/usr/bin/env bash
# scripts/install.sh — install ttl without cloning the repo.
#
# Downloads a release binary for the current OS/arch, verifies its
# SHA256, and installs to a directory on PATH.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/<owner>/ttl/main/scripts/install.sh | bash
#   curl -sSL ... | bash -s -- --version v0.4.1 --to ~/.local/bin
#
# Flags:
#   --version <tag>     Release tag to install (default: latest)
#   --to <dir>           Install directory (default: /usr/local/bin, or ~/.local/bin if no sudo)
#   --repo <owner/repo>  GitHub repo slug (default: anirudh-777/ttl)
#   --print-latest       Just print the latest release tag and exit
#   --print-checksum     Print the expected SHA256 for the current platform and exit
#   --uninstall          Remove ttl from --to
#
# Env:
#   TTL_VERSION, TTL_REPO, TTL_INSTALL_DIR — same as the flags

set -euo pipefail

REPO="${TTL_REPO:-anirudh-777/ttl}"
VERSION="${TTL_VERSION:-latest}"
INSTALL_DIR="${TTL_INSTALL_DIR:-}"
PRINT_LATEST=0
PRINT_CHECKSUM=0
UNINSTALL=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)    VERSION="${2:-}"; shift 2 ;;
    --to)         INSTALL_DIR="${2:-}"; shift 2 ;;
    --repo)       REPO="${2:-}"; shift 2 ;;
    --print-latest)   PRINT_LATEST=1; shift ;;
    --print-checksum) PRINT_CHECKSUM=1; shift ;;
    --uninstall)  UNINSTALL=1; shift ;;
    -h|--help)
      sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done

# ---------- OS / arch detection ----------
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    MINGW*|MSYS*|CYGWIN*) os="windows" ;;
    *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
  echo "${os}-${arch}"
}

PLATFORM="$(detect_platform)"
OS="${PLATFORM%-*}"
ARCH="${PLATFORM#*-}"
EXT=""
if [[ "$OS" == "windows" ]]; then EXT=".exe"; fi

# ---------- helpers ----------
die() { echo "ttl-install: $*" >&2; exit 1; }

have_cmd() { command -v "$1" >/dev/null 2>&1; }

# Pick an install dir: /usr/local/bin if writable, else ~/.local/bin.
pick_install_dir() {
  if [[ -n "$INSTALL_DIR" ]]; then echo "$INSTALL_DIR"; return; fi
  if [[ -w "/usr/local/bin" ]]; then echo "/usr/local/bin"; return; fi
  if have_cmd sudo && sudo -n true 2>/dev/null; then
    echo "/usr/local/bin"; return
  fi
  echo "$HOME/.local/bin"
}

# Fetch a URL to stdout. Honours $TMPDIR for download caches.
fetch() {
  local url="$1"
  if have_cmd curl; then
    curl -fsSL --retry 3 "$url"
  elif have_cmd wget; then
    wget -qO- --tries=3 "$url"
  else
    die "need curl or wget"
  fi
}

# Resolve "latest" -> actual tag via GitHub API (no auth, public).
resolve_latest() {
  local tag
  tag=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep -m1 '"tag_name":' \
        | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [[ -n "$tag" ]] || die "could not resolve latest release for ${REPO}"
  echo "$tag"
}

# Get the SHA256SUMS file from a release and extract the checksum for our binary.
checksum_for() {
  local tag="$1" platform="$2"
  local sums
  sums=$(fetch "https://github.com/${REPO}/releases/download/${tag}/SHA256SUMS") \
    || die "could not fetch SHA256SUMS from ${tag}"
  # The release artefacts are named ttl-<os>-<arch>[.exe].
  local want="ttl-${platform}${EXT}"
  echo "$sums" | awk -v want="$want" '$2 == want { print $1; exit }'
}

# ---------- uninstall ----------
if [[ $UNINSTALL -eq 1 ]]; then
  DIR="$(pick_install_dir)"
  BIN="$DIR/ttl${EXT}"
  if [[ -f "$BIN" ]]; then
    rm "$BIN"
    echo "removed $BIN"
  else
    echo "ttl not found at $BIN" >&2
    exit 1
  fi
  exit 0
fi

# ---------- read-only modes ----------
if [[ $PRINT_LATEST -eq 1 ]]; then
  resolve_latest
  exit 0
fi
if [[ $PRINT_CHECKSUM -eq 1 ]]; then
  if [[ "$VERSION" == "latest" ]]; then VERSION="$(resolve_latest)"; fi
  s="$(checksum_for "$VERSION" "$PLATFORM")"
  [[ -n "$s" ]] || die "no checksum for ttl-${PLATFORM}${EXT} in ${VERSION}"
  echo "$s"
  exit 0
fi

# ---------- install ----------
if [[ "$VERSION" == "latest" ]]; then VERSION="$(resolve_latest)"; fi
URL="https://github.com/${REPO}/releases/download/${VERSION}/ttl-${PLATFORM}${EXT}"
EXPECTED="$(checksum_for "$VERSION" "$PLATFORM" || true)"
[[ -n "$EXPECTED" ]] || die "no checksum for ttl-${PLATFORM}${EXT} in ${VERSION} (release ${VERSION} may be incomplete)"

INSTALL_DIR="$(pick_install_dir)"
mkdir -p "$INSTALL_DIR"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
DEST="$INSTALL_DIR/ttl${EXT}"

echo "ttl $VERSION ($PLATFORM)"
echo "  downloading: $URL"
fetch "$URL" > "$TMP/ttl"

ACTUAL="$(shasum -a 256 "$TMP/ttl" 2>/dev/null | cut -d' ' -f1 \
         || sha256sum "$TMP/ttl" | cut -d' ' -f1)"
[[ "$ACTUAL" == "$EXPECTED" ]] || die "checksum mismatch: expected $EXPECTED got $ACTUAL"

# Atomic move + chmod. If install dir requires sudo, prompt once.
if [[ -w "$INSTALL_DIR" ]]; then
  mv "$TMP/ttl" "$DEST"
  chmod 0755 "$DEST"
else
  echo "  $INSTALL_DIR requires elevated permissions; using sudo"
  sudo mv "$TMP/ttl" "$DEST"
  sudo chmod 0755 "$DEST"
fi

echo "  installed: $DEST"
echo ""
echo "verify:"
echo "  $DEST version"
echo ""
echo "next steps:"
echo "  $DEST serve                      # start the server (default :8093)"
echo "  $DEST signup --server http://localhost:8093   # create a workspace"
echo "  $DEST login  --server http://localhost:8093   # or sign in"
