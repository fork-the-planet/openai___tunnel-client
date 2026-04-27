#!/usr/bin/env bash
set -euo pipefail

readonly PUBLIC_BUCKET_PREFIX="tunnel-client"
readonly PUBLIC_BASE_URL_ROOT="https://persistent.oaistatic.com"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/release_tag.sh make <version> <word>
  ./scripts/release_tag.sh parse <tag>

Examples:
  ./scripts/release_tag.sh make 0.3.1 ember-orchid
  ./scripts/release_tag.sh parse v0.3.1--ember-orchid
  ./scripts/release_tag.sh parse v0.3.1-rc.1--ember-orchid
EOF
}

die() {
  echo "release_tag.sh: $*" >&2
  exit 1
}

version_re='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
word_re='^[a-z0-9]+(-[a-z0-9]+)*$'

validate_version() {
  local version="$1"
  [[ "$version" =~ $version_re ]] || die "version must be a semver like 1.2.3 or 1.2.3-rc.1"
}

validate_word() {
  local word="$1"
  [[ "$word" =~ $word_re ]] || die "word must contain only lowercase letters, digits, and single hyphen separators"
}

make_tag() {
  local version="$1"
  local word="$2"
  validate_version "$version"
  validate_word "$word"
  printf 'v%s--%s\n' "$version" "$word"
}

parse_tag() {
  local tag="$1"
  local version_prefix word version prerelease public_blob_path public_base_url

  [[ "$tag" == v*--* ]] || die "tag must look like v<semver>--<word>"

  version_prefix="${tag%%--*}"
  word="${tag#*--}"
  version="${version_prefix#v}"

  [[ -n "$version" && -n "$word" ]] || die "tag must include both a version and a word"
  validate_version "$version"
  validate_word "$word"

  prerelease=false
  if [[ "$version" == *-* ]]; then
    prerelease=true
  fi

  public_blob_path="${PUBLIC_BUCKET_PREFIX}/${tag}"
  public_base_url="${PUBLIC_BASE_URL_ROOT}/${public_blob_path}"

  printf 'release_tag=%s\n' "$tag"
  printf 'release_version=%s\n' "$version"
  printf 'release_word=%s\n' "$word"
  printf 'prerelease=%s\n' "$prerelease"
  printf 'public_blob_path=%s\n' "$public_blob_path"
  printf 'public_base_url=%s\n' "$public_base_url"
}

main() {
  local command="${1:-}"
  case "$command" in
    make)
      [[ $# -eq 3 ]] || {
        usage >&2
        exit 1
      }
      make_tag "$2" "$3"
      ;;
    parse)
      [[ $# -eq 2 ]] || {
        usage >&2
        exit 1
      }
      parse_tag "$2"
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
