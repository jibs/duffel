#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

allowlist_file="${1:-ops/config/release_audit_allowlist.txt}"
if [ ! -f "$allowlist_file" ]; then
  echo "release-audit: allowlist file not found: $allowlist_file" >&2
  exit 1
fi

tmp_allowlist="$(mktemp)"
trap 'rm -f "$tmp_allowlist"' EXIT

grep -vE '^\s*($|#)' "$allowlist_file" > "$tmp_allowlist" || true

scan_files=()
while IFS= read -r -d '' file; do
  case "$file" in
    src/frontend/lib/marked.min.js|bun.lock|pnpm-lock.yaml|go.sum)
      continue
      ;;
  esac
  scan_files+=("$file")
done < <(git ls-files -z)

if [ "${#scan_files[@]}" -eq 0 ]; then
  echo "release-audit: no tracked files to scan"
  exit 0
fi

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

  if [ "$mode" = "pcre" ]; then
    output="$(rg -n --no-heading --color=never -H -P "$pattern" "${scan_files[@]}" || true)"
  else
    output="$(rg -n --no-heading --color=never -H -e "$pattern" "${scan_files[@]}" || true)"
  fi

  output="$(apply_allowlist "$output")"
  if [ -n "$output" ]; then
    printf '### %s\n%s\n\n' "$label" "$output"
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

echo "release-audit: no issues found"
