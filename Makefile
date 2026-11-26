VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS = -s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)

.PHONY: all build build-all ui tygo test test-browser lint vulncheck policy shellcheck check release clean

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

# 4. Run Go tests (VERBOSE=1 lists every test)
test: ui
	go test $(if $(filter 1,$(VERBOSE)),-v) ./...

# Run browser tests
test-browser: build
	cd ui && bunx playwright test

# 5. Format and Lint checks
lint: ui
	@files=$$(gofmt -s -l .); if [ -n "$$files" ]; then echo "gofmt -s would change:"; echo "$$files"; exit 1; fi
	go vet ./...
	go tool staticcheck ./...
	cd ui && bun run check
	cd ui && bun run format:check

# 6. Vulnerability scanning
vulncheck: ui
	go tool govulncheck ./...
	cd ui && bun run audit:check

# 7. Repository policy: images pinned by digest, no secrets, build output or raw evidence tracked
policy:
	scripts/check-policy

# 8. shellcheck on every shell script. CI installs it and sets SHELLCHECK_REQUIRED=1 (DEC-27);
# locally a missing tool is skipped, never a failure.
SHELL_SCRIPTS = $(shell find . \( -path ./.git -o -path ./ui/node_modules -o -path ./tmp \) -prune -o \
	-type f \( -name '*.sh' -o -path './scripts/*' \) -print | sort)
shellcheck:
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck $(SHELL_SCRIPTS) && echo "shellcheck: $(words $(SHELL_SCRIPTS)) scripts clean"; \
	elif [ "$(SHELLCHECK_REQUIRED)" = 1 ]; then \
		echo "shellcheck is required here but not installed"; exit 1; \
	else \
		echo "SKIP shellcheck (not installed)"; \
	fi

# 9. Complete check target required by PLAN.md. Quiet by default: one line per step, the full
# output in .check-logs/<step>.log and its tail on failure. VERBOSE=1 (CI) prints everything.
ifeq ($(VERBOSE),1)
check: lint test vulncheck build-all policy shellcheck
	@echo "=== make check passed successfully ==="
else
CHECK_STEPS = ui lint test vulncheck build-all policy shellcheck
check:
	@mkdir -p .check-logs
	@for step in $(CHECK_STEPS); do \
		skip=; [ $$step = ui ] || skip="-o ui"; \
		if $(MAKE) --no-print-directory $$skip $$step >.check-logs/$$step.log 2>&1; then \
			grep -m 1 '^SKIP ' .check-logs/$$step.log || echo "ok   $$step"; \
		else \
			echo "FAIL $$step (full log: .check-logs/$$step.log)"; tail -n 40 .check-logs/$$step.log; exit 1; \
		fi; \
	done
	@echo "=== make check passed successfully ==="
endif

# 10. Release files in dist/: both binaries, install.sh stamped with the version, checksums.txt.
# CI runs this on every push and the release workflow on a tag, both as user server, so a tag
# never meets a step that has not run before. RELEASE_TAG must equal the git describe version.
release: build-all
	@case "$(VERSION)" in *-dirty|dev) echo "refusing to assemble a release from version $(VERSION)"; exit 1;; esac
	@if [ -n "$(RELEASE_TAG)" ] && [ "$(RELEASE_TAG)" != "$(VERSION)" ]; then echo "tag $(RELEASE_TAG) but the build version is $(VERSION)"; exit 1; fi
	rm -rf dist
	mkdir dist
	cp bin/dermaga-linux-amd64 bin/dermaga-linux-arm64 LICENSE dist/
	test "$$(grep -c '__DERMAGA_VERSION__' install/install.sh)" = 1
	sed 's/__DERMAGA_VERSION__/$(VERSION)/' install/install.sh > dist/install.sh
	grep -qx 'readonly DERMAGA_VERSION="$(VERSION)"' dist/install.sh
	dist/dermaga-linux-amd64 version | grep -qxF "dermaga version $(VERSION) (commit $(COMMIT))"
	cd dist && sha256sum dermaga-linux-amd64 dermaga-linux-arm64 install.sh LICENSE > checksums.txt
	cat dist/checksums.txt

clean:
	rm -rf bin dist ui/build internal/server/dist coverage.out
