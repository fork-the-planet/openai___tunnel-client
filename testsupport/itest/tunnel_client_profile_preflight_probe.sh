#!/usr/bin/env bash

set -euo pipefail

if (($# < 3)); then
  printf 'usage: %s <client-path> <profile-file> <preflight-base-url> [client-args...]\n' "$0" >&2
  exit 1
fi

client_path="$1"
profile_file="$2"
preflight_base_url="$3"
shift 3

startup_timeout_seconds="${STARTUP_TIMEOUT_SECONDS:-120.0}"
client_pid=""
tmpdir=""

cleanup() {
  if [[ -n "$client_pid" ]] && kill -0 "$client_pid" 2>/dev/null; then
    kill "$client_pid" 2>/dev/null || true
    wait "$client_pid" 2>/dev/null || true
  fi
  if [[ -n "$tmpdir" && -d "$tmpdir" ]]; then
    rm -rf "$tmpdir"
  fi
}

profile_tunnel_id() {
  local path="$1"

  awk '
    $1 == "control_plane:" { in_control_plane = 1; next }
    in_control_plane && $0 ~ /^[^[:space:]]/ { in_control_plane = 0 }
    in_control_plane && $1 == "tunnel_id:" { print $2; found = 1; exit }
    END {
      if (!found) {
        exit 1
      }
    }
  ' "$path"
}

request_ok() {
  local url="$1"
  local timeout_seconds="$2"
  shift 2

  local -a curl_args=(
    --silent
    --show-error
    --output /dev/null
    --write-out '%{http_code}'
    --max-time "$timeout_seconds"
  )
  while (($# > 0)); do
    curl_args+=(-H "$1")
    shift
  done

  local status=""
  status="$(curl "${curl_args[@]}" "$url" 2>/dev/null || true)"
  [[ "$status" == 2* ]]
}

wait_for_running_process() {
  local description="$1"

  if kill -0 "$client_pid" 2>/dev/null; then
    return 0
  fi

  wait "$client_pid" || true
  printf 'tunnel-client exited before %s\n' "$description" >&2
  exit 1
}

trap cleanup EXIT

tunnel_id="$(profile_tunnel_id "$profile_file")" || {
  printf 'Missing control_plane.tunnel_id in %s\n' "$profile_file" >&2
  exit 1
}
preflight_url="${preflight_base_url%/}/.well-known/oauth-protected-resource/v1/mcp/${tunnel_id}"

timeout_ceiling="${startup_timeout_seconds%.*}"
if [[ "$startup_timeout_seconds" == *.* ]]; then
  timeout_ceiling="$((timeout_ceiling + 1))"
fi
if [[ "$timeout_ceiling" -le 0 ]]; then
  timeout_ceiling=1
fi

tmp_parent="${TMPDIR:-${TEST_TMPDIR:-/tmp}}"
mkdir -p "$tmp_parent"
tmpdir="$(mktemp -d "$tmp_parent/tunnel-client-preflight-XXXXXX")"
health_url_file="$tmpdir/tunnel-client-health-url"

command=(
  "$client_path"
  run
  "--profile-file=$profile_file"
  "--health.listen-addr=127.0.0.1:0"
  "--health.url-file=$health_url_file"
)

if (($# > 0)); then
  command+=("$@")
fi

"${command[@]}" &
client_pid="$!"

deadline="$((SECONDS + timeout_ceiling))"
health_url=""
while ((SECONDS < deadline)); do
  wait_for_running_process "becoming ready"
  if [[ -f "$health_url_file" ]]; then
    candidate="$(<"$health_url_file")"
    candidate="${candidate//$'\n'/}"
    if [[ -n "$candidate" ]]; then
      health_url="$candidate"
      if request_ok "${candidate%/}/readyz" 1.0; then
        break
      fi
    fi
  fi
  sleep 0.2
done

if [[ -z "$health_url" ]] || ! request_ok "${health_url%/}/readyz" 1.0; then
  printf 'tunnel-client did not become ready within %ss\n' "$startup_timeout_seconds" >&2
  exit 1
fi

preflight_deadline="$((SECONDS + timeout_ceiling))"
while ((SECONDS < preflight_deadline)); do
  wait_for_running_process "preflight succeeding"
  if request_ok "$preflight_url" 2.0 "X-OPENAI-SKIP-AUTH: true"; then
    exit 0
  fi
  sleep 0.2
done

printf 'tunnel-client preflight did not succeed before timeout\n' >&2
exit 1
