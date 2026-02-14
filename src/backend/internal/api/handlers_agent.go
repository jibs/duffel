package api

import (
	"fmt"
	"net/http"
)

const agentProtocolVersion = 2

func handleAgentVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "%d", agentProtocolVersion) //nolint:errcheck
	}
}

func handleAgentScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		baseURL := scheme + "://" + r.Host

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="duffel.sh"`)
		fmt.Fprintf(w, agentScript, baseURL, agentProtocolVersion) //nolint:errcheck
	}
}

func handleAgentSnippet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		baseURL := scheme + "://" + r.Host
		path := r.URL.Query().Get("path")

		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		if path != "" {
			fmt.Fprintf(w, agentSnippetProject, path, baseURL, baseURL, path, path, path, path, path, path, path, path, baseURL) //nolint:errcheck
		} else {
			fmt.Fprintf(w, agentSnippet, baseURL, baseURL, baseURL) //nolint:errcheck
		}
	}
}

// agentScript is the bash script template. %s is replaced with the base URL.
var agentScript = `#!/usr/bin/env bash
set -euo pipefail

DUFFEL_URL="${DUFFEL_URL:-%s}"
DUFFEL_SCRIPT_VERSION="%d"

check_version() {
  local server_version
  server_version=$(curl -s --max-time 2 "${DUFFEL_URL}/api/agent/version") || true
  if [ -z "$server_version" ]; then
    printf 'duffel: cannot reach duffel server at %%s\n' "$DUFFEL_URL" >&2
    exit 1
  fi
  if [ "$server_version" != "$DUFFEL_SCRIPT_VERSION" ]; then
    printf 'duffel: script version mismatch (script=%%s, server=%%s)\n' "$DUFFEL_SCRIPT_VERSION" "$server_version" >&2
    printf 'Update with: curl -s %%s/api/agent/script > %%s\n' "$DUFFEL_URL" "$0" >&2
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
  search <query> [options]          Search notes
    -n <limit>                     Max results (default 20, max 100)
    -o <offset>                    Skip N results (pagination)
    -s <sort>                      Sort by: score (default) or date
    -p <prefix>                    Filter by path prefix
    --after <date>                 Only docs modified after ISO date
    --before <date>                Only docs modified before ISO date
USAGE
  exit 1
}

# URL-encode a path (minimal: spaces and special chars)
urlencode_path() {
  local path="$1"
  # curl handles most encoding; just make sure we pass the path cleanly
  printf '%%s' "$path"
}

cmd_ls() {
  local path="${1:-/}"
  local response
  response=$(curl -s "${DUFFEL_URL}/api/fs/${path}")
  # Check for error
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  # Parse entries: extract name and isDir pairs
  printf '%%s' "$response" | grep -o '"name":"[^"]*","isDir":[a-z]*' | while IFS= read -r line; do
    name=$(printf '%%s' "$line" | sed 's/.*"name":"\([^"]*\)".*/\1/')
    is_dir=$(printf '%%s' "$line" | sed 's/.*"isDir":\([a-z]*\).*/\1/')
    if [ "$is_dir" = "true" ]; then
      printf 'dir  %%s\n' "$name"
    else
      printf 'file %%s\n' "$name"
    fi
  done
}

cmd_read() {
  local path="$1"
  local response
  response=$(curl -s "${DUFFEL_URL}/api/fs/${path}")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  # Extract content field value - handles multiline JSON strings
  printf '%%s\n' "$response" | sed -n 's/.*"content":"\(.*\)","size":.*/\1/p' | sed 's/\\n/\
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
  json_content=$(printf '%%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%%s", $0}')
  local response
  response=$(curl -s -X PUT -H "Content-Type: application/json" \
    -d "{\"content\":\"${json_content}\"}" \
    "${DUFFEL_URL}/api/fs/${path}")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_rm() {
  local path="$1"
  local response
  response=$(curl -s -X DELETE "${DUFFEL_URL}/api/fs/${path}")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
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
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
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
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_archive() {
  local path="$1"
  local response
  response=$(curl -s -X POST "${DUFFEL_URL}/api/archive/${path}")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_unarchive() {
  local path="$1"
  local response
  response=$(curl -s -X POST "${DUFFEL_URL}/api/unarchive/${path}")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_journal_create() {
  local path="$1"
  shift
  local content="${*:-}"
  local json_content
  json_content=$(printf '%%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%%s", $0}')
  local response
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"content\":\"${json_content}\"}" \
    "${DUFFEL_URL}/api/journal/${path}")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
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
  json_content=$(printf '%%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%%s", $0}')
  local response
  response=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"content\":\"${json_content}\"}" \
    "${DUFFEL_URL}/api/journal/${path}/append")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf 'ok\n'
}

cmd_search() {
  local limit="" offset="" sort="" prefix="" after="" before=""
  local query_parts=()
  while [ $# -gt 0 ]; do
    case "$1" in
      -n)     shift; limit="$1" ;;
      -o)     shift; offset="$1" ;;
      -s)     shift; sort="$1" ;;
      -p)     shift; prefix="$1" ;;
      --after)  shift; after="$1" ;;
      --before) shift; before="$1" ;;
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
  [ -n "$sort" ]   && curl_args+=(--data-urlencode "sort=${sort}")
  [ -n "$prefix" ] && curl_args+=(--data-urlencode "prefix=${prefix}")
  [ -n "$after" ]  && curl_args+=(--data-urlencode "after=${after}")
  [ -n "$before" ] && curl_args+=(--data-urlencode "before=${before}")
  local response
  response=$(curl "${curl_args[@]}" "${DUFFEL_URL}/api/search")
  if printf '%%s' "$response" | grep -q '"error"'; then
    printf '%%s' "$response" | sed -n 's/.*"error" *: *"\([^"]*\)".*/\1/p'
    return 1
  fi
  printf '%%s\n' "$response"
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
  search)   shift; [ $# -lt 1 ] && usage; cmd_search "$@" ;;
  *)        usage ;;
