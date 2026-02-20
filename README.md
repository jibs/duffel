# Duffel

Duffel is a local-network markdown notes tool with:

- A web UI
- A JSON API
- A lightweight agent CLI (`duffel.sh`)

Storage is filesystem-backed, and URL paths map directly to files/directories under a data root.

## Why Duffel

- Fast, simple note storage with no database service to run
- Markdown-first workflow
- Journal files with timestamped append entries
- Archive/unarchive workflow using `.archive/` sibling directories
- Optional full-text search powered by qmd
- Agent-friendly API and copyable prompt snippet

## Project Layout

- `src/backend/` Go server (chi router + handlers + storage)
- `src/frontend/` static frontend assets
- `tests/` unit/integration/e2e/live tests
- `ops/` operational docs
- `data/` notes root (gitignored by default)

## Requirements

- Go `1.25+`
- Node.js + pnpm (for frontend tooling and `qmd` dependency)
- `just` command runner
- `curl`
- Optional: `qmd` (search support; local package is installed via `pnpm`)

## Quick Start

```bash
just setup
just dev
```

Then open:

- `http://localhost:4386`

Default runtime config:

- `DUFFEL_PORT=4386`
- `DUFFEL_DATA_DIR=./data`
- `DUFFEL_FRONTEND_DIR=./src/frontend`

## Web UI

Key capabilities:

- Browse directories and files
- Create files/folders
- Edit markdown with preview
- Render markdown safely in read mode
- Journal append view for journal files
- Archive or delete files
- Search sidebar with scoped prefix behavior
- Copy "Agent Snippet" for current project path

## API Overview

All API routes are under `/api` and return JSON (except agent script/snippet/version endpoints).

### Filesystem

- `GET /api/fs/*` list directory or read file
- `PUT /api/fs/*` write file body: `{ "content": "..." }`
- `POST /api/fs/*` create directory body: `{ "type": "directory" }`
- `DELETE /api/fs/*` delete file or empty dir
- `POST /api/move/*` move/rename body: `{ "destination": "..." }`

### Archive

- `POST /api/archive/*` archive file into sibling `.archive/`
- `POST /api/unarchive/*` restore from sibling `.archive/`

### Journal

- `POST /api/journal/*` create journal body: `{ "content": "..." }`
- `POST /api/journal/*/append` append entry body: `{ "content": "..." }`

Journal format:

- Front matter: `type: journal`
- Append entries include timestamp header `## YYYY-MM-DD HH:MM`
- Existing journals are protected from normal `PUT` writes

### Search

- `GET /api/search?q=<query>`

Optional query params:

- `limit` positive integer, default `20`, max `100`
- `offset` integer, default `0`
- `sort` one of `score` (default) or `date`
- `prefix` path prefix filter
- `after` ISO date lower bound (`modified_at >= after`)
- `before` ISO date upper bound (`modified_at < before`)
- `fields` comma-separated projection subset:
  - `path,title,snippet,score,modified_at`

Search uses qmd-backed FTS5 query syntax (phrases, boolean terms, prefix wildcard, field filters such as `title:...`).

If qmd has not indexed yet, `/api/search` returns `503`.

### Agent Endpoints

- `GET /api/agent/script` download `duffel.sh`
- `GET /api/agent/version` protocol version string
- `GET /api/agent/snippet` prompt snippet
- `GET /api/agent/snippet?path=<projectPrefix>` project-scoped snippet

## Agent CLI (`duffel.sh`)

Download once:

```bash
curl -s http://localhost:4386/api/agent/script > ./duffel.sh && chmod +x ./duffel.sh
```

Core commands:

- `duffel ls [path]`
- `duffel read <path>`
- `duffel write <path> [content|-]`
- `duffel rm <path>`
- `duffel mkdir <path>`
- `duffel mv <source> <destination>`
- `duffel archive <path>`
- `duffel unarchive <path>`
- `duffel journal create <path> [content]`
- `duffel journal append <path> <content>`
- `duffel find <query> [options]`
- `duffel search <query> [options]`

Search-first options in CLI:

- `-n <limit>`
- `-o <offset>`
- `-s <score|date>`
- `-p <prefix>`
- `--after <date>`
- `--before <date>`
- `--fields <csv>`
- `--brief` (`path,title,modified_at,score`)
- `--paths` (`path` only)

`find` is a compact helper equivalent to:

- `search -n 8 --brief ...`

## Search Setup Notes

At startup, Duffel attempts to:

- Ensure qmd collection `duffel` is configured for the data directory
- Start background indexing (`qmd update`)
- Open qmd index at `~/.cache/qmd/index.sqlite`

If this fails, the app still runs, but search is unavailable until qmd indexing is healthy.

## Development Commands

- `just setup` install deps and rebuild native addons (run on new devices)
- `just dev` run dev server
- `just build` build `./duffel` binary
- `just test` run unit + integration tests
- `just lint` run Go + frontend TypeScript lint
- `just fmt` format Go and frontend TypeScript (via ESLint `--fix`)
- `just ci` full pipeline (`fmt-check`, `lint`, `typecheck`, `build-js`, `test`)

## Security Model

- No authentication (intended for trusted local network usage)
- Path traversal prevention via canonicalization and symlink checks
- CORS allows same-origin by default
- Additional origins can be allowed via `DUFFEL_ALLOWED_ORIGINS`
- Cross-origin mutating API requests are blocked unless explicitly allowed

## Contributing

1. Make changes.
2. Run `just ci`.
3. Update journal with one concise entry per logical change:

```bash
./duffel.sh journal append self/journal.md "Area: what changed and why"
```

If you change `duffel.sh` behavior or its server-side contract, increment:

- `agentProtocolVersion` in `src/backend/internal/api/handlers_agent.go`
