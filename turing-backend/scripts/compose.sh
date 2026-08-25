#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

is_canonical_non_root_id() {
  local value="$1"
  [[ "$value" =~ ^[1-9][0-9]{0,9}$ ]] &&
    ((10#$value <= 2147483647))
}

is_group_or_world_writable() {
  local path="$1"
  local matches
  matches="$(find "$path" -prune \( -perm -0020 -o -perm -0002 \) -print)"
  [[ -n "$matches" ]]
}

validate_sandbox_bind_source() {
  local sandbox_path="$PWD/sandbox"
  if [[ -L "$sandbox_path" ]]; then
    printf 'Compose launch failed: sandbox must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -d "$sandbox_path" ]]; then
    printf 'Compose launch failed: sandbox must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -O "$sandbox_path" || ! -r "$sandbox_path" || ! -w "$sandbox_path" || ! -x "$sandbox_path" ]]; then
    printf 'Compose launch failed: sandbox is not owned, readable, writable, and traversable by the host user.\n' >&2
    return 1
  fi
  if is_group_or_world_writable "$sandbox_path"; then
    printf 'Compose launch failed: sandbox must not be group- or world-writable.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$sandbox_path")" != "700" ]]; then
    printf 'Compose launch failed: sandbox must have mode 0700.\n' >&2
    return 1
  fi
}

validate_skills_bind_source() {
  local skills_path="$PWD/skills"
  if [[ -L "$skills_path" ]]; then
    printf 'Compose launch failed: skills must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -d "$skills_path" ]]; then
    printf 'Compose launch failed: skills must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -O "$skills_path" || ! -r "$skills_path" || ! -w "$skills_path" || ! -x "$skills_path" ]]; then
    printf 'Compose launch failed: skills is not owned, readable, writable, and traversable by the host user.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$skills_path")" != "700" ]]; then
    printf 'Compose launch failed: skills must have mode 0700.\n' >&2
    return 1
  fi
}

validate_memory_bind_source() {
  local memory_path="$PWD/memory"
  if [[ -L "$memory_path" ]]; then
    printf 'Compose launch failed: memory must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -d "$memory_path" ]]; then
    printf 'Compose launch failed: memory must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -O "$memory_path" || ! -r "$memory_path" || ! -w "$memory_path" || ! -x "$memory_path" ]]; then
    printf 'Compose launch failed: memory is not owned, readable, writable, and traversable by the host user.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$memory_path")" != "700" ]]; then
    printf 'Compose launch failed: memory must have mode 0700.\n' >&2
    return 1
  fi
}

validate_mcp_bind_source() {
  local mcp_path="$PWD/mcp"
  local config_path="$mcp_path/mcp.json"
  if [[ -L "$mcp_path" || ! -d "$mcp_path" ]]; then
    printf 'Compose launch failed: mcp must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -O "$mcp_path" || ! -r "$mcp_path" || ! -x "$mcp_path" || "$(path_mode "$mcp_path")" != "700" ]]; then
    printf 'Compose launch failed: mcp must be owned, readable, traversable, and mode 0700.\n' >&2
    return 1
  fi
  if [[ -e "$config_path" || -L "$config_path" ]]; then
    if [[ -L "$config_path" || ! -f "$config_path" || ! -O "$config_path" || "$(path_mode "$config_path")" != "600" ]]; then
      printf 'Compose launch failed: mcp/mcp.json must be an owned regular file with mode 0600.\n' >&2
      return 1
    fi
  fi
}

path_mode() {
  local path="$1"
  local mode
  if mode="$(stat -c '%a' "$path" 2>/dev/null)"; then
    printf '%s\n' "$mode"
    return 0
  fi
  if mode="$(stat -f '%Lp' "$path" 2>/dev/null)"; then
    printf '%s\n' "$mode"
    return 0
  fi
  return 1
}

configured_database_name() {
  local configured
  configured="$(sed -n 's/^DATABASE_PATH=//p' .env | tail -n 1)"
  if [[ -z "$configured" ]]; then
    configured="/app/data/turing.db"
  fi
  case "$configured" in
    /app/data/*)
      configured="${configured#/app/data/}"
      if [[ -n "$configured" && "$configured" != */* ]]; then
        printf '%s\n' "$configured"
      fi
      ;;
  esac
}

validate_database_file() {
  local database_path="$1"
  local relative="${database_path#"$PWD/data/"}"
  if [[ ! -e "$database_path" && ! -L "$database_path" ]]; then
    return 0
  fi
  if [[ -L "$database_path" ]]; then
    printf 'Compose launch failed: database file must be a regular file, not a symlink: %s\n' \
      "$relative" >&2
    return 1
  fi
  if [[ ! -f "$database_path" ]]; then
    printf 'Compose launch failed: database file must be a regular file: %s\n' "$relative" >&2
    return 1
  fi
  if [[ ! -O "$database_path" ]]; then
    printf 'Compose launch failed: database file is not owned by the host user: %s\n' "$relative" >&2
    return 1
  fi
  if [[ "$(path_mode "$database_path")" != "600" ]]; then
    printf 'Compose launch failed: database file must have mode 0600: %s\n' "$relative" >&2
    return 1
  fi
}

validate_data_bind_source() {
  local data_path="$PWD/data"
  local database_name
  local database_path

  if [[ -L "$data_path" ]]; then
    printf 'Compose launch failed: data must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -d "$data_path" ]]; then
    printf 'Compose launch failed: data must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -O "$data_path" || ! -r "$data_path" || ! -w "$data_path" || ! -x "$data_path" ]]; then
    printf 'Compose launch failed: data is not owned, readable, writable, and traversable by the host user.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$data_path")" != "700" ]]; then
    printf 'Compose launch failed: data must have mode 0700.\n' >&2
    return 1
  fi

  for database_path in \
    "$data_path"/*.db "$data_path"/*.db-journal "$data_path"/*.db-shm "$data_path"/*.db-wal \
    "$data_path"/*.sqlite "$data_path"/*.sqlite-journal "$data_path"/*.sqlite-shm "$data_path"/*.sqlite-wal \
    "$data_path"/*.sqlite3 "$data_path"/*.sqlite3-journal "$data_path"/*.sqlite3-shm "$data_path"/*.sqlite3-wal; do
    validate_database_file "$database_path"
  done
  database_name="$(configured_database_name)"
  if [[ -n "$database_name" ]]; then
    validate_database_file "$data_path/$database_name"
    validate_database_file "$data_path/$database_name-journal"
    validate_database_file "$data_path/$database_name-shm"
    validate_database_file "$data_path/$database_name-wal"
  fi
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
  if [[ "${1:-}" != "down" ]]; then
    validate_sandbox_bind_source
    validate_skills_bind_source
    validate_memory_bind_source
    validate_mcp_bind_source
    validate_data_bind_source
  fi
  exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
    docker compose --env-file .env -f infra/docker-compose.yml "$@"
fi
if [[ "${1:-}" != "down" ]]; then
  printf 'Compose launch failed: .env is missing; run ./scripts/init.sh first.\n' >&2
  exit 1
fi

exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
  docker compose -f infra/docker-compose.yml "$@"
