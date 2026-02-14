# Duffel — Personal Notes Tool

## Overview
A personal markdown notes tool accessible over a local network. Filesystem-backed storage where URL paths mirror the directory structure.

## After Every Code Change

Every completed change (feature, bugfix, refactor, etc.) **must** follow these steps in order:

1. Run `just ci` — ensure all checks pass
2. Commit and push (if requested)
3. Log the change to the project journal:

```bash
./duffel.sh journal append self/journal.md "Area: brief description of what changed and why"
```

If the duffel CLI doesn't exist locally, download it first:

```bash
curl -s http://localhost:4386/api/agent/script > ./duffel.sh && chmod +x ./duffel.sh
```

If the journal doesn't exist yet, create it first:

```bash
./duffel.sh journal create self/journal.md "Project changelog"
```

Journal rules:
- One entry per logical change (not per file)
- Keep entries concise — what changed and why, not how
- Include the affected area (e.g. "API: added version endpoint", "Frontend: fixed search rendering")

## Architecture
- **Backend:** Go with chi router
- **Frontend:** Static HTML/CSS/JS, no build step
- **Storage:** Filesystem, paths mirror URLs, data dir defaults to `./data`
- **Search:** Via qmd CLI (hybrid markdown search)

## Key Conventions
- All API responses are JSON
- Path security: all paths canonicalized, must stay under data dir
- Journal files use front-matter `type: journal`, append adds `---` separator + `## YYYY-MM-DD HH:MM` timestamp
- Archive moves files to `.archive/` sibling directory
- No auth needed (local network tool)

## Code Style
- Go: standard library style, `gofmt`, no unnecessary abstractions
- JS: vanilla ES modules, no framework, ESLint for linting
- Keep functions small and focused
- Error messages should be user-friendly

## Testing
- Unit tests alongside code in `tests/unit/backend/`
- Integration tests in `tests/integration/backend/`
- Run `just ci` before committing

## Commands
- `just dev` — run dev server
- `just ci` — full CI pipeline (fmt-check, lint, typecheck, test)
- `just test` — run all tests
- `just lint` — run all linters

## Important Paths
- API handlers: `src/backend/internal/api/`
- Storage layer: `src/backend/internal/storage/`
- Frontend JS: `src/frontend/js/`
- Config: `src/backend/internal/config/`

## Directory Layout
- `src/backend/` — Go server code
- `src/frontend/` — Static HTML/CSS/JS
- `tests/` — Test suites (unit, integration, e2e, live)
- `ops/` — Deployment and documentation
- `data/` — Storage directory (gitignored)

## Agent CLI Script

The agent CLI (`duffel.sh`) is served by the server at `/api/agent/script` and cached locally by clients. A protocol version is baked into both the script and the server to detect incompatibility.

- Version constant: `agentProtocolVersion` in `src/backend/internal/api/handlers_agent.go`
- Version endpoint: `GET /api/agent/version` returns the current version as plain text
- The script checks this on every invocation and errors on mismatch

## Release Protocol

When making changes that affect the agent CLI script or its API surface, you **must** increment `agentProtocolVersion` in `src/backend/internal/api/handlers_agent.go`. This includes:

- Adding, removing, or renaming CLI commands in the script template
- Changing API endpoint paths, methods, or response formats used by the script
- Changing the script's argument parsing or dispatch logic
- Modifying the `DUFFEL_URL` resolution or any behavioral contract the script depends on

Do **not** increment for changes that are purely server-internal (e.g. storage refactors, frontend-only changes, new endpoints not used by the script).
