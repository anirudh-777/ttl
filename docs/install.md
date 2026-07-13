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
curl -sSL .../install.sh | bash -s -- --version v1.0.0

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
# ttl v1.0.0 (commit ..., built ...)

# and the server's built-in version endpoint
ttl serve &
curl -s http://localhost:8093/version
# {"version":"v1.0.0","built":"...","go":"go1.25.0"}
```

## 2. `go install`

If you have Go 1.25+:

```bash
go install github.com/anirudh-777/ttl/cmd/ttl@latest
# or pin:
go install github.com/anirudh-777/ttl/cmd/ttl@v1.0.0
```

The binary lands in `$GOBIN` (default `~/go/bin`). Make sure that's
on your `PATH`.

## 3. Homebrew (planned)

Once a tap is published:

```bash
brew install anirudh-777/tap/ttl
```

The repository includes a formula template under `deploy/homebrew/`, but it is
not installable until release checksums are substituted and the tap is published.

## 4. Docker

```bash
git clone https://github.com/anirudh-777/ttl && cd ttl
make docker
docker run --rm -it \
  -p 8093:8093 \
  -v ~/.local/share/ttl:/data \
  ttl:dev
```

The image is ~20 MB (distroless). Web UI at `http://localhost:8093/`.

## Installing coding-agent support

After logging into a ttl server, install the embedded skill and register the
local MCP server in detected coding agents:

```bash
ttl login --server https://tasks.example.com
ttl agents install
```

Or select targets explicitly:

```bash
ttl agents install --agent claude
ttl agents install --agent codex --agent cursor
ttl agents install --all
```

Use `--skills-only` to avoid MCP changes, or `--dry-run` to preview them.
Installs are user-scoped and preserve unrelated files and MCP entries.

See `docs/agents.md` for the full per-agent guide.

## Updating

```bash
# binary
curl -sSL .../install.sh | bash         # re-run, picks up latest

# coding-agent support
ttl agents update
```

## Uninstalling

```bash
# binary
curl -sSL .../install.sh | bash -s -- --uninstall

# coding-agent support (only ttl-owned resources)
ttl agents uninstall
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
