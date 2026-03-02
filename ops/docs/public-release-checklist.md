# Public Release Checklist

Use this checklist before publishing Duffel as a public repository.

## Release readiness

- [ ] `just ci` passes locally
- [ ] `just release-audit` passes locally
- [ ] README positioning is accurate and public-safe
- [ ] `AGENTS.md` is public-safe and aligned with README
- [ ] `LICENSE`, `CONTRIBUTING.md`, and `SECURITY.md` exist

## Privacy and data hygiene

- [ ] No tracked personal note data or local runtime artifacts
- [ ] No absolute personal machine paths in tracked files
- [ ] No personal email addresses or secret material in tracked files

## Git history identity rewrite (one-time pre-publication)

Replace local/private commit identity with GitHub noreply identity.

1. Create mailmap file:

```bash
cat > /tmp/duffel-mailmap <<'MAP'
Public Name <github-username@users.noreply.github.com> <jibs@example.com>
MAP
```

2. Rewrite history:

```bash
git filter-repo --force --mailmap /tmp/duffel-mailmap
```

3. Verify rewritten history:

```bash
git log --format='%an <%ae>' | sort -u
```

4. Force-push rewritten history:

```bash
git push --force-with-lease origin main
```

## Final project journal entry

```bash
./duffel.sh journal append self/journal.md "Release: public release hardening and privacy audit prep"
```
