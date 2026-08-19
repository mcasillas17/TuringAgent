#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REQUIRED_BUF_VERSION="1.72.0"
BASE_REF="${1:-${PROTO_BREAKING_BASE_REF:-origin/main}}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 127
  fi
}

require git
require buf

buf_version="$(buf --version 2>/dev/null || true)"
if [[ "$buf_version" != "$REQUIRED_BUF_VERSION" ]]; then
  echo "buf $REQUIRED_BUF_VERSION is required (found: ${buf_version:-unknown})" >&2
  exit 1
fi

if [[ ! "$BASE_REF" =~ ^([A-Za-z0-9][A-Za-z0-9._-]*)/(.+)$ ]]; then
  echo "base ref must be a valid remote-tracking ref such as origin/main (found: $BASE_REF)" >&2
  exit 2
fi
remote="${BASH_REMATCH[1]}"
branch="${BASH_REMATCH[2]}"

if ! git check-ref-format --branch "$branch" >/dev/null 2>&1; then
  echo "base ref must be a valid remote-tracking ref such as origin/main (found: $BASE_REF)" >&2
  exit 2
fi
if ! git -C "$ROOT" remote get-url "$remote" >/dev/null 2>&1; then
  echo "base ref remote is not configured: $remote" >&2
  exit 2
fi

fetch_args=(--no-tags)
if [[ "$(git -C "$ROOT" rev-parse --is-shallow-repository)" == "true" ]]; then
  fetch_args+=(--depth=1)
fi
if ! git -C "$ROOT" fetch "${fetch_args[@]}" "$remote" \
  "refs/heads/$branch:refs/remotes/$remote/$branch"; then
  echo "failed to refresh base ref $BASE_REF; verify the remote and branch are reachable" >&2
  exit 1
fi

if ! base_commit="$(git -C "$ROOT" rev-parse --verify "refs/remotes/$remote/$branch^{commit}" 2>/dev/null)"; then
  echo "failed to resolve refreshed base ref $BASE_REF" >&2
  exit 1
fi

buf breaking "$ROOT/proto" \
  --against "$ROOT#format=git,ref=$base_commit,subdir=proto"
