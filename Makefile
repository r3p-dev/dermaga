# Needs only a Go toolchain: go.mod pins Go (GOTOOLCHAIN=auto fetches it) and
# the linters (`tool` directives), so nothing is installed globally.
GO ?= go
GOFMT := $(shell $(GO) env GOROOT)/bin/gofmt
BUN ?= bun
export CGO_ENABLED := 0

.PHONY: check build tygo tygo-check ui ui-check

check: tygo-check
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
	cd ui && $(BUN) install && $(BUN) run build

ui-check:
	cd ui && $(BUN) run check

build:
	GOARCH=amd64 $(GO) build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-amd64 ./cmd/dermaga
	GOARCH=arm64 $(GO) build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-arm64 ./cmd/dermaga
