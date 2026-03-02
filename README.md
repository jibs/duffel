# Duffel

Duffel is a local-network markdown workspace for humans and LLM coding agents to collaborate through a shared, searchable repository of notes.

It combines:

- A web UI for browsing and editing notes
- A JSON API for automation and integrations
- A lightweight CLI (`duffel.sh`) for model-friendly retrieval and updates

Storage is filesystem-backed, and URL paths map directly to files/directories under the data root.

## Why Duffel

Duffel is optimized for coding workflows where models need fast context retrieval before making changes.

- Search-first workflow (`find`/`search`) to reduce token usage
- Markdown-native notes and design docs
- Journal files with timestamped append entries
- Simple archive/unarchive behavior using `.archive/` sibling directories
- No database service to run

## Requirements

- Go `1.26+`
- Node.js + pnpm
- `just`
- `curl`
- Optional: `qmd` (local package installed via `pnpm`)

## Quick Start

```bash
just setup
just dev
```

Then open `http://localhost:4386`.

Default runtime config:

- `DUFFEL_PORT=4386`
- `DUFFEL_DATA_DIR=./data`
- `DUFFEL_FRONTEND_DIR=./src/frontend`

## LLM Collaboration Workflow

Recommended flow for coding agents and humans:

1. Discover relevant notes with compact retrieval first.
2. Expand only when needed.
3. Read targeted files.
4. Write or append updates after synthesis.

Example:

```bash
# Start compact
./duffel.sh find "auth session cache" -p projects/

# Expand cheaply (paths only)
./duffel.sh search "auth OR session OR cache*" --paths -p projects/ -n 30 -o 0

# Read strongest match
./duffel.sh read projects/auth/session-design.md

# Append change notes
./duffel.sh journal append self/journal.md "API: updated session invalidation behavior"
```

Token-saving defaults:

- Prefer `find` before `ls`
- Start with small limits (`-n 5` to `-n 8`)
- Use `--paths` or `--brief` before full result payloads
- Scope with `-p <prefix>` and optional `--after` / `--before`

## Web UI Capabilities

- Browse files and directories
- Create files/folders
- Edit markdown with preview
- Safe markdown rendering in read mode
- Journal append view for journal files
- Archive/delete actions
- Search with sort, prefix, and date filters
- Copy project-scoped agent snippet

## API Overview

All API routes are under `/api` and return JSON, except the agent script/snippet/version endpoints.

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

Journal rules:

- Front matter includes `type: journal`
- Append entries include `## YYYY-MM-DD HH:MM`
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
- `fields` comma-separated subset: `path,title,snippet,score,modified_at`

Search uses qmd-backed FTS5 syntax (phrases, boolean terms, prefix wildcard, and field filters like `title:...`).

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

Search options:

- `-n <limit>`
- `-o <offset>`
- `-s <score|date>`
- `-p <prefix>`
- `--after <date>`
- `--before <date>`
- `--fields <csv>`
- `--brief` (`path,title,modified_at,score`)
- `--paths` (`path` only)

`find` is shorthand for `search -n 8 --brief ...`.

## Search Setup Notes

At startup, Duffel attempts to:

- Ensure qmd collection `duffel` points at the data directory
- Start background indexing (`qmd update`)
- Open qmd index at `~/.cache/qmd/index.sqlite`

If qmd is unavailable, Duffel still runs and search endpoints return unavailable until indexing succeeds.

## Security and Deployment Boundaries

Duffel is for trusted local-network usage.

- No built-in authentication
- Path traversal defenses in storage layer
- Same-origin CORS by default
- Cross-origin mutating requests blocked unless explicitly allowed
- Extra allowed origins can be configured via `DUFFEL_ALLOWED_ORIGINS`

Do not expose Duffel directly to the public internet without additional auth and network controls.

## Development Commands

- `just setup` install deps and rebuild native addons
- `just dev` run dev server
- `just build` build `./duffel` binary
- `just test` run unit + integration tests
- `just lint` run Go + frontend lint
- `just fmt` format Go and frontend TypeScript
- `just release-audit` run privacy/leakage audit on tracked files
- `just ci` full pipeline (`fmt-check`, `lint`, `typecheck`, `build-js`, `test`, `release-audit`)

## Contributing and Release Docs

- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Security policy: [SECURITY.md](SECURITY.md)
- Agent instructions: [AGENTS.md](AGENTS.md)
- Public release checklist: [ops/docs/public-release-checklist.md](ops/docs/public-release-checklist.md)
