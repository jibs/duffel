package api

import (
	"fmt"
	"net/http"
	"strings"

	"duffel/src/backend/internal/auth"
)

const agentProtocolVersion = 6

func handleAgentVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "%d", agentProtocolVersion) //nolint:errcheck
	}
}

func handleAgentScript(_ *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseURL := requestOrigin(r)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="duffel.sh"`)
		script := strings.NewReplacer(
			"__DUFFEL_URL__", baseURL,
			"__DUFFEL_SCRIPT_VERSION__", fmt.Sprintf("%d", agentProtocolVersion),
		).Replace(agentScript)
		script = strings.ReplaceAll(script, "%%", "%")
		fmt.Fprint(w, script) //nolint:errcheck
	}
}

func handleAgentSnippet(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseURL := requestOrigin(r)
		path := r.URL.Query().Get("path")
		authEnabled := authService != nil && authService.Enabled()

		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprint(w, buildAgentSnippet(path, baseURL, authEnabled)) //nolint:errcheck
	}
}

func buildAgentSnippet(path, baseURL string, authEnabled bool) string {
	var b strings.Builder
	if path != "" {
		fmt.Fprintf(&b, "# Duffel — %s\n\n", path)
		fmt.Fprintf(&b, "Project notes base URL: %s\n\n", baseURL)
	} else {
		b.WriteString("# Duffel\n\n")
		fmt.Fprintf(&b, "Duffel notes base URL: %s\n\n", baseURL)
	}

	b.WriteString("## Setup\n\n")
	if authEnabled {
		b.WriteString("Trusted Tailscale devices can connect without a token. Other devices need a managed bearer token from:\n\n")
	} else {
		b.WriteString("Bearer auth is disabled on this Duffel server. Connect MCP clients without an Authorization header.\n\n")
		b.WriteString("Connector page:\n\n")
	}
	fmt.Fprintf(&b, "```text\n%s/#/_mcp\n```\n\n", baseURL)
	b.WriteString("Configure a streamable HTTP MCP client:\n\n")
	if authEnabled {
		b.WriteString("Trusted Tailscale device:\n\n")
	}
	b.WriteString("```json\n")
	b.WriteString("{\n  \"mcpServers\": {\n    \"duffel\": {\n")
	b.WriteString("      \"type\": \"streamable-http\",\n")
	fmt.Fprintf(&b, "      \"url\": \"%s/mcp\"\n", baseURL)
	b.WriteString("    }\n  }\n}\n")
	b.WriteString("```\n\n")
	if authEnabled {
		b.WriteString("Other devices with bearer token:\n\n")
		b.WriteString("```json\n")
		b.WriteString("{\n  \"mcpServers\": {\n    \"duffel\": {\n")
		b.WriteString("      \"type\": \"streamable-http\",\n")
		fmt.Fprintf(&b, "      \"url\": \"%s/mcp\",\n", baseURL)
		b.WriteString("      \"headers\": {\n        \"Authorization\": \"Bearer ${DUFFEL_MCP_TOKEN}\"\n      }\n")
		b.WriteString("    }\n  }\n}\n")
		b.WriteString("```\n\n")
	}

	if path != "" {
		b.WriteString("## Project scoped retrieval\n\n")
		fmt.Fprintf(&b, "- Search with project path terms: `%s`\n", path)
		fmt.Fprintf(&b, "- Read selected files below `%s/`\n", path)
		fmt.Fprintf(&b, "- Write notes below `%s/` when needed\n", path)
		b.WriteString("- Append changelog entries with `duffel_journal_append`\n\n")
	} else {
		b.WriteString("## Search-first workflow\n\n")
		b.WriteString("1. Use `duffel_search` with compact limits before reading full notes\n")
		b.WriteString("2. Use `duffel_read` for selected files\n")
		b.WriteString("3. Use `duffel_journal_append` for project changelog entries\n\n")
	}

	return b.String()
}

