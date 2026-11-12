# Needs only a Go toolchain: go.mod pins Go (GOTOOLCHAIN=auto fetches it) and
# the linters (`tool` directives), so nothing is installed globally.
# `check` also runs the UI check, which needs Bun and `ui/node_modules`
# (`cd ui && bun install --frozen-lockfile`, or `make ui`).
GO ?= go
GOFMT := $(shell $(GO) env GOROOT)/bin/gofmt
BUN ?= bun
export CGO_ENABLED := 0

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/r3p-dev/dermaga/internal/name.VersionOverride=$(VERSION)

.PHONY: check build tygo tygo-check ui ui-check

check: tygo-check ui-check
	@out=$$($(GOFMT) -l cmd internal ui/embed.go); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	$(GO) tool staticcheck ./...
	$(GO) tool govulncheck ./...
	$(GO) test ./...

tygo:
	$(GO) tool tygo generate

tygo-check:
	$(GO) tool tygo generate
	@git diff --exit-code ui/src/lib/types || (echo "error: generated TypeScript types are out of date; run 'make tygo'"; exit 1)

ui:
	cd ui && $(BUN) install --frozen-lockfile && $(BUN) run build

ui-check:
	cd ui && $(BUN) run check

build:
	GOARCH=amd64 $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o bin/dermaga-linux-amd64 ./cmd/dermaga
	GOARCH=arm64 $(GO) build -trimpath -ldflags='$(LDFLAGS)' -o bin/dermaga-linux-arm64 ./cmd/dermaga
