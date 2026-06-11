# Duffel

Duffel is a local-network markdown workspace for people and LLM agents.

It stores notes as ordinary Markdown files, serves them through a small web UI,
indexes them with `qmd`, and exposes an MCP server so coding agents can search,
read, write, and append journal entries without custom glue scripts.

## Who this is for

Use Duffel when you want:

- A local notes workspace backed by files you can inspect and version normally.
- A browser UI for reading, editing, archiving, and searching Markdown notes.
- An MCP endpoint that lets LLM tools use the same notes as durable project memory.
- A simple Go + TypeScript app that can run on a laptop, home server, or private network.

## Ask an LLM to set it up

Paste this prompt into your coding assistant from a clean machine or server:

```text
Set up Duffel from https://github.com/jibs/duffel.

Requirements:
- Use pnpm, not npm.
- Do not install a global qmd binary; Duffel uses the pinned @tobilu/qmd package from pnpm dependencies.
- Start with a local development run first.
- If this is only a private local demo, run with DUFFEL_AUTH_ENABLED=false.
- If this is a real shared/private-network deployment, keep auth enabled and set DUFFEL_SETUP_TOKEN before first setup.

Steps:
1. Clone the repo and enter it.
2. Run make setup.
3. Create a few sample Markdown files under ./data if the directory is empty.
4. Start the app with make dev for development, or follow ops/deploy/DEPLOY.md for systemd deployment.
5. Open http://localhost:4386.
6. For LLM integration, open http://localhost:4386/#/_mcp and configure the MCP client shown there.
7. Run make ci before proposing any code changes.
```

## Quick start: local demo

Requirements:

- Go `1.26+`
- Node.js and pnpm
- `make`
- `curl`

```bash
git clone https://github.com/jibs/duffel.git
cd duffel
make setup
mkdir -p data/projects data/research data/journal
cat > data/index.md <<'MD'
---
type: note
---

# Welcome to Duffel

This is a filesystem-backed Markdown workspace.

Try:

- Browse files in the sidebar
- Edit this note
- Search for `MCP`
- Open the MCP Connector page
MD
cat > data/projects/demo.md <<'MD'
---
type: note
---

# Demo project

Duffel keeps project notes in Markdown and exposes them to LLM agents through MCP.

## Tasks

- Capture setup decisions
- Store useful research
- Append journal updates after changes
MD
cat > data/research/mcp.md <<'MD'
---
type: research
---

# MCP notes

Use the MCP connector at `#/_mcp` to configure compatible agents.

Agents should search first, read only relevant files, and append concise journal entries for durable changes.
MD
cat > data/journal/worklog.md <<'MD'
---
type: journal
---

## 2026-01-01 09:00

Demo: created a sample Duffel workspace.
MD
DUFFEL_AUTH_ENABLED=false make dev
```

Open `http://localhost:4386`.

For a local demo, auth is disabled in the command above. For anything reachable by
other people or machines, keep auth enabled and follow the authentication setup
below.

## Production/private-network setup

Duffel enables auth by default.

Before the first owner setup, set a one-time setup token:

```bash
export DUFFEL_SETUP_TOKEN="$(openssl rand -base64 32)"
make dev
```

Open `http://localhost:4386/oauth/setup`, create the owner account, store the
returned bootstrap token in your secret manager if you need CLI/MCP bootstrap
access, then remove `DUFFEL_SETUP_TOKEN` from the runtime environment.

For a systemd deployment, use [ops/deploy/DEPLOY.md](ops/deploy/DEPLOY.md).
That guide uses generic hostnames and paths; replace them with your own server
values.

## LLM and MCP setup

Duffel exposes a streamable HTTP MCP endpoint at `/mcp`.

In the browser UI:

1. Open `http://localhost:4386/#/_mcp`.
2. Create or paste a bearer token if auth is enabled.
3. Copy the generated client config for your agent.

Generic MCP config shape:

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

If you started Duffel with `DUFFEL_AUTH_ENABLED=false`, omit the `headers` block.

Recommended agent behavior:

- Search before reading broad folders.
- Start with small result limits.
- Read only the files needed for the task.
- Use journal files for concise change history.
- Keep research artifacts under a clear folder such as `research/`.
- Do not commit secrets, local note data, `.env`, generated tokens, or runtime logs.

## Configuration

Environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `DUFFEL_HOST` | empty | Bind address. Empty means all interfaces. |
| `DUFFEL_PORT` | `4386` | HTTP port. |
| `DUFFEL_DATA_DIR` | `./data` | Markdown workspace root. |
| `DUFFEL_FRONTEND_DIR` | `./src/frontend` | Static frontend directory. |
| `DUFFEL_AUTH_ENABLED` | `true` | Require OAuth/Bearer auth for API and MCP. |
| `DUFFEL_SETUP_TOKEN` | empty | Required for first owner setup when auth is enabled. |

`qmd` is provided by the pinned npm dependency `@tobilu/qmd@2.1.0`; do not
install or depend on a global `qmd` binary.

## API overview

All data APIs are under `/api`. OAuth and MCP discovery routes are at the root.
Responses are JSON except agent helper script/snippet/version routes and OAuth
HTML forms.

Main routes:

- `GET|PUT|POST|DELETE /api/fs/*` for filesystem-backed notes and directories.
- `POST /api/archive/*` and `POST /api/unarchive/*` for archive moves.
- `POST /api/journal/*` and `POST /api/journal/*/append` for journal files.
- `GET /api/search?q=<query>` for qmd-backed search.
- `GET|POST|DELETE /api/mcp/tokens` for MCP bearer token management.
- `GET|POST /mcp` for the MCP server.
- `GET|POST /oauth/setup`, `/oauth/authorize`, `/oauth/token`, and `/oauth/register` for auth.

## Development

Common commands:

```bash
make setup          # install dependencies
make dev            # build frontend JS and run the Go server
make build          # build the backend binary
make test           # run unit, integration, and e2e tests
make lint           # run Go and TypeScript linters
make fmt            # format Go and TypeScript
make release-audit  # scan tracked files for privacy/secret risks
make ci             # full local check suite
```

Before publishing changes, run:

```bash
make ci
```

The CI target includes formatting checks, linting, Go vet, TypeScript checks,
frontend build, backend tests, e2e tests, and the release audit.

## Security model

- Auth is enabled by default.
- First owner setup requires `DUFFEL_SETUP_TOKEN`.
- API data routes and `/mcp` require Bearer auth when auth is enabled.
- OAuth discovery/setup routes and agent helper routes remain public for bootstrap.
- Storage canonicalizes paths and keeps operations under the configured data root.
- Same-origin CORS defaults and mutating cross-origin protections are enforced.

For public or internet-facing deployment, put Duffel behind HTTPS/TLS and normal
network controls. Duffel is primarily designed for trusted local or private
network use.

## Repository hygiene

Tracked source should not include personal note data, generated tokens, `.env`,
runtime logs, or local machine paths.

Run these before publishing:

```bash
make release-audit
trufflehog filesystem --no-update --only-verified .
```

## More docs

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [SECURITY.md](SECURITY.md)
- [AGENTS.md](AGENTS.md)
- [ops/deploy/DEPLOY.md](ops/deploy/DEPLOY.md)
- [ops/docs/public-release-checklist.md](ops/docs/public-release-checklist.md)
