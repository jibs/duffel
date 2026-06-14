# Duffel Agent Instructions

## Overview

Duffel is a local-network markdown notes workspace with filesystem-backed storage and a search-first workflow for human + LLM collaboration.

The core model is simple:

- Markdown files are the source of truth
- URL paths map directly to files/directories under the data directory
- Search is provided through qmd-backed indexing

## Required Workflow After Every Code Change

Every completed logical change (feature, bugfix, refactor, docs affecting behavior) must follow this order:

1. Run `make ci`
2. Commit changes (and push if requested)
3. Append one journal entry through the MCP connector or API

Agents must use the MCP connector directly for Duffel operations. Do not use standalone helper scripts unless the user explicitly asks for that path.

Journal entry rules:

- One entry per logical change
- Concise summary of what changed and why
- Include affected area (for example, `API: ...`, `Frontend: ...`, `Search: ...`)

## Architecture

- Backend: Go + chi router
- Frontend: static HTML/CSS/TypeScript (compiled with `pnpm tsc`, output to `src/frontend/js/`)
- Storage: filesystem under `./data` by default; SQLite (via `modernc.org/sqlite`) for auth/token state
- Search: qmd CLI index/query pipeline
- Auth: optional Tailscale-based trusted-device auth, password auth, and OAuth (`src/backend/internal/auth/`)

## Core Conventions

- API responses are JSON (except agent script/snippet/version, OAuth authorize HTML, image file responses, and SSE event streams)
- All paths must be canonicalized and remain under data root
- Journal files use front matter `type: journal`
- Journal append inserts `---` + `## YYYY-MM-DD HH:MM` timestamp section
- Archive moves files to a sibling `.archive/` directory
- SSE events are broadcast on content change via the event broker (`handlers_events.go`)

## Code Style

- Go: standard library style, gofmt-compatible, minimal abstraction
- TypeScript: strict mode, vanilla ES modules, no framework assumptions
- Prefer small, focused functions and clear user-facing errors

## Test Expectations

- Unit tests: `src/backend/internal/...` (excluding `/api`) + `tests/unit/backend/` — run with `make test-unit`
- Integration tests: `src/backend/internal/api` + `tests/integration/backend/` — run with `make test-integration`
- End-to-end tests: `tests/e2e/` — run with `make test-e2e`
- Live tests: `tests/live/` — require `LIVE_TESTS=1 LIVE_TESTS_CONFIRM=YES`, run with `make test-live`
- Run `make ci` before merge

## Common Commands

- `make setup` install dependencies and rebuild native addons
- `make dev` build frontend JS and run development server
- `make build-js` build TypeScript frontend only
- `make build` build backend binary
- `make test` run all tests (unit + integration + e2e)
- `make test-unit` run backend unit tests
- `make test-integration` run backend integration tests
- `make test-e2e` run end-to-end tests
- `make fmt` format Go and frontend TypeScript
- `make lint` run Go and JS linters
- `make typecheck` run go vet and TypeScript type checks
- `make ci` full CI pipeline (fmt-check, lint, typecheck, build-js, test, release-audit)
- `make deploy` restart the production service via SSH (push origin/main first)
- `make release-audit` run privacy/leak scan on tracked files

## Important Paths

- API handlers: `src/backend/internal/api/`
- Storage layer: `src/backend/internal/storage/`
- Search layer: `src/backend/internal/search/`
- Auth service: `src/backend/internal/auth/`
- Markdown validation: `src/backend/internal/markdown/`
- Frontend TS source: `src/frontend/ts/`
- Frontend compiled JS: `src/frontend/js/` (generated, do not edit directly)
- Config: `src/backend/internal/config/`
- Server entrypoint: `src/backend/cmd/server/main.go`
- Ops scripts: `ops/scripts/`

## Agent Integration Contract

New agent integrations should use the MCP connector:

- Open `#/_mcp` in the Duffel UI
- Create or revoke MCP bearer tokens from that page
- Configure clients with streamable HTTP at `/mcp`
- Use the managed bearer token in the `Authorization` header
