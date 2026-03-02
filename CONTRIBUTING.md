# Contributing to Duffel

Thanks for helping improve Duffel.

## Development setup

1. Install prerequisites:
   - Go 1.26+
   - Node.js + pnpm
   - `just`
   - `curl`
2. Install project dependencies:

```bash
just setup
```

## Standard workflow

1. Create a branch for your change.
2. Make code changes with tests.
3. Run full checks:

```bash
just ci
```

4. Run the release/privacy scanner:

```bash
just release-audit
```

5. Add one concise journal entry for the logical change:

```bash
./duffel.sh journal append self/journal.md "Area: what changed and why"
```

## Required checks before merge

- `just ci` passes
- `just release-audit` passes
- Relevant tests for the changed behavior exist or are updated

## Agent CLI protocol rule

If you change `duffel.sh` behavior or its server-side API contract, increment:

- `agentProtocolVersion` in `src/backend/internal/api/handlers_agent.go`

Increment the protocol version when changing:

- CLI command names/arguments/dispatch behavior
- Agent endpoints used by the script (paths, methods, response contracts)
- `DUFFEL_URL` resolution or compatibility behavior

Do not increment for server-internal changes that do not affect CLI compatibility.
