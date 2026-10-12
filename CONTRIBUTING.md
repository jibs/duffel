# Contributing to Duffel

Thanks for helping improve Duffel.

## Development setup

1. Install prerequisites:
   - Go 1.26+
   - Node.js + pnpm
   - `make`
   - `curl`
   - `jq` (redacted release scanner reports)
   - `rg` (ripgrep, used by privacy checks)
2. Install project dependencies:

```bash
make setup
make setup-lint
make setup-release-audit
```

## Standard workflow

The Go linter is pinned to golangci-lint v2.14.0 and installed under `.tools/bin`
with a verified SHA-256 checksum. `make lint` uses that binary and validates the
v2 configuration before linting; it does not fall back to an older global tool.

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

The release gate requires TruffleHog and full Git history. It scans committed
history, staged files, and the current non-ignored source files, fails on secret
candidates or scanner errors, and reports locations without printing credential
values. Verification and automatic scanner updates are disabled. Ignored local
data and configuration are excluded; force-added ignored files are rejected.

5. Add one concise journal entry for the logical change through the MCP connector.

## Required checks before merge

- `make ci` passes
- `make release-audit` and `make test-release-audit` pass
- Relevant tests for the changed behavior exist or are updated
