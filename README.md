# Duffel

Duffel is a local-network markdown workspace for human + LLM coding collaboration.

It provides:

- Web UI for browsing and editing notes
- JSON API for automation
- Lightweight CLI (`duffel.sh`) for search-first retrieval

Notes are filesystem-backed, and URL paths map directly to files/directories under the data root.

## Quick Start

Requirements: Go `1.26+`, Node.js + pnpm, `just`, `curl`.

```bash
just setup
just dev
```

Open `http://localhost:4386`.

Default config:

- `DUFFEL_PORT=4386`
- `DUFFEL_DATA_DIR=./data`
- `DUFFEL_FRONTEND_DIR=./src/frontend`

## LLM Workflow (Search First)

Use compact retrieval before reading full notes:

```bash
./duffel.sh find "auth session cache"
./duffel.sh search "performance" --intent "software optimization" --brief -n 8
./duffel.sh search "auth OR session OR cache*" --paths -n 30 -o 0
./duffel.sh read projects/auth/session-design.md
./duffel.sh journal append self/journal.md "API: updated session invalidation behavior"
```

Tips:

- Prefer `find` before `ls`
- Start with small limits (`-n 5` to `-n 8`)
- Use `--paths` or `--brief` before full payloads
- Add `--intent` when a query is ambiguous

## API Summary

All routes are under `/api`. Responses are JSON except `/api/agent/*`.

- Filesystem:
  - `GET /api/fs/*`
  - `PUT /api/fs/*` body `{ "content": "..." }`
  - `POST /api/fs/*` body `{ "type": "directory" }`
  - `DELETE /api/fs/*`
  - `POST /api/move/*` body `{ "destination": "..." }`
- Archive:
  - `POST /api/archive/*`
  - `POST /api/unarchive/*`
- Journal:
  - `POST /api/journal/*`
  - `POST /api/journal/*/append`
- Search:
  - `GET /api/search?q=<query>`
  - Options: `limit`, `offset`, `intent`, `candidate_limit`, `min_score`, `explain`, `fields`
  - Legacy filters rejected with `400`: `sort`, `prefix`, `after`, `before`
- Agent:
  - `GET /api/agent/script`
  - `GET /api/agent/version`
  - `GET /api/agent/snippet`

## CLI Summary

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
- `duffel find <query> [options]` (shorthand for `search -n 8 --brief`)
- `duffel search <query> [options]`

Search options:

- `-n`, `-o`, `--intent`
- `-C`/`--candidate-limit`, `--min-score`, `--explain`
- `--fields`, `--brief`, `--paths`
- Legacy flags removed: `-s`, `-p`, `--after`, `--before`

## Security Boundaries

Duffel is intended for trusted local-network use.

- No built-in authentication
- Path traversal protections in storage layer
- Same-origin CORS by default
- Cross-origin mutating requests blocked unless explicitly allowed
- Additional allowed origins configurable via `DUFFEL_ALLOWED_ORIGINS`

Do not expose Duffel directly to the public internet without additional auth/network controls.

## Development

- `just setup`
- `just dev`
- `just build`
- `just test`
- `just lint`
- `just fmt`
- `just release-audit`
- `just ci`

Docs:

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [SECURITY.md](SECURITY.md)
- [AGENTS.md](AGENTS.md)
- [ops/docs/public-release-checklist.md](ops/docs/public-release-checklist.md)
