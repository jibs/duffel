# Contributing to Duffel

Thanks for helping improve Duffel.

## Development setup

1. Install prerequisites:
   - Go 1.26+
   - Node.js + pnpm
   - `make`
   - `curl`
2. Install project dependencies:

```bash
make setup
```

## Standard workflow

1. Create a branch for your change.
2. Make code changes with tests.
3. Run full checks:

```bash
make ci
```

4. Run the release/privacy scanner:

```bash
make release-audit
```

5. Add one concise journal entry for the logical change through the MCP connector.

## Required checks before merge

- `make ci` passes
- `make release-audit` passes
- Relevant tests for the changed behavior exist or are updated
