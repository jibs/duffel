#!/usr/bin/env bash
set -euo pipefail

DUFFEL_URL="${DUFFEL_URL:-http://localhost:4386}"
DUFFEL_SCRIPT_VERSION="4"

check_version() {
  local server_version
  server_version=$(curl -s --max-time 2 "${DUFFEL_URL}/api/agent/version") || true
  if [ -z "$server_version" ]; then
    printf 'duffel: cannot reach duffel server at %s\n' "$DUFFEL_URL" >&2
    exit 1
  fi
  if [ "$server_version" != "$DUFFEL_SCRIPT_VERSION" ]; then
    printf 'duffel: script version mismatch (script=%s, server=%s)\n' "$DUFFEL_SCRIPT_VERSION" "$server_version" >&2
    printf 'Update with: curl -s %s/api/agent/script > %s\n' "$DUFFEL_URL" "$0" >&2
    exit 1
  fi
}

usage() {
  cat <<'USAGE'
Usage: duffel <command> [args]

Commands:
  ls [path]                        List directory contents
  read <path>                      Read file content
  write <path> [content|-]         Create/update file (stdin if - or no content)
  rm <path>                        Delete file or empty directory
  mkdir <path>                     Create directory
  mv <source> <destination>         Move/rename file or directory
  archive <path>                   Archive file
  unarchive <path>                 Unarchive file
  journal create <path> [content]  Create journal
  journal append <path> <content>  Append to journal
  find <query> [options]            Search-first helper (defaults: -n 8 --brief)
  search <query> [options]          Search notes
    -n <limit>                     Max results (default 20, max 100)
    -o <offset>                    Skip N results (pagination)
    --intent <text>                Optional disambiguation intent
    -C, --candidate-limit <num>    Max candidates sent to reranker
    --min-score <num>              Minimum score threshold
    --explain                      Include retrieval score traces
    --fields <csv>                 Projection: path,title,snippet,score,modified_at,explain
    --brief                        Equivalent to --fields path,title,modified_at,score
    --paths                        Equivalent to --fields path
    legacy flags removed: -s, -p, --after, --before
USAGE
  exit 1
}

# URL-encode a path (minimal: spaces and special chars)
urlencode_path() {
  local path="$1"
  # curl handles most encoding; just make sure we pass the path cleanly
  printf '%s' "$path"
}

cmd_ls() {
  local path="${1:-/}"
  local response
  response=$(curl -s "${DUFFEL_URL}/api/fs/${path}")
  # Check for error
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  # Parse entries: extract name and isDir pairs
  printf '%s' "$response" | grep -o '"name":"[^"]*","isDir":[a-z]*' | while IFS= read -r line; do
    name=$(printf '%s' "$line" | sed 's/.*"name":"\([^"]*\)".*/\1/')
    is_dir=$(printf '%s' "$line" | sed 's/.*"isDir":\([a-z]*\).*/\1/')
    if [ "$is_dir" = "true" ]; then
      printf 'dir  %s\n' "$name"
    else
      printf 'file %s\n' "$name"
    fi
  done
}

