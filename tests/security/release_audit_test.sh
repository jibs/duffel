#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
export TRUFFLEHOG="${TRUFFLEHOG:-$repo_root/.tools/bin/trufflehog}"
test_tmp="$(mktemp -d)"
trap 'rm -rf "$test_tmp"' EXIT
fixture="$test_tmp/repo"
mkdir -p "$fixture/ops/scripts" "$fixture/ops/config"
cp "$repo_root/.gitignore" "$fixture/.gitignore"
cp "$repo_root/ops/scripts/release_audit.sh" "$fixture/ops/scripts/"
cp "$repo_root/ops/config/release_audit_allowlist.txt" "$fixture/ops/config/"
cd "$fixture"
git init -q
git config user.name release-audit-test
git config user.email audit@example.com
printf 'Safe source\n' > README.md
git add .
git commit -qm 'Safe initial source'

expect_pass() {
  if ! bash ops/scripts/release_audit.sh > "$test_tmp/output" 2>&1; then
    echo "FAIL: $1 did not pass" >&2
    cat "$test_tmp/output" >&2
    exit 1
  fi
  echo "PASS: $1"
}

expect_failure() {
  local label="$1"
  local message="$2"
  if bash ops/scripts/release_audit.sh > "$test_tmp/output" 2>&1; then
    echo "FAIL: $label passed unexpectedly" >&2
    exit 1
  fi
  if ! rg -q -F "$message" "$test_tmp/output"; then
    echo "FAIL: $label did not report the expected failure" >&2
    cat "$test_tmp/output" >&2
    exit 1
  fi
  echo "PASS: $label"
}

expect_pass 'clean repository'
TRUFFLEHOG="$test_tmp/missing" expect_failure 'missing scanner' 'TruffleHog is required'
printf '#!/usr/bin/env bash\nexit 47\n' > "$test_tmp/broken-scanner"
chmod +x "$test_tmp/broken-scanner"
TRUFFLEHOG="$test_tmp/broken-scanner" expect_failure 'scanner error' 'failed (exit 47)'

git clone -q --depth 1 "file://$fixture" "$test_tmp/shallow"
(
  cd "$test_tmp/shallow"
  expect_failure 'shallow history' 'full Git history is required'
)

# Generated, disposable credentials exercise the actual detector without
# retaining a secret fixture or contacting a credential provider.
ssh-keygen -q -t ed25519 -N '' -C release-audit-test -f "$test_tmp/key"
cp "$test_tmp/key" .env
printf '# Sanitized example\n' > .env.example
for path in .env .env.production .envrc client_secret_google.json .mcp.json \
  ops/deploy/private/host.env ops/config/local/runtime.yml auth.sqlite3 \
  debug.log .tools/bin/tool; do
  git check-ignore -q "$path"
done
for path in .env.example .env.production.example ops/deploy/duffel.service \
  src/backend/main.go .github/workflows/ci.yml; do
  if git check-ignore -q "$path"; then
    echo "FAIL: shareable source/configuration was ignored: $path" >&2
    exit 1
  fi
done
expect_pass 'ignored private files and shareable examples'
git add -f .env
expect_failure 'force-added ignored credentials' 'tracked files match ignore rules'
git rm -q --cached .env

cp .env sensitive.txt
expect_failure 'untracked source secret' 'matched content withheld'
git add sensitive.txt
printf 'Clean worktree; secret remains staged\n' > sensitive.txt
expect_failure 'staged secret hidden by clean worktree' 'secret candidate; value withheld'
key_fragment="$(sed -n '2p' "$test_tmp/key")"
if rg -q -F "$key_fragment" "$test_tmp/output"; then
  echo 'FAIL: scanner report disclosed a credential value' >&2
  exit 1
fi

git commit -qm 'Disposable historical credential'
git rm -q -f sensitive.txt
git commit -qm 'Remove disposable credential'
expect_failure 'deleted secret retained in history' 'TruffleHog git failed'
if rg -q -F "$key_fragment" "$test_tmp/output"; then
  echo 'FAIL: historical report disclosed a credential value' >&2
  exit 1
fi
echo 'Release audit regression checks passed'
