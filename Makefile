# ttl — Makefile for local dev + cross-platform builds.
#
# Common targets:
#   make build              -> build for the host OS
#   make test               -> run the test suite
#   make build-all          -> cross-compile for linux/darwin/windows
#   make docker             -> build the distroless container image
#   make clean              -> remove build artifacts
#   make precommit          -> install + run pre-commit hooks (gitleaks, go-fmt, ...)
#
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# Stamp the version into the symbols consumed by serve.go's banner and
# /version endpoint.
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/ttl ./cmd/ttl

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: build-all
build-all:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ttl-linux-amd64   ./cmd/ttl
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ttl-linux-arm64   ./cmd/ttl
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ttl-darwin-amd64  ./cmd/ttl
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ttl-darwin-arm64  ./cmd/ttl
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ttl-windows-amd64.exe ./cmd/ttl

.PHONY: docker
docker:
	docker build -f deploy/docker/Dockerfile -t ttl:$(VERSION) .

.PHONY: clean
clean:
	rm -rf bin/ dist/

.PHONY: run-server
run-server: build
	./bin/ttl serve --addr :8093

# Install the freshly-built binary onto PATH.
# Honours PREFIX (default /usr/local). For a user-local install:
#   make install PREFIX=$HOME/.local
.PHONY: install
install: build
	install -m 0755 bin/ttl $(PREFIX)/bin/ttl

.PHONY: uninstall
uninstall:
	rm -f $(PREFIX)/bin/ttl

.PHONY: precommit-install
precommit-install:
	@command -v pre-commit >/dev/null 2>&1 || { \
		echo "pre-commit not found. Install with: python3 -m pip install --user pre-commit"; \
		exit 1; \
	}
	pre-commit install
	pre-commit run --all-files

.PHONY: precommit
precommit:
	pre-commit run --all-files

.PHONY: secrets-scan
secrets-scan:
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo "gitleaks not found. Install with: brew install gitleaks"; \
		exit 1; \
	}
	gitleaks protect --staged --redact --no-banner
	gitleaks detect --source . --redact --no-banner
