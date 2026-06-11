SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

DEPLOY_HOST ?= deploy@example-host

ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help build-js dev build test test-unit test-integration test-e2e test-live fmt fmt-check lint lint-go lint-js typecheck ci deploy setup clean release-audit

help:
	@echo "Available targets:"
	@echo "  setup          Install dependencies and rebuild native addons"
	@echo "  dev            Build frontend JS and run development server"
	@echo "  build-js       Build TypeScript frontend"
	@echo "  build          Build backend binary"
	@echo "  test           Run all tests"
	@echo "  test-unit      Run backend unit tests"
	@echo "  test-integration Run backend integration tests"
	@echo "  test-e2e       Run end-to-end tests"
	@echo "  test-live      Run live tests (LIVE_TESTS=1 LIVE_TESTS_CONFIRM=YES)"
	@echo "  fmt            Format Go and frontend TS"
	@echo "  fmt-check      Check Go formatting"
	@echo "  lint           Run Go and JS linters"
	@echo "  typecheck      Run go vet and TypeScript checks"
	@echo "  ci             Run full CI pipeline"
	@echo "  deploy         Deploy latest to production (push origin/main first)"
	@echo "  release-audit  Run privacy/leak scan on tracked files"
	@echo "  clean          Remove build artifacts"

build-js:
	pnpm tsc

dev: build-js
	go run ./src/backend/cmd/server

build:
	go build -o duffel ./src/backend/cmd/server

test: test-unit test-integration test-e2e

test-unit:
	pkgs="$$( { go list ./src/backend/internal/... 2>/dev/null || true; go list ./tests/unit/backend/... 2>/dev/null || true; } | sort -u )"; \
	unit_pkgs="$$(printf '%s\n' "$$pkgs" | rg -v '/api$$' || true)"; \
	if [ -z "$$unit_pkgs" ]; then \
		echo "No unit test packages found"; \
		exit 1; \
	fi; \
	go test $$unit_pkgs

test-integration:
	pkgs="$$( { go list ./src/backend/internal/api 2>/dev/null || true; go list ./tests/integration/backend/... 2>/dev/null || true; } | sort -u )"; \
	if [ -z "$$pkgs" ]; then \
		echo "No integration test packages found"; \
		exit 1; \
	fi; \
	go test $$pkgs

test-e2e:
	go test ./tests/e2e/...

test-live:
	if [ "$${LIVE_TESTS:-}" != "1" ] || [ "$${LIVE_TESTS_CONFIRM:-}" != "YES" ]; then \
		echo "Live tests require LIVE_TESTS=1 and LIVE_TESTS_CONFIRM=YES"; \
		exit 1; \
	fi; \
	go test ./tests/live/...

fmt:
	gofmt -w ./src/backend/ ./tests/
	node node_modules/eslint/bin/eslint.js --fix src/frontend/ts/

fmt-check:
	if [ -n "$$(gofmt -l ./src/backend/ ./tests/)" ]; then \
		echo "Go files not formatted:"; \
		gofmt -l ./src/backend/ ./tests/; \
		exit 1; \
	fi

lint: lint-go lint-js

lint-go:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./src/backend/...

lint-js:
	node node_modules/eslint/bin/eslint.js src/frontend/ts/

typecheck:
	pkgs="$$( { go list ./src/backend/... 2>/dev/null || true; go list ./tests/unit/backend/... 2>/dev/null || true; go list ./tests/integration/backend/... 2>/dev/null || true; } | sort -u )"; \
	if [ -z "$$pkgs" ]; then \
		echo "No packages found for typecheck"; \
		exit 1; \
	fi; \
	go vet $$pkgs; \
	pnpm tsc --noEmit

ci: fmt-check lint typecheck build-js test release-audit

deploy:
	@echo "Deploying to $(DEPLOY_HOST)..."
	ssh $(DEPLOY_HOST) "sudo systemctl restart duffel"
	@echo "Done. start.sh will pull origin/main, rebuild, and restart."

setup:
	pnpm install
	for d in node_modules/.pnpm/better-sqlite3@*/node_modules/better-sqlite3; do (cd "$$d" && npm run build-release) || exit 1; done

clean:
	rm -f duffel
	rm -rf src/frontend/js/
	go clean ./...

release-audit:
	bash ./ops/scripts/release_audit.sh
