# Makefile — cenvkit / cenvkit
#
# Default goal: help (auto-generated from ## comments).
# Portability: POSIX sh recipes; portable across macOS (BSD) and Linux.
# Requires: GNU make, Go toolchain on PATH.

.DEFAULT_GOAL := help

# ──────────────────────────────────────────────────────────────────────────────
# Variables
# ──────────────────────────────────────────────────────────────────────────────

BINARY  := cenvkit
PKG     := ./cmd/cenvkit
MODULE  := $(shell go list -m)

# Resolve the install target directory.
# `go env GOBIN` is preferred; if empty, fall back to $(GOPATH)/bin.
_GOBIN  := $(shell go env GOBIN)
ifeq ($(_GOBIN),)
_GOBIN  := $(shell go env GOPATH)/bin
endif
GOBIN   := $(_GOBIN)

# Local dev binary (gitignored; used by dev-build / demo / clean).
DEV_BIN := .cenvkit.bin

# Optional PREFIX override: `make install PREFIX=/usr/local`
# When PREFIX is set the binary is built then copied; the directory must be
# writable (cenvkit never invokes sudo).
PREFIX  :=

# Version stamping for non-release builds: `git describe` (tag + commits-since +
# short-hash, "-dirty" if the tree is modified), falling back to "dev". goreleaser
# injects its own clean version at release time, so this only affects local
# `make dev-build`/`install`. Override with `make install VERSION=v1.2.3`.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# ──────────────────────────────────────────────────────────────────────────────
# Phony declarations (every target)
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: help build dev-build install uninstall \
        test test-nodocker test-acceptance test-race \
        vet fmt fmt-check lint tidy seam ci demo run clean

# ──────────────────────────────────────────────────────────────────────────────
# Help (default)
# ──────────────────────────────────────────────────────────────────────────────

help: ## List available targets with one-line descriptions
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) \
	  | sed 's/:.*## /\t/' \
	  | awk -F '\t' '{ printf "  %-18s %s\n", $$1, $$2 }'

# ──────────────────────────────────────────────────────────────────────────────
# Build
# ──────────────────────────────────────────────────────────────────────────────

build: ## Compile-check all packages (go build ./...)
	go build ./...

dev-build: ## Build a fast local binary → $(DEV_BIN) (gitignored)
	go build -ldflags "$(LDFLAGS)" -o $(DEV_BIN) $(PKG)
	@echo "Local binary: $(DEV_BIN) ($(VERSION))"

# ──────────────────────────────────────────────────────────────────────────────
# Install / uninstall
# ──────────────────────────────────────────────────────────────────────────────

install: ## Install cenvkit from the LOCAL repo into $(GOBIN) (or PREFIX/bin via go install)
ifdef PREFIX
	@echo "Installing $(BINARY) $(VERSION) into $(PREFIX)/bin via go install …"
	mkdir -p $(abspath $(PREFIX)/bin)
	GOBIN=$(abspath $(PREFIX)/bin) go install -ldflags "$(LDFLAGS)" $(PKG)
	@echo "Installed: $(abspath $(PREFIX)/bin)/$(BINARY)"
	@echo "NOTE: $(PREFIX)/bin must be writable; cenvkit never uses sudo."
else
	go install -ldflags "$(LDFLAGS)" $(PKG)
	@echo "Installed: $(GOBIN)/$(BINARY) ($(VERSION))"
endif

uninstall: ## Remove the installed cenvkit binary
ifdef PREFIX
	rm -f $(PREFIX)/bin/$(BINARY)
	@echo "Removed: $(PREFIX)/bin/$(BINARY)"
else
	rm -f $(GOBIN)/$(BINARY)
	@echo "Removed: $(GOBIN)/$(BINARY)"
endif

# ──────────────────────────────────────────────────────────────────────────────
# Test
# ──────────────────────────────────────────────────────────────────────────────

# -count=1 defeats the go test result cache: with a warm GOCACHE a previous
# "ok" can be replayed without re-running anything, which is exactly the signal
# CI must not accept. Same reason every `go test` in .github/workflows/ci.yml
# carries -count=1.
test: ## Run the full test suite (docker-gated acceptance included if daemon is up)
	go test ./... -count=1

test-nodocker: ## Run tests without docker (skips docker-gated acceptance)
	SMOKE_SKIP_DOCKER=1 go test ./... -count=1

test-acceptance: ## Run the docker-gated acceptance suite verbosely
	go test ./test/... -v -count=1

test-race: ## Run tests with the race detector
	go test -race ./... -count=1

# ──────────────────────────────────────────────────────────────────────────────
# Code quality
# ──────────────────────────────────────────────────────────────────────────────

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go source files in-place (gofmt -w)
	gofmt -w .

fmt-check: ## Check formatting; fails (non-zero) if any file is unformatted
	@UNFORMATTED=$$(gofmt -l .); \
	if [ -n "$$UNFORMATTED" ]; then \
	  echo "Unformatted files:"; \
	  echo "$$UNFORMATTED"; \
	  exit 1; \
	fi
	@echo "All files gofmt-clean."

