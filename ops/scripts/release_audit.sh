#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

trufflehog="${TRUFFLEHOG:-$repo_root/.tools/bin/trufflehog}"
if ! command -v "$trufflehog" >/dev/null 2>&1; then
  echo "release-audit: TruffleHog is required; run make setup-release-audit" >&2
  exit 1
fi
for tool in jq rg; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "release-audit: $tool is required" >&2
    exit 1
  fi
done
if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
  echo "release-audit: full Git history is required; run git fetch --unshallow" >&2
  exit 1
fi

# Gitignore alone does not protect files that were already added or force-added.
ignored_tracked="$(git ls-files -ci --exclude-standard)"
if [ -n "$ignored_tracked" ]; then
  echo "release-audit: tracked files match ignore rules; remove them from the index:" >&2
  printf '%s\n' "$ignored_tracked" >&2
  exit 1
fi
if git ls-files --stage | rg '^(120000|160000) ' >/dev/null; then
  echo "release-audit: tracked symlinks/submodules need an explicit safe scan policy" >&2
  exit 1
fi

allowlist_file="${1:-ops/config/release_audit_allowlist.txt}"
if [ ! -f "$allowlist_file" ]; then
  echo "release-audit: allowlist file not found: $allowlist_file" >&2
  exit 1
fi

audit_tmp="$(mktemp -d)"
trap 'rm -rf "$audit_tmp"' EXIT
tmp_allowlist="$audit_tmp/allowlist"
mkdir -p "$audit_tmp/worktree" "$audit_tmp/index"

grep -vE '^\s*($|#)' "$allowlist_file" > "$tmp_allowlist" || true

scan_files=()
git ls-files --cached --others --exclude-standard -z > "$audit_tmp/files"
while IFS= read -r -d '' file; do
  if [ ! -f "$file" ]; then
    continue
  fi
  if [ -L "$file" ]; then
    echo "release-audit: refusing to follow symlink: $file" >&2
    exit 1
  fi
  mkdir -p "$audit_tmp/worktree/$(dirname "$file")"
  cp "$file" "$audit_tmp/worktree/$file"
  case "$file" in
    src/frontend/lib/marked.min.js|pnpm-lock.yaml|go.sum|ops/scripts/release_audit.sh|ops/config/release_audit_allowlist.txt)
      continue
      ;;
  esac
  scan_files+=("$file")
done < "$audit_tmp/files"

# Scan staged content too: a clean worktree can hide a secret still in the index.
git checkout-index --all --prefix="$audit_tmp/index/"

apply_allowlist() {
  local raw="$1"
  if [ -z "$raw" ]; then
    return 0
  fi
  if [ -s "$tmp_allowlist" ]; then
    printf '%s\n' "$raw" | grep -F -v -f "$tmp_allowlist" || true
  else
    printf '%s\n' "$raw"
  fi
}

run_scan() {
  local label="$1"
  local mode="$2"
  local pattern="$3"
  local output=""
  local status=0
  if [ "${#scan_files[@]}" -eq 0 ]; then
    return 0
  fi

  if [ "$mode" = "pcre" ]; then
    output="$(rg -n --no-heading --color=never -H -P "$pattern" "${scan_files[@]}")" || status=$?
  else
    output="$(rg -n --no-heading --color=never -H -e "$pattern" "${scan_files[@]}")" || status=$?
  fi
  if [ "$status" -gt 1 ]; then
    echo "release-audit: $label scan failed" >&2
    return "$status"
  fi

  output="$(apply_allowlist "$output")"
  if [ -n "$output" ]; then
    printf '### %s\n' "$label"
    printf '%s\n' "$output" | awk -F: '{print $1 ":" $2 ": [matched content withheld]"}'
  fi
}

report=""
report+="$(run_scan "Absolute user paths" "pcre" "(/Users/[A-Za-z0-9._-]+|/home/[A-Za-z0-9._-]+|[A-Za-z]:\\\\Users\\\\[A-Za-z0-9._ -]+)")"
report+="$(run_scan "High-confidence secret signatures" "pcre" "(BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{35}|aws_secret_access_key|aws_access_key_id)")"
report+="$(run_scan "Suspicious credential assignments" "pcre" "(?i)\\b(api[_-]?key|access[_-]?token|auth[_-]?token|password|passwd|secret)\\b\\s*[:=]\\s*['\"][^'\"]{8,}['\"]")"
report+="$(run_scan "Personal email addresses" "pcre" "[A-Za-z0-9._%+-]+@(?!example\\.(com|org|net)\\b)(?!users\\.noreply\\.github\\.com\\b)[A-Za-z0-9.-]+\\.[A-Za-z]{2,}")"

if [ -n "$report" ]; then
  echo "release-audit: potential issues found"
  echo
  printf '%s' "$report"
  exit 1
fi

run_trufflehog() {
  local label="$1"
  shift
  local status=0
  # Candidate credentials stay local. Include unverified results so expired or
  # offline credentials cannot pass the gate. Never emit raw secrets into CI.
  "$trufflehog" "$label" "$@" --no-update --no-verification --no-ignore-tag \
    --results=verified,unknown,unverified --fail --fail-on-scan-errors --json \
    > "$audit_tmp/results.jsonl" 2> "$audit_tmp/scanner.log" || status=$?
  if [ "$status" -ne 0 ]; then
    echo "release-audit: TruffleHog $label failed (exit $status)" >&2
    jq -r '
      .SourceMetadata.Data | to_entries[] | .value |
      "  \(.file // .path // "unknown file"):\(.line // "?") (secret candidate; value withheld)"
    ' "$audit_tmp/results.jsonl" >&2
    echo "Inspect locally with TruffleHog; candidate values and scanner logs are withheld." >&2
    return "$status"
  fi
  echo "release-audit: TruffleHog $label passed"
}

run_trufflehog git "file://$repo_root"
run_trufflehog filesystem "$audit_tmp/worktree" "$audit_tmp/index"
echo "release-audit: privacy, Git history, index, and worktree scans passed"
