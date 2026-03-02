# Duffel Agent Instructions

## Overview

Duffel is a local-network markdown notes workspace with filesystem-backed storage and a search-first workflow for human + LLM collaboration.

The core model is simple:

- Markdown files are the source of truth
- URL paths map directly to files/directories under the data directory
- Search is provided through qmd-backed indexing

## Required Workflow After Every Code Change

Every completed logical change (feature, bugfix, refactor, docs affecting behavior) must follow this order:

1. Run `just ci`
2. Commit changes (and push if requested)
3. Append one journal entry

```bash
./duffel.sh journal append self/journal.md "Area: brief description of what changed and why"
```

If `duffel.sh` is missing locally:

```bash
curl -s http://localhost:4386/api/agent/script > ./duffel.sh && chmod +x ./duffel.sh
```

If the journal file does not exist yet:

```bash
./duffel.sh journal create self/journal.md "Project changelog"
```

Journal entry rules:

- One entry per logical change
- Concise summary of what changed and why
- Include affected area (for example, `API: ...`, `Frontend: ...`, `Search: ...`)

## Architecture

- Backend: Go + chi router
- Frontend: static HTML/CSS/TypeScript (compiled with `tsc`)
- Storage: filesystem under `./data` by default
- Search: qmd CLI index/query pipeline

## Core Conventions

- API responses are JSON (except agent script/snippet/version endpoints)
- All paths must be canonicalized and remain under data root
- Journal files use front matter `type: journal`
- Journal append inserts `---` + `## YYYY-MM-DD HH:MM` timestamp section
- Archive moves files to a sibling `.archive/` directory

## Code Style

- Go: standard library style, gofmt-compatible, minimal abstraction
- TypeScript: strict mode, vanilla ES modules, no framework assumptions
- Prefer small, focused functions and clear user-facing errors

## Test Expectations

- Unit tests: `tests/unit/backend/`
- Integration tests: `tests/integration/backend/`
- Run `just ci` before merge

## Common Commands

- `just setup` install dependencies
- `just dev` run development server
- `just test` run all tests
- `just lint` run linters
- `just ci` full checks (including release audit)
- `just release-audit` run privacy/leak scan on tracked files

## Important Paths

- API handlers: `src/backend/internal/api/`
- Storage layer: `src/backend/internal/storage/`
- Search layer: `src/backend/internal/search/`
- Frontend TS: `src/frontend/ts/`
- Config: `src/backend/internal/config/`

## Agent CLI Contract

The CLI script (`duffel.sh`) is served by the backend at `/api/agent/script`.

Compatibility is enforced by `agentProtocolVersion` in `src/backend/internal/api/handlers_agent.go`.

- `GET /api/agent/version` returns protocol version text
- `duffel.sh` checks server version on every run and errors on mismatch

## Protocol Version Bump Rules

Increment `agentProtocolVersion` when changing CLI script behavior or its API contract, including:

- Adding/removing/renaming script commands
- Changing endpoint paths/methods/response formats used by the script
- Changing script argument parsing or dispatch behavior
- Changing `DUFFEL_URL` resolution or compatibility behavior

Do not increment for server-internal changes that do not affect CLI/script compatibility.
