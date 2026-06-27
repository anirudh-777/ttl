#!/usr/bin/env bash
# scripts/build.sh — release helper.
#
# Cross-compiles ttl for all supported platforms and emits a SHA256
# checksums file. Outputs go to dist/.

set -euo pipefail

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
COMMIT=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo none)}
DATE=${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

mkdir -p dist
echo "building ttl ${VERSION} (${COMMIT})"

PLATFORMS=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

for p in "${PLATFORMS[@]}"; do
  os=$(echo "$p" | cut -d' ' -f1)
  arch=$(echo "$p" | cut -d' ' -f2)
  ext=""
  if [ "$os" = "windows" ]; then ext=".exe"; fi
  out="dist/ttl-${os}-${arch}${ext}"
  echo "  -> ${out}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$out" ./cmd/ttl
done

( cd dist && shasum -a 256 ttl-* > SHA256SUMS ) 2>/dev/null \
  || ( cd dist && sha256sum ttl-* > SHA256SUMS )

echo "done. artefacts in dist/:"
ls -la dist/
