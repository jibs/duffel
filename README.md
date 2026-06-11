# Duffel

Duffel is a local-network markdown workspace for human + LLM coding collaboration.

It provides:

- Web UI for browsing and editing notes
- JSON API for automation
- MCP connector for search-first LLM retrieval and writes

Notes are filesystem-backed, and URL paths map directly to files/directories under the data root.

## Quick Start

Requirements: Go `1.26+`, Node.js + pnpm, `make`, `curl`.

`qmd` is vendored as a pinned npm dependency (`@tobilu/qmd@2.1.0`) and resolved from local `node_modules`; no global `qmd` install is required.

```bash
make setup
make dev
```

Open `http://localhost:4386`.

Default config:

- `DUFFEL_PORT=4386`
- `DUFFEL_DATA_DIR=./data`
- `DUFFEL_FRONTEND_DIR=./src/frontend`
- `DUFFEL_AUTH_ENABLED=true`
- `DUFFEL_SETUP_TOKEN=<required for first owner setup when auth is enabled>`

For trusted local-only development, you can disable auth with `DUFFEL_AUTH_ENABLED=false`.

## LLM Workflow (Search First)

Use the MCP connector for new agent integrations. Open `http://localhost:4386/#/_mcp`, create a token, and configure your client with the streamable HTTP endpoint:

```json
{
  "mcpServers": {
    "duffel": {
      "type": "streamable-http",
      "url": "http://localhost:4386/mcp",
      "headers": {
        "Authorization": "Bearer ${DUFFEL_MCP_TOKEN}"
      }
    }
  }
}
```

Tips:

- Prefer `find` before `ls`
- Start with small limits (`-n 5` to `-n 8`)
- Use `--paths` or `--brief` before full payloads
- Add `--intent` when a query is ambiguous

## API Summary

All routes are under `/api` (plus OAuth/MCP discovery at root). Responses are JSON except `/api/agent/*`.

- OAuth / discovery:
  - `GET /.well-known/oauth-protected-resource`
  - `GET /.well-known/oauth-protected-resource/mcp`
  - `GET /.well-known/oauth-authorization-server`
  - `GET|POST /oauth/setup`
  - `GET|POST /oauth/authorize`
  - `POST /oauth/token`
  - `POST /oauth/register`
- MCP:
  - `GET /mcp`
  - `POST /mcp`

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
- MCP token management:
  - `GET /api/mcp/tokens`
  - `POST /api/mcp/tokens` body `{ "label": "Codex desktop" }`
  - `DELETE /api/mcp/tokens/{id}`
- Agent:
  - `GET /api/agent/script`
  - `GET /api/agent/version`
  - `GET /api/agent/snippet`

## Security Boundaries

Duffel now ships built-in OAuth protection for API and MCP endpoints.

- Auth is enabled by default (`DUFFEL_AUTH_ENABLED=true`)
- First owner setup requires `DUFFEL_SETUP_TOKEN`
- API data routes and `/mcp` require Bearer auth
- `/api/agent/*` and OAuth discovery/setup routes stay public for bootstrap
- Path traversal protections remain enforced in storage
- Same-origin CORS defaults and mutating cross-origin guard remain active

## Deployment

Duffel can run as a systemd service on any host that has Go, Node.js, pnpm, and this repository checked out. Set `DEPLOY_HOST` for your server, push to `origin/main`, then:

```bash
DEPLOY_HOST=deploy@example-host make deploy
```

This restarts the service over SSH. `start.sh` handles the rest: pulls latest, rebuilds, and starts the binary. See [ops/deploy/DEPLOY.md](ops/deploy/DEPLOY.md) for server details and first-time setup.

## Development

- `make setup`
- `make dev`
- `make build`
- `make test`
- `make lint`
- `make fmt`
- `make release-audit`
- `make ci`

Docs:

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [SECURITY.md](SECURITY.md)
- [AGENTS.md](AGENTS.md)
- [ops/docs/public-release-checklist.md](ops/docs/public-release-checklist.md)
