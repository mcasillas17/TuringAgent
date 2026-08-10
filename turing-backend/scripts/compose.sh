#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

is_canonical_non_root_id() {
  local value="$1"
  [[ "$value" =~ ^[1-9][0-9]{0,9}$ ]] &&
    ((10#$value <= 2147483647))
}

current_uid="$(id -u 2>/dev/null || true)"
current_gid="$(id -g 2>/dev/null || true)"
if ! is_canonical_non_root_id "$current_uid" || ! is_canonical_non_root_id "$current_gid"; then
  printf 'Compose launch failed: scripts/compose.sh must be run by a non-root host user with canonical UID/GID values.\n' >&2
  exit 1
fi
if [[ "${1:-}" == "--validate-host-identity" ]]; then
  exit 0
fi
if [[ -f .env ]]; then
  exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
    docker compose --env-file .env -f infra/docker-compose.yml "$@"
fi
if [[ "${1:-}" != "down" ]]; then
  printf 'Compose launch failed: .env is missing; run ./scripts/init.sh first.\n' >&2
  exit 1
fi

exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
  docker compose -f infra/docker-compose.yml "$@"
