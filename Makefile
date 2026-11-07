# Needs only a Go toolchain: go.mod pins Go (GOTOOLCHAIN=auto fetches it) and
# the linters (`tool` directives), so nothing is installed globally.
GO ?= go
GOFMT := $(shell $(GO) env GOROOT)/bin/gofmt
export CGO_ENABLED := 0

.PHONY: check build

check:
	@out=$$($(GOFMT) -l cmd internal); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	$(GO) tool staticcheck ./...
	$(GO) tool govulncheck ./...
	$(GO) test ./...

build:
	GOARCH=amd64 $(GO) build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-amd64 ./cmd/dermaga
	GOARCH=arm64 $(GO) build -trimpath -ldflags='-s -w' -o bin/dermaga-linux-arm64 ./cmd/dermaga
