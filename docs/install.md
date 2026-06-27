# Installing ttl without cloning the repo

Three paths, pick the one that matches your environment:

| Path | Best for |
|---|---|
| **`curl \| bash` script** | One-line install. Works on macOS, Linux, Windows (WSL). |
| **`go install`** | Anyone with a Go toolchain. |
| **Homebrew tap** | macOS / Linux users who prefer system package managers. |

All three install only the **binary** (~13 MB) — no git, no Go, no
Docker required to run ttl.

## 1. `curl | bash` (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/anirudh-777/ttl/main/scripts/install.sh | bash
```

What this does:

1. Detects your OS (`darwin` / `linux`) and CPU (`amd64` / `arm64`)
2. Resolves the latest GitHub release tag via the API
3. Downloads the matching binary + `SHA256SUMS`
4. Verifies the SHA256
5. Moves the binary to `/usr/local/bin/ttl` (or `~/.local/bin/ttl`
   if you don't have write access there)
6. Prints verification + next-steps

### Flags

```bash
# Pin a specific version
curl -sSL .../install.sh | bash -s -- --version v0.4.1

# User-local install (no sudo)
curl -sSL .../install.sh | bash -s -- --to ~/.local/bin

# Custom repo (fork / mirror)
TTL_REPO=my-org/ttl curl -sSL .../install.sh | bash

# Uninstall
curl -sSL .../install.sh | bash -s -- --uninstall

# Inspect what would happen (no download)
curl -sSL .../install.sh | bash -s -- --print-latest
curl -sSL .../install.sh | bash -s -- --print-checksum
```

### Verify what you got

```bash
ttl version
# ttl 0.4.1 (commit ..., built ...)

# and the server's built-in version endpoint
ttl serve &
curl -s http://localhost:8093/version
# {"version":"0.4.1","built":"...","go":"go1.25.0"}
```

## 2. `go install`

If you have Go 1.22+:

```bash
go install github.com/anirudh-777/ttl/cmd/ttl@latest
# or pin:
go install github.com/anirudh-777/ttl/cmd/ttl@v0.4.1
```

The binary lands in `$GOBIN` (default `~/go/bin`). Make sure that's
on your `PATH`.

## 3. Homebrew

Once a tap is published:

```bash
brew install anirudh-777/tap/ttl
```

Until then, you can install directly from the formula in the repo:

```bash
brew install --build-from-source deploy/homebrew/ttl.rb
```

The formula is set up to download release binaries (not build from
source), so the install is fast.

## 4. Docker

```bash
docker run --rm -it \
  -p 8093:8093 \
  -v ~/.local/share/ttl:/data \
  ghcr.io/anirudh-777/ttl:latest
```

The image is ~20 MB (distroless). Web UI at `http://localhost:8093/`.

## Installing the agent skill (no binary needed)

The skill is a single Markdown file that AI coding agents load for
context. Install it separately:

```bash
curl -sSL https://raw.githubusercontent.com/anirudh-777/ttl/main/scripts/install-skill-remote.sh \
  | bash -s -- all
```

Or pick one agent:

```bash
curl -sSL .../install-skill-remote.sh | bash -s -- claude
curl -sSL .../install-skill-remote.sh | bash -s -- cursor
curl -sSL .../install-skill-remote.sh | bash -s -- copilot
curl -sSL .../install-skill-remote.sh | bash -s -- cline
```

Supports: Claude Code, Cursor, GitHub Copilot, Continue.dev,
Windsurf, Cline / Roo-Cline, Aider, OpenAI Codex, plus a README.md
fallback for agents with no skill system.

See `docs/agents.md` for the full per-agent guide.

## Updating

```bash
# binary
curl -sSL .../install.sh | bash         # re-run, picks up latest

# skill (overwrites every installed copy)
curl -sSL .../install-skill-remote.sh | bash -s -- all
```

## Uninstalling

```bash
# binary
curl -sSL .../install.sh | bash -s -- --uninstall

# skill (uses the local install-skill.sh if available)
curl -sSL .../install-skill-remote.sh | bash -s -- --uninstall
# or directly, if you already cloned:
./scripts/install-skill.sh --uninstall
```

## Verifying releases

Every release artefact is checksummed. `SHA256SUMS` is published
alongside the binaries. `install.sh` verifies before installing.

To verify a binary you already downloaded:

```bash
curl -sSL https://github.com/anirudh-777/ttl/releases/latest/download/SHA256SUMS \
  | grep ttl-darwin-arm64
shasum -a 256 ttl-darwin-arm64
# the two hashes must match
```

## Air-gapped / offline installs

If your machine can't reach GitHub:

1. Download on a connected machine:
   ```bash
   curl -sSL .../install.sh | bash -s -- --to ./ttl-bin
   cp ./ttl-bin/ttl /media/usb/
   cp SHA256SUMS /media/usb/
   ```

2. On the air-gapped machine:
   ```bash
   cp /media/usb/ttl /usr/local/bin/
   cp /media/usb/SHA256SUMS /tmp/
   cd /usr/local/bin && shasum -a 256 ttl
   # compare to the entry in /tmp/SHA256SUMS
   ```