# golangci-lint must run under the module's OWN Go toolchain. The linter binary
# is built from source with a specific Go (CI builds v2.12.2 with the project's
# Go); when the machine's default `go` is NEWER, the linter's typechecker sees a
# standard library it was not built against and reports bogus "undefined: rand"
# style typecheck errors. Pinning GOTOOLCHAIN to go.mod's version makes the local
# gate reproduce CI instead of failing on a toolchain mismatch.
LINT_TOOLCHAIN := $(shell go list -m -f '{{.GoVersion}}' 2>/dev/null)

# Keep the pin in one place: both `lint` and `ci` run it. An empty probe (not in
# a module) falls back to running the linter as-is.
GOLANGCI = if [ -n "$(LINT_TOOLCHAIN)" ]; then GOTOOLCHAIN=go$(LINT_TOOLCHAIN) golangci-lint run; else golangci-lint run; fi

lint: ## Run golangci-lint if installed, else print a skip notice
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  $(GOLANGCI); \
	else \
	  echo "golangci-lint not installed — skipping lint (install from https://golangci-lint.run)."; \
	fi

tidy: ## Run go mod tidy
	go mod tidy

# ──────────────────────────────────────────────────────────────────────────────
# Seam check
#
# CI contract: ONLY internal/engine may import compose-spec/compose-go.
# Any other package importing it is a violation (import-seam isolation).
# ──────────────────────────────────────────────────────────────────────────────

seam: ## Fail if any package other than internal/engine imports compose-go
	@VIOLATIONS=$$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
	  | grep 'compose-spec/compose-go' \
	  | grep -v '^$(MODULE)/internal/engine '); \
	if [ -n "$$VIOLATIONS" ]; then \
	  echo "SEAM VIOLATION — only internal/engine may import compose-go:"; \
	  echo "$$VIOLATIONS"; \
	  exit 1; \
	fi
	@echo "Seam OK — only internal/engine imports compose-go."

# ──────────────────────────────────────────────────────────────────────────────
# CI aggregate
# ──────────────────────────────────────────────────────────────────────────────

ci: vet fmt-check seam build test ## Run CI checks: vet fmt-check seam build test (lint if available)
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  $(GOLANGCI); \
	fi

# ──────────────────────────────────────────────────────────────────────────────
# Demo (dev/test route — exercises the gap-detector on examples/monorepo)
#
# Workflow:
#   1. Build a local binary.
#   2. Copy examples/monorepo to a temp dir (mktemp -d — portable).
#   3. Run `cenvkit init` there to seed .env/.*.env from example.* templates.
#   4. Run the four showcase commands against the temp dir.
#   5. Clean up the temp dir.
# ──────────────────────────────────────────────────────────────────────────────

demo: dev-build ## Build + run gap-detector showcase on a temp copy of examples/monorepo
	@DEMO_DIR=$$(mktemp -d); \
	echo "==> Copying examples/monorepo to $$DEMO_DIR"; \
	cp -r examples/monorepo/. $$DEMO_DIR/; \
	echo ""; \
	echo "==> cenvkit init (seed .env / .dev.env / .prod.env from example.*)"; \
	./$(DEV_BIN) --project-dir $$DEMO_DIR init; \
	echo ""; \
	echo "==> cenvkit env-files (Layer-1 COMPOSE_ENV_FILES)"; \
	./$(DEV_BIN) --project-dir $$DEMO_DIR env-files; \
	echo ""; \
	echo "==> cenvkit env-debug --files (interpolation set + runtime-only service env_file paths)"; \
	./$(DEV_BIN) --project-dir $$DEMO_DIR env-debug --files; \
	echo ""; \
	echo "==> cenvkit env-debug --var WEB_PORT (gap-detector: WEB_PORT is env_file-only)"; \
	./$(DEV_BIN) --project-dir $$DEMO_DIR env-debug --var WEB_PORT; \
	echo ""; \
	echo "==> cenvkit env-debug --effective --service web (final container env for web)"; \
	./$(DEV_BIN) --project-dir $$DEMO_DIR env-debug --effective --service web; \
	echo ""; \
	echo "==> Cleaning up $$DEMO_DIR"; \
	rm -rf $$DEMO_DIR; \
	echo "Done."

# ──────────────────────────────────────────────────────────────────────────────
# Dev convenience
# ──────────────────────────────────────────────────────────────────────────────

run: ## Run cenvkit via go run (pass args with ARGS="…", e.g. make run ARGS="env-files")
	go run $(PKG) $(ARGS)

# ──────────────────────────────────────────────────────────────────────────────
# Clean
# ──────────────────────────────────────────────────────────────────────────────

clean: ## Remove local build artifacts ($(DEV_BIN), bin/, dist/)
	rm -f $(DEV_BIN)
	rm -rf bin/ dist/
	@echo "Clean."
