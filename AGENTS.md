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
- Frontend: static HTML/CSS/TypeScript (compiled with `tsc`)
- Storage: filesystem under `./data` by default
- Search: qmd CLI index/query pipeline

## Core Conventions

- API responses are JSON (except agent script/snippet/version and OAuth authorize HTML responses)
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
- Run `make ci` before merge

## Common Commands

- `make setup` install dependencies
- `make dev` run development server
- `make test` run all tests
- `make lint` run linters
- `make ci` full checks (including release audit)
- `make release-audit` run privacy/leak scan on tracked files

## Important Paths

- API handlers: `src/backend/internal/api/`
- Storage layer: `src/backend/internal/storage/`
- Search layer: `src/backend/internal/search/`
- Frontend TS: `src/frontend/ts/`
- Config: `src/backend/internal/config/`

## Agent Integration Contract

New agent integrations should use the MCP connector:

- Open `#/_mcp` in the Duffel UI
- Create or revoke MCP bearer tokens from that page
- Configure clients with streamable HTTP at `/mcp`
- Use the managed bearer token in the `Authorization` header
