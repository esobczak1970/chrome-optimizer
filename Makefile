SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO ?= go
GO_VERSION := $(shell tr -d '[:space:]' < .go-version)
APP := chrome-optimizer
BIN_DIR := $(CURDIR)/bin
CHROME_DEBUG_PORT ?= 9222
CHROME_DEBUG_PROFILE ?= $(HOME)/Library/Application Support/ChromeOptimizerDebug

.PHONY: help doctor deps tidy format format-check lint vet test test-shell test-race build run run-debug chrome-start check clean

help:
	@printf '%s\n' 'chrome-optimizer development commands:' \
	  '  make doctor        Check pinned Go version' \
	  '  make deps          Download Go dependencies' \
	  '  make format-check  Verify gofmt' \
	  '  make lint          Run go vet (additional linter planned)' \
	  '  make test          Run offline-compatible unit tests after deps' \
	  '  make test-race     Run race tests' \
	  '  make build         Build CLI' \
	  '  make check         Format, vet, test, and build' \
	  '  make run           Sample an already-debuggable Chrome (read-only)' \
	  '  make run ARGS="--compact --filter=news"      Search in a compact table' \
	  '  make run ARGS="--freeze=ID --confirm-freeze"  Freeze a hidden page explicitly' \
	  '  make run ARGS="--resume=ID"                 Resume a selected page explicitly' \
	  '  make chrome-start  Start a separate Chrome debugging profile (macOS)' \
	  '  make run-debug     Start/reuse the separate Chrome profile and discover its tabs' \
	  '  make test-shell    Test Chrome launch safeguards without real Chrome'

doctor:
	@command -v "$(GO)" >/dev/null || { echo "Go must be installed" >&2; exit 1; }
	@actual="$$($(GO) env GOVERSION)"; expected="go$(GO_VERSION)"; \
	  [[ "$$actual" == "$$expected" ]] || { echo "expected $$expected, got $$actual" >&2; exit 1; }
	@$(GO) version

deps: doctor
	$(GO) mod download

tidy: doctor
	$(GO) mod tidy

format: doctor
	@find . -type f -name '*.go' -not -path './bin/*' -print0 | xargs -0 gofmt -w

format-check: doctor
	@bad="$$(find . -type f -name '*.go' -not -path './bin/*' -print0 | xargs -0 gofmt -l)"; \
	  [[ -z "$$bad" ]] || { echo "gofmt needed: $$bad" >&2; exit 1; }

vet: deps
	$(GO) vet ./...

lint: vet

test-shell:
	bash scripts/chrome-debug_test.sh

test: deps test-shell
	$(GO) test -count=1 ./...

test-race: deps
	$(GO) test -race -count=1 ./...

build: deps
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -trimpath -o "$(BIN_DIR)/$(APP)" .

run: deps
	$(GO) run . $(ARGS)

# This is intentionally explicit: make run never starts or changes Chrome.
chrome-start:
	@CHROME_DEBUG_PORT="$(CHROME_DEBUG_PORT)" CHROME_DEBUG_PROFILE="$(CHROME_DEBUG_PROFILE)" bash scripts/chrome-debug.sh

run-debug: deps
	@$(MAKE) --no-print-directory chrome-start
	$(GO) run . --endpoint="http://127.0.0.1:$(CHROME_DEBUG_PORT)" $(ARGS)

check: format-check vet test build

clean:
	rm -rf "$(BIN_DIR)"