cmd_read() {
  local path="$1"
  local response
  response=$(curl -s "${DUFFEL_URL}/api/fs/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  # Extract content field value - handles multiline JSON strings
  printf '%s\n' "$response" | sed -n 's/.*"content":"\(.*\)","size":.*/\1/p' | sed 's/\\n/\
/g; s/\\t/\t/g; s/\\"/"/g; s/\\\\/\\/g'
}

cmd_write() {
  local path="$1"
  shift
  local content
  if [ $# -eq 0 ] || [ "${1:-}" = "-" ]; then
    content=$(cat)
  else
    content="$*"
  fi
  # Escape content for JSON
  local json_content
  json_content=$(printf '%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%s", $0}')
  local response
  response=$(curl -s -X PUT -H "Content-Type: application/json" \
    -d "{\"content\":\"${json_content}\"}" \
    "${DUFFEL_URL}/api/fs/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_rm() {
  local path="$1"
  local response
  response=$(curl -s -X DELETE "${DUFFEL_URL}/api/fs/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_mkdir() {
  local path="$1"
  local response
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    -d '{"type":"directory"}' \
    "${DUFFEL_URL}/api/fs/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_mv() {
  local src="$1"
  local dst="$2"
  local response
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"destination\":\"${dst}\"}" \
    "${DUFFEL_URL}/api/move/${src}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_archive() {
  local path="$1"
  local response
  response=$(curl -s -X POST "${DUFFEL_URL}/api/archive/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_unarchive() {
  local path="$1"
  local response
  response=$(curl -s -X POST "${DUFFEL_URL}/api/unarchive/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_journal_create() {
  local path="$1"
  shift
  local content="${*:-}"
  local json_content
  json_content=$(printf '%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%s", $0}')
  local response
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"content\":\"${json_content}\"}" \
    "${DUFFEL_URL}/api/journal/${path}")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_journal_append() {
  local path="$1"
  shift
  local content="$*"
  if [ -z "$content" ]; then
    echo "error: content is required for journal append"
    return 1
  fi
  local json_content
  json_content=$(printf '%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%s", $0}')
  local response
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"content\":\"${json_content}\"}" \
    "${DUFFEL_URL}/api/journal/${path}/append")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_search() {
  local limit="" offset="" intent="" candidate_limit="" min_score="" explain="" fields=""
  local query_parts=()
  while [ $# -gt 0 ]; do
    case "$1" in
      -n)
        [ $# -lt 2 ] && { echo "error: -n requires a value"; return 1; }
        shift; limit="$1"
        ;;
      -o)
        [ $# -lt 2 ] && { echo "error: -o requires a value"; return 1; }
        shift; offset="$1"
        ;;
      --intent)
        [ $# -lt 2 ] && { echo "error: --intent requires a value"; return 1; }
        shift; intent="$1"
        ;;
      -C|--candidate-limit)
        [ $# -lt 2 ] && { echo "error: $1 requires a value"; return 1; }
        shift; candidate_limit="$1"
        ;;
      --min-score)
        [ $# -lt 2 ] && { echo "error: --min-score requires a value"; return 1; }
        shift; min_score="$1"
        ;;
      --explain) explain="true" ;;
      --fields)
        [ $# -lt 2 ] && { echo "error: --fields requires a value"; return 1; }
        shift; fields="$1"
        ;;
      --brief) fields="path,title,modified_at,score" ;;
      --paths) fields="path" ;;
      -s|-p|--after|--before)
        echo "error: $1 is no longer supported. Use --intent, -C/--candidate-limit, --min-score, --explain, --fields."
        return 1
        ;;
      *)      query_parts+=("$1") ;;
    esac
    shift
  done
  local query="${query_parts[*]}"
  if [ -z "$query" ]; then
    echo "error: search query is required"
    return 1
  fi
  local curl_args=(-s -G --data-urlencode "q=${query}")
  [ -n "$limit" ]  && curl_args+=(--data-urlencode "limit=${limit}")
  [ -n "$offset" ] && curl_args+=(--data-urlencode "offset=${offset}")
  [ -n "$intent" ] && curl_args+=(--data-urlencode "intent=${intent}")
  [ -n "$candidate_limit" ] && curl_args+=(--data-urlencode "candidate_limit=${candidate_limit}")
  [ -n "$min_score" ] && curl_args+=(--data-urlencode "min_score=${min_score}")
  [ -n "$explain" ] && curl_args+=(--data-urlencode "explain=${explain}")
  [ -n "$fields" ] && curl_args+=(--data-urlencode "fields=${fields}")
  local response
  response=$(curl "${curl_args[@]}" "${DUFFEL_URL}/api/search")
  if printf '%s' "$response" | grep -q '"error"'; then
    printf '%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf '%s\n' "$response"
}

cmd_find() {
  cmd_search -n 8 --brief "$@"
}

# Main dispatch
[ $# -lt 1 ] && usage
check_version

case "$1" in
  ls)       shift; cmd_ls "$@" ;;
  read)     shift; [ $# -lt 1 ] && usage; cmd_read "$@" ;;
  write)    shift; [ $# -lt 1 ] && usage; cmd_write "$@" ;;
  rm)       shift; [ $# -lt 1 ] && usage; cmd_rm "$@" ;;
  mkdir)    shift; [ $# -lt 1 ] && usage; cmd_mkdir "$@" ;;
  mv)       shift; [ $# -lt 2 ] && usage; cmd_mv "$@" ;;
  archive)  shift; [ $# -lt 1 ] && usage; cmd_archive "$@" ;;
  unarchive) shift; [ $# -lt 1 ] && usage; cmd_unarchive "$@" ;;
  journal)
    shift
    [ $# -lt 1 ] && usage
    subcmd="$1"; shift
    case "$subcmd" in
      create) [ $# -lt 1 ] && usage; cmd_journal_create "$@" ;;
      append) [ $# -lt 2 ] && usage; cmd_journal_append "$@" ;;
      *) usage ;;
    esac
    ;;
  find)     shift; [ $# -lt 1 ] && usage; cmd_find "$@" ;;
  search)   shift; [ $# -lt 1 ] && usage; cmd_search "$@" ;;
  *)        usage ;;
esac
