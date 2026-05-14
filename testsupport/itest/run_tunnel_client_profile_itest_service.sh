#!/usr/bin/env bash

set -euo pipefail

client_path=""
profile_file=""
declare -a client_args=()
declare -a runfile_env=()

resolve_runfile_path() {
  local value="$1"

  if [[ "$value" = /* && -e "$value" ]]; then
    printf '%s\n' "$value"
    return 0
  fi

  if [[ -e "$value" ]]; then
    printf '%s\n' "$value"
    return 0
  fi

  local base_dir=""
  for base_dir in "${RUNFILES_DIR:-}" "${TEST_SRCDIR:-}"; do
    if [[ -n "$base_dir" && -e "$base_dir/$value" ]]; then
      printf '%s\n' "$base_dir/$value"
      return 0
    fi
  done

  printf 'Could not resolve runfile: %s\n' "$value" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --client-path)
      client_path="$2"
      shift 2
      ;;
    --profile-file)
      profile_file="$2"
      shift 2
      ;;
    --set-env)
      export "$2=$3"
      shift 3
      ;;
    --runfile-env)
      runfile_env+=("$2")
      shift 2
      ;;
    --)
      shift
      client_args=("$@")
      break
      ;;
    *)
      printf 'Unexpected argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$client_path" || -z "$profile_file" ]]; then
  printf 'Both --client-path and --profile-file are required\n' >&2
  exit 1
fi

if ((${#runfile_env[@]} > 0)); then
  for assignment in "${runfile_env[@]}"; do
    env_name="${assignment%%=*}"
    env_value="${assignment#*=}"
    if [[ -z "$env_name" || "$env_name" == "$assignment" ]]; then
      printf 'Invalid --runfile-env assignment: %s\n' "$assignment" >&2
      exit 1
    fi
    export "$env_name=$(resolve_runfile_path "$env_value")"
  done
fi

resolved_client_path="$(resolve_runfile_path "$client_path")"
resolved_profile_path="$(resolve_runfile_path "$profile_file")"

command=(
  "$resolved_client_path"
  run
  "--profile-file=$resolved_profile_path"
)

if ((${#client_args[@]} > 0)); then
  command+=("${client_args[@]}")
fi

exec "${command[@]}"