var agentScript = `#!/usr/bin/env bash
set -euo pipefail

DUFFEL_URL="${DUFFEL_URL:-__DUFFEL_URL__}"
DUFFEL_SCRIPT_VERSION="__DUFFEL_SCRIPT_VERSION__"
DUFFEL_AUTH_CACHE="${DUFFEL_AUTH_CACHE:-${HOME:-.}/.duffel/token.env}"
DUFFEL_AUTH_REQUIRED=""

ACCESS_TOKEN=""
REFRESH_TOKEN=""
EXPIRES_AT="0"
AUTH_BEARER=""
API_BODY=""
API_STATUS=""

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
  detect_auth_mode
}

usage() {
  cat <<'USAGE'
Usage: duffel <command> [args]

Deprecated: use Duffel MCP at ${DUFFEL_URL}/mcp for new agent integrations.

Commands:
  auth login                       Login via OAuth (manual code flow)
  ls [path]                        List directory contents
  read <path>                      Read file content
  write <path> [content|-]         Create/update file (stdin if - or no content)
  rm <path>                        Delete file or empty directory
  mkdir <path>                     Create directory
  mv <source> <destination>        Move/rename file or directory
  archive <path>                   Archive file
  unarchive <path>                 Unarchive file
  journal create <path> [content]  Create journal
  journal append <path> <content>  Append to journal
  find <query> [options]           Search-first helper (defaults: -n 8 --brief)
  search <query> [options]         Search notes
USAGE
  exit 1
}

urlencode() {
  local s="$1"
  local i c out=""
  for ((i=0; i<${#s}; i++)); do
    c="${s:i:1}"
    case "$c" in
      [a-zA-Z0-9.~_-]) out+="$c" ;;
      *) printf -v out '%%s%%%02X' "$out" "'$c" ;;
    esac
  done
  printf '%%s' "$out"
}

json_get_string() {
  local key="$1"
  local json="$2"
  printf '%%s' "$json" | tr -d '\n' | sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
}

json_get_number() {
  local key="$1"
  local json="$2"
  printf '%%s' "$json" | tr -d '\n' | sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p"
}

parse_api_error() {
  local body="$1"
  local err
  err=$(json_get_string "error" "$body")
  if [ -n "$err" ]; then
    printf '%%s\n' "$err"
  else
    printf 'request failed\n'
  fi
}

load_auth_cache() {
  ACCESS_TOKEN=""
  REFRESH_TOKEN=""
  EXPIRES_AT="0"
  if [ -f "$DUFFEL_AUTH_CACHE" ]; then
    # shellcheck disable=SC1090
    source "$DUFFEL_AUTH_CACHE"
  fi
}

save_auth_cache() {
  mkdir -p "$(dirname "$DUFFEL_AUTH_CACHE")"
  {
    printf 'ACCESS_TOKEN=%%q\n' "$ACCESS_TOKEN"
    printf 'REFRESH_TOKEN=%%q\n' "$REFRESH_TOKEN"
    printf 'EXPIRES_AT=%%q\n' "$EXPIRES_AT"
  } > "$DUFFEL_AUTH_CACHE"
  chmod 600 "$DUFFEL_AUTH_CACHE" 2>/dev/null || true
}

detect_auth_mode() {
  local status
  status=$(curl -s -o /dev/null -w '%%{http_code}' "${DUFFEL_URL}/oauth/setup") || status="000"
  if [ "$status" = "404" ]; then
    DUFFEL_AUTH_REQUIRED="0"
  else
    DUFFEL_AUTH_REQUIRED="1"
  fi
}

refresh_access_token() {
  load_auth_cache
  if [ -z "$REFRESH_TOKEN" ]; then
    return 1
  fi

  local response status body now expires_in
  response=$(curl -s -X POST \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=refresh_token" \
    --data-urlencode "refresh_token=${REFRESH_TOKEN}" \
    --data-urlencode "client_id=duffel-cli" \
    -w $'\n%%{http_code}' \
    "${DUFFEL_URL}/oauth/token")

  status="${response##*$'\n'}"
  body="${response%$'\n'*}"
  if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
    return 1
  fi

  ACCESS_TOKEN=$(json_get_string "access_token" "$body")
  REFRESH_TOKEN=$(json_get_string "refresh_token" "$body")
  expires_in=$(json_get_number "expires_in" "$body")
  if [ -z "$ACCESS_TOKEN" ] || [ -z "$REFRESH_TOKEN" ] || [ -z "$expires_in" ]; then
    return 1
  fi

  now=$(date +%%s)
  EXPIRES_AT=$((now + expires_in))
  save_auth_cache
  return 0
}

ensure_auth() {
  AUTH_BEARER=""
  if [ "${DUFFEL_AUTH_REQUIRED}" = "0" ]; then
    return 0
  fi
  if [ -n "${DUFFEL_TOKEN:-}" ]; then
    AUTH_BEARER="${DUFFEL_TOKEN}"
    return 0
  fi

  load_auth_cache
  if [ -z "$ACCESS_TOKEN" ]; then
    echo "error: authentication required. Run: duffel auth login" >&2
    return 1
  fi

  local now
  now=$(date +%%s)
  if [ -z "$EXPIRES_AT" ]; then
    EXPIRES_AT=0
  fi
  if [ "$EXPIRES_AT" -le $((now + 30)) ]; then
    if ! refresh_access_token; then
      echo "error: session expired. Run: duffel auth login" >&2
      return 1
    fi
    load_auth_cache
  fi

  AUTH_BEARER="${ACCESS_TOKEN}"
  return 0
}

api_request() {
  local method="$1"
  local url="$2"
  local json_body="${3:-}"

  ensure_auth || return 1

  local response
  local auth_header=""
  if [ -n "$AUTH_BEARER" ]; then
    auth_header="Authorization: Bearer ${AUTH_BEARER}"
  fi
  if [ -n "$json_body" ]; then
    if [ -n "$auth_header" ]; then
      response=$(curl -s -X "$method" -H "$auth_header" -H "Content-Type: application/json" -d "$json_body" -w $'\n%%{http_code}' "$url")
    else
      response=$(curl -s -X "$method" -H "Content-Type: application/json" -d "$json_body" -w $'\n%%{http_code}' "$url")
    fi
  else
    if [ -n "$auth_header" ]; then
      response=$(curl -s -X "$method" -H "$auth_header" -w $'\n%%{http_code}' "$url")
    else
      response=$(curl -s -X "$method" -w $'\n%%{http_code}' "$url")
    fi
  fi

  API_STATUS="${response##*$'\n'}"
  API_BODY="${response%$'\n'*}"

  if [ "$API_STATUS" = "401" ] && [ -z "${DUFFEL_TOKEN:-}" ] && [ "${DUFFEL_AUTH_REQUIRED}" = "1" ]; then
    if refresh_access_token; then
      load_auth_cache
      auth_header="Authorization: Bearer ${ACCESS_TOKEN}"
      if [ -n "$json_body" ]; then
        response=$(curl -s -X "$method" -H "$auth_header" -H "Content-Type: application/json" -d "$json_body" -w $'\n%%{http_code}' "$url")
      else
        response=$(curl -s -X "$method" -H "$auth_header" -w $'\n%%{http_code}' "$url")
      fi
      API_STATUS="${response##*$'\n'}"
      API_BODY="${response%$'\n'*}"
    fi
  fi

  return 0
}

api_request_search() {
  local query="$1"
  shift

  ensure_auth || return 1

  local response
  local auth_header=""
  if [ -n "$AUTH_BEARER" ]; then
    auth_header="Authorization: Bearer ${AUTH_BEARER}"
  fi
  if [ -n "$auth_header" ]; then
    response=$(curl -s -G -H "$auth_header" "$@" --data-urlencode "q=${query}" -w $'\n%%{http_code}' "${DUFFEL_URL}/api/search")
  else
    response=$(curl -s -G "$@" --data-urlencode "q=${query}" -w $'\n%%{http_code}' "${DUFFEL_URL}/api/search")
  fi
  API_STATUS="${response##*$'\n'}"
  API_BODY="${response%$'\n'*}"

  if [ "$API_STATUS" = "401" ] && [ -z "${DUFFEL_TOKEN:-}" ] && [ "${DUFFEL_AUTH_REQUIRED}" = "1" ]; then
    if refresh_access_token; then
      load_auth_cache
      auth_header="Authorization: Bearer ${ACCESS_TOKEN}"
      response=$(curl -s -G -H "$auth_header" "$@" --data-urlencode "q=${query}" -w $'\n%%{http_code}' "${DUFFEL_URL}/api/search")
      API_STATUS="${response##*$'\n'}"
      API_BODY="${response%$'\n'*}"
    fi
  fi

  return 0
}

require_api_success() {
  if [ "$API_STATUS" -ge 200 ] && [ "$API_STATUS" -lt 300 ]; then
    return 0
  fi
  parse_api_error "$API_BODY"
  return 1
}

cmd_auth_login() {
  if [ "${DUFFEL_AUTH_REQUIRED}" = "0" ]; then
    echo "auth is not enabled on this server"
    return 0
  fi

  local state verifier auth_url code response status body now expires_in
  state=$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 20 || true)
  verifier=$(LC_ALL=C tr -dc 'A-Za-z0-9._~-' </dev/urandom | head -c 64 || true)
  if [ -z "$state" ]; then state="state$(date +%%s)"; fi
  if [ -z "$verifier" ]; then verifier="verify$(date +%%s)"; fi

  auth_url="${DUFFEL_URL}/oauth/authorize?response_type=code"
  auth_url+="&client_id=$(urlencode "duffel-cli")"
  auth_url+="&redirect_uri=$(urlencode "urn:ietf:wg:oauth:2.0:oob")"
  auth_url+="&scope=$(urlencode "duffel.full_access")"
  auth_url+="&state=$(urlencode "$state")"
  auth_url+="&code_challenge=$(urlencode "$verifier")"
  auth_url+="&code_challenge_method=plain"

  echo "Open this URL in your browser and sign in:"
  echo "$auth_url"
  printf 'Paste authorization code: '
  read -r code

  if [ -z "$code" ]; then
    echo "error: code is required"
    return 1
  fi

  response=$(curl -s -X POST \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=authorization_code" \
    --data-urlencode "code=${code}" \
    --data-urlencode "client_id=duffel-cli" \
    --data-urlencode "redirect_uri=urn:ietf:wg:oauth:2.0:oob" \
    --data-urlencode "code_verifier=${verifier}" \
    -w $'\n%%{http_code}' \
    "${DUFFEL_URL}/oauth/token")

  status="${response##*$'\n'}"
  body="${response%$'\n'*}"
  if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
    parse_api_error "$body"
    return 1
  fi

  ACCESS_TOKEN=$(json_get_string "access_token" "$body")
  REFRESH_TOKEN=$(json_get_string "refresh_token" "$body")
  expires_in=$(json_get_number "expires_in" "$body")
  if [ -z "$ACCESS_TOKEN" ] || [ -z "$REFRESH_TOKEN" ] || [ -z "$expires_in" ]; then
    echo "error: invalid token response"
    return 1
  fi

  now=$(date +%%s)
  EXPIRES_AT=$((now + expires_in))
  save_auth_cache
  echo "ok"
}

cmd_ls() {
  local path="${1:-/}"
  api_request "GET" "${DUFFEL_URL}/api/fs/${path}"
  require_api_success || return 1

  printf '%%s' "$API_BODY" | grep -o '"name":"[^"]*","isDir":[a-z]*' | while IFS= read -r line; do
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
  api_request "GET" "${DUFFEL_URL}/api/fs/${path}"
  require_api_success || return 1
  printf '%%s\n' "$API_BODY" | sed -n 's/.*"content":"\(.*\)","size":.*/\1/p' | sed 's/\\n/\
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
  local json_content
  json_content=$(printf '%%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%%s", $0}')
  api_request "PUT" "${DUFFEL_URL}/api/fs/${path}" "{\"content\":\"${json_content}\"}"
  require_api_success || return 1
  printf 'ok\n'
}

cmd_rm() {
  local path="$1"
  api_request "DELETE" "${DUFFEL_URL}/api/fs/${path}"
  require_api_success || return 1
  printf 'ok\n'
}

cmd_mkdir() {
  local path="$1"
  api_request "POST" "${DUFFEL_URL}/api/fs/${path}" '{"type":"directory"}'
  require_api_success || return 1
  printf 'ok\n'
}

cmd_mv() {
  local src="$1"
  local dst="$2"
  api_request "POST" "${DUFFEL_URL}/api/move/${src}" "{\"destination\":\"${dst}\"}"
  require_api_success || return 1
  printf 'ok\n'
}

cmd_archive() {
  local path="$1"
  api_request "POST" "${DUFFEL_URL}/api/archive/${path}"
  require_api_success || return 1
  printf 'ok\n'
}

cmd_unarchive() {
  local path="$1"
  api_request "POST" "${DUFFEL_URL}/api/unarchive/${path}"
  require_api_success || return 1
  printf 'ok\n'
}

cmd_journal_create() {
  local path="$1"
  shift
  local content="${*:-}"
  local json_content
  json_content=$(printf '%%s' "$content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\t/\\t/g' | awk '{if(NR>1) printf "\\n"; printf "%%s", $0}')
  api_request "POST" "${DUFFEL_URL}/api/journal/${path}" "{\"content\":\"${json_content}\"}"
  require_api_success || return 1
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
  api_request "POST" "${DUFFEL_URL}/api/journal/${path}/append" "{\"content\":\"${json_content}\"}"
  require_api_success || return 1
  printf 'ok\n'
}

cmd_search() {
  local limit="" offset="" intent="" candidate_limit="" min_score="" explain="" fields=""
  local query_parts=()
  while [ $# -gt 0 ]; do
    case "$1" in
      -n) shift; limit="$1" ;;
      -o) shift; offset="$1" ;;
      --intent) shift; intent="$1" ;;
      -C|--candidate-limit) shift; candidate_limit="$1" ;;
      --min-score) shift; min_score="$1" ;;
      --explain) explain="true" ;;
      --fields) shift; fields="$1" ;;
      --brief) fields="path,title,modified_at,score" ;;
      --paths) fields="path" ;;
      -s|-p|--after|--before)
        echo "error: $1 is no longer supported. Use --intent, -C/--candidate-limit, --min-score, --explain, --fields."
        return 1
        ;;
      *) query_parts+=("$1") ;;
    esac
    shift
  done

  local query="${query_parts[*]}"
  if [ -z "$query" ]; then
    echo "error: search query is required"
    return 1
  fi

  local curl_args=()
  [ -n "$limit" ]  && curl_args+=(--data-urlencode "limit=${limit}")
  [ -n "$offset" ] && curl_args+=(--data-urlencode "offset=${offset}")
  [ -n "$intent" ] && curl_args+=(--data-urlencode "intent=${intent}")
  [ -n "$candidate_limit" ] && curl_args+=(--data-urlencode "candidate_limit=${candidate_limit}")
  [ -n "$min_score" ] && curl_args+=(--data-urlencode "min_score=${min_score}")
  [ -n "$explain" ] && curl_args+=(--data-urlencode "explain=${explain}")
  [ -n "$fields" ] && curl_args+=(--data-urlencode "fields=${fields}")

  api_request_search "$query" "${curl_args[@]}"
  require_api_success || return 1
  printf '%%s\n' "$API_BODY"
}

cmd_find() {
  cmd_search -n 8 --brief "$@"
}

[ $# -lt 1 ] && usage
check_version

case "$1" in
  auth)
    shift
    [ $# -lt 1 ] && usage
    case "$1" in
      login) shift; cmd_auth_login "$@" ;;
      *) usage ;;
    esac
    ;;
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
    case "$1" in
      create) shift; [ $# -lt 1 ] && usage; cmd_journal_create "$@" ;;
      append) shift; [ $# -lt 2 ] && usage; cmd_journal_append "$@" ;;
      *) usage ;;
    esac
    ;;
  find)     shift; [ $# -lt 1 ] && usage; cmd_find "$@" ;;
  search)   shift; [ $# -lt 1 ] && usage; cmd_search "$@" ;;
  *)        usage ;;
esac
`
