VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS = -s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)

.PHONY: all build build-all ui tygo test test-browser lint vulncheck check clean

all: check

# 1. Generate TypeScript API types from Go structs
tygo:
	go tool tygo generate

# 2. Build SvelteKit static UI and copy to embed dist directory
ui: tygo
	cd ui && bun install && bun run build
	rm -rf internal/server/dist
	cp -r ui/build internal/server/dist

# 3. Build static binary for host architecture
build: ui
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/dermaga ./cmd/dermaga

# Build static binaries for both amd64 and arm64
build-all: ui
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/dermaga-linux-amd64 ./cmd/dermaga
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/dermaga-linux-arm64 ./cmd/dermaga

# 4. Run Go tests
test:
	go test -v ./...

# Run browser tests
test-browser:
	cd ui && bunx playwright test

# 5. Format and Lint checks
lint:
	test -z "$$(gofmt -s -l .)"
	go vet ./...
	go tool staticcheck ./...
	cd ui && bun run check
	cd ui && bun run format:check

# 6. Vulnerability scanning
vulncheck:
	go tool govulncheck ./...
	cd ui && bun audit || echo "WARNING: bun audit found issues (see docs/DEPENDENCIES.md §6)"

# 7. Complete check target required by PLAN.md
check: lint test vulncheck build-all
	@echo "=== make check passed successfully ==="

clean:
	rm -rf bin ui/build internal/server/dist coverage.out