esac
`

// agentSnippet is the markdown template. %s slots: (1) base URL for download, (2) base URL for description, (3) base URL for examples.
var agentSnippet = `# Duffel

Duffel is a note-taking system accessible via API at %s.

## Setup

Download the CLI script (one-time):

` + "```" + `bash
curl -s %s/api/agent/script > ./duffel.sh && chmod +x ./duffel.sh
` + "```" + `

## Commands

| Command | Description |
|---------|-------------|
| ` + "`duffel ls [path]`" + ` | List directory contents |
| ` + "`duffel read <path>`" + ` | Read file content (raw markdown) |
| ` + "`duffel write <path> [content\\|-]`" + ` | Create/update file (reads stdin if - or no content) |
| ` + "`duffel rm <path>`" + ` | Delete file or empty directory |
| ` + "`duffel mkdir <path>`" + ` | Create directory |
| ` + "`duffel mv <source> <destination>`" + ` | Move/rename file or directory |
| ` + "`duffel archive <path>`" + ` | Archive a file |
| ` + "`duffel unarchive <path>`" + ` | Unarchive a file |
| ` + "`duffel journal create <path> [content]`" + ` | Create a journal file |
| ` + "`duffel journal append <path> <content>`" + ` | Append entry to journal |
| ` + "`duffel search <query> [options]`" + ` | Search notes (options: -n limit, -o offset, -s score/date, -p prefix, --after date, --before date) |

## Search

The search supports FTS5 query syntax passed through directly:

- ` + "`duffel search \"exact phrase\"`" + ` — phrase search
- ` + "`duffel search \"title:keyword\"`" + ` — search only in titles
- ` + "`duffel search \"word1 OR word2\"`" + ` — boolean OR
- ` + "`duffel search \"word1 NOT word2\"`" + ` — exclusion
- ` + "`duffel search \"key*\"`" + ` — prefix wildcard
- ` + "`duffel search pasta -n 5`" + ` — limit to 5 results
- ` + "`duffel search pasta -s date`" + ` — sort by date (newest first)
- ` + "`duffel search pasta -p projects/`" + ` — only in projects/ path
- ` + "`duffel search pasta --after 2026-01-01`" + ` — modified after date

Results include ` + "`path`" + `, ` + "`title`" + `, ` + "`snippet`" + `, ` + "`score`" + `, and ` + "`modified_at`" + `.

## Example Workflow

` + "```" + `bash
# List all notes
duffel ls

# Read a note
duffel read projects/myproject.md

# Create a new note
duffel write meeting-notes.md "# Standup 2024-01-15\n- Discussed API changes"

# Write from stdin
echo "# Draft" | duffel write draft.md -

# Create a directory
duffel mkdir projects

# Search for something
duffel search "API design"

# Search with options
duffel search pasta -n 5 -s date --after 2026-01-01

# Journal workflow
duffel journal create log.md "Started project"
duffel journal append log.md "Added authentication"
` + "```" + `

## Notes

- All output is plain text, optimized for LLM consumption
- The script requires ` + "`curl`" + ` (no other dependencies)
- Override the server URL: ` + "`DUFFEL_URL=%s ./duffel.sh ls`" + `
`

// agentSnippetProject is the project-scoped markdown template.
// %s slots: (1) project path, (2) base URL description, (3) base URL download,
// (4-11) project path in examples, (12) base URL for env override.
var agentSnippetProject = `# Duffel — %s

Project notes are stored in duffel at %s. All paths below are relative to this project.

## Setup

Download the CLI script (one-time):

` + "```" + `bash
curl -s %s/api/agent/script > ./duffel.sh && chmod +x ./duffel.sh
` + "```" + `

## Commands

| Command | Description |
|---------|-------------|
| ` + "`duffel ls %s`" + ` | List project files |
| ` + "`duffel read %s/<file>`" + ` | Read a project note |
| ` + "`duffel write %s/<file> <content>`" + ` | Create/update a project note |
| ` + "`duffel rm %s/<file>`" + ` | Delete a project note |
| ` + "`duffel mkdir %s/<subdir>`" + ` | Create a subdirectory |
| ` + "`duffel mv %s/<old> %s/<new>`" + ` | Move/rename a project file |
| ` + "`duffel journal create %s/log.md <content>`" + ` | Create a project journal |
| ` + "`duffel search <query> [options]`" + ` | Search notes (-n limit, -o offset, -s score/date, -p prefix, --after/--before date) |

## Notes

- All output is plain text, optimized for LLM consumption
- The script requires ` + "`curl`" + ` (no other dependencies)
- Override the server URL: ` + "`DUFFEL_URL=%s ./duffel.sh ls`" + `
`
