set dotenv-load

default:
    @just --list

# Build TypeScript
build-js:
    pnpm tsc

# Run dev server
dev: build-js
    go run ./src/backend/cmd/server

# Build binary
build:
    go build -o duffel ./src/backend/cmd/server

# Run all tests
test: test-unit test-integration

# Unit tests
test-unit:
    #!/usr/bin/env bash
    set -euo pipefail
    pkgs=$(go list ./src/backend/internal/... ./tests/unit/backend/... 2>/dev/null)
    unit_pkgs=$(printf '%s\n' "$pkgs" | rg -v '/api$' || true)
    if [ -z "$unit_pkgs" ]; then
        echo "No unit test packages found"
        exit 1
    fi
    go test $unit_pkgs

# Integration tests
test-integration:
    #!/usr/bin/env bash
    set -euo pipefail
    pkgs=$(go list ./src/backend/internal/api ./tests/integration/backend/... 2>/dev/null)
    if [ -z "$pkgs" ]; then
        echo "No integration test packages found"
        exit 1
    fi
    go test $pkgs

# E2E tests
test-e2e:
    go test ./tests/e2e/...

# Live tests (requires LIVE_TESTS=1 LIVE_TESTS_CONFIRM=YES)
test-live:
    #!/usr/bin/env bash
    if [ "$LIVE_TESTS" != "1" ] || [ "$LIVE_TESTS_CONFIRM" != "YES" ]; then
        echo "Live tests require LIVE_TESTS=1 and LIVE_TESTS_CONFIRM=YES"
        exit 1
    fi
    go test ./tests/live/...

# Format code
fmt:
    gofmt -w ./src/backend/ ./tests/
    pnpm eslint --fix src/frontend/ts/

# Check formatting
fmt-check:
    #!/usr/bin/env bash
    if [ -n "$(gofmt -l ./src/backend/ ./tests/)" ]; then
        echo "Go files not formatted:"
        gofmt -l ./src/backend/ ./tests/
        exit 1
    fi

# Run all linters
lint: lint-go lint-js

# Lint Go
lint-go:
    golangci-lint run ./src/backend/...

# Lint JS
lint-js:
    pnpm eslint src/frontend/ts/

# Type check (go vet + tsc)
typecheck:
    #!/usr/bin/env bash
    set -euo pipefail
    pkgs=$(go list ./src/backend/... ./tests/unit/backend/... ./tests/integration/backend/... 2>/dev/null)
    go vet $pkgs
    pnpm tsc --noEmit

# CI pipeline
ci: fmt-check lint typecheck build-js test

# Deploy
deploy:
    @echo "Deploy not configured yet"

# Clean build artifacts
clean:
    rm -f duffel
    rm -rf src/frontend/js/
    go clean ./...
