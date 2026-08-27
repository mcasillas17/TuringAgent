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

# env_literal_value reads one setting out of .env the way Compose's own dotenv
# reader does, for the settings this script has to look at itself.
#
# A single-quoted value is literal: no interpolation, and the one escape inside
# it is \' for an apostrophe. That is how init.sh writes a filesystem path, so
# that a checkout under a directory containing $HOME, ${SOMETHING}, # or a space
# reaches Compose as the characters that are actually in the path. A bare value
# is read as-is, which is what an .env written before this did and what an
# operator's hand-edit usually looks like.
env_literal_value() {
  local name="$1"
  local raw
  raw="$(sed -n "s/^${name}=//p" .env | tail -n 1)"
  case "$raw" in
    "'"*"'")
      raw="${raw#\'}"
      raw="${raw%\'}"
      raw="$(printf '%s' "$raw" | sed "s/\\\\'/'/g")"
      ;;
    '"'*'"')
      raw="${raw#\"}"
      raw="${raw%\"}"
      ;;
  esac
  printf '%s\n' "$raw"
}

# env_value_interpolates answers whether Compose will substitute something into
# one recorded value before the stack ever sees it.
#
# Only a single-quoted value is literal. A bare or double-quoted one has $NAME
# and ${NAME} replaced from the environment and from .env itself — so a line
# left by an older install, or typed by hand, can name a variable and arrive as
# whatever that variable holds. For the vault path that is a folder nobody has,
# and when the variable it names is a secret it is that secret on a card in the
# app.
env_value_interpolates() {
  local name="$1"
  local raw
  raw="$(sed -n "s/^${name}=//p" .env | tail -n 1)"
  case "$raw" in
    "'"*"'") return 1 ;;
    *'$'*) return 0 ;;
  esac
  return 1
}

# is_clean_absolute_path answers the same question the orchestrator asks of this
# value when it loads: absolute, and spelled the way the filesystem would spell
# it. A path with a traversal, a doubled separator or a trailing slash is one
# that resolves somewhere other than it reads, and this one exists to be read.
is_clean_absolute_path() {
  local candidate="$1"
  local component
  [[ "$candidate" == /* ]] || return 1
  [[ "$candidate" == "/" || "$candidate" != */ ]] || return 1
  [[ "$candidate" != *//* ]] || return 1
  local rest="${candidate#/}"
  while [[ -n "$rest" ]]; do
    component="${rest%%/*}"
    if [[ "$component" == "." || "$component" == ".." ]]; then
      return 1
    fi
    if [[ "$rest" == */* ]]; then
      rest="${rest#*/}"
    else
      rest=""
    fi
  done
  return 0
}

# validate_memory_display_root is the launch-time half of a requirement the
# compose file used to carry.
#
# It cannot live in the compose file. A ${MEMORY_DISPLAY_ROOT:?...} there is
# evaluated on every subcommand, including the ones a person reaches for when
# an install is broken: `down`, `stop`, `rm`. An .env that predates the setting,
# or has been emptied, or is missing entirely then fails interpolation, and the
# containers stay up — with reset.sh going on to delete the data underneath
# them.
#
# So the requirement is enforced here, on the paths that actually start or
# resolve services, and nowhere else. What it refuses is what the orchestrator
# would refuse or, worse, quietly replace with the container's own /memory: a
# value that is missing, empty, relative, or not the path it appears to be.
#
# The value is never printed. It names a directory on the user's own machine,
# and this script's output is what a person pastes into an issue.
# MEMORY_DISPLAY_ROOT_VALIDATED is what validate_memory_display_root proved and
# what is handed to Compose, because Compose reads the shell environment ahead
# of --env-file: a value exported in somebody's terminal would otherwise win
# over the one this script just checked.
MEMORY_DISPLAY_ROOT_VALIDATED=""

validate_memory_display_root() {
  local configured
  configured="$(env_literal_value MEMORY_DISPLAY_ROOT)"
  if [[ -z "$configured" ]]; then
    printf 'Compose launch failed: MEMORY_DISPLAY_ROOT is not set in .env; run ./scripts/init.sh to record the host vault path.\n' >&2
    return 1
  fi
  if ! is_clean_absolute_path "$configured"; then
    printf 'Compose launch failed: MEMORY_DISPLAY_ROOT must be a clean absolute path; run ./scripts/init.sh to record the host vault path.\n' >&2
    return 1
  fi
  if env_value_interpolates MEMORY_DISPLAY_ROOT; then
    printf 'Compose launch failed: MEMORY_DISPLAY_ROOT is not quoted, so Compose would substitute a variable into the folder the app shows; run ./scripts/init.sh to record the host vault path.\n' >&2
    return 1
  fi
  MEMORY_DISPLAY_ROOT_VALIDATED="$configured"
}

# compose_subcommand finds the verb in an argument list, past Compose's own
# options.
#
# `docker compose` takes its options before the subcommand — `--project-name x
# down`, `-f other.yml stop` — and a person tearing down a broken install may
# well pass one. Reading only the first argument would read those as the
# subcommand and run the launch checks on a teardown.
#
# Options that take a separate value are named so their value is not mistaken
# for the verb. The `=` spelling needs no such handling, and an unknown option
# is skipped rather than guessed at: this classification only decides which
# checks run, and docker itself is what refuses a malformed command line.
compose_subcommand() {
  local skip_value=0
  local argument
  for argument in "$@"; do
    if ((skip_value)); then
      skip_value=0
      continue
    fi
    case "$argument" in
      --ansi | --env-file | --file | --parallel | --profile | \
        --progress | --project-directory | --project-name | -f | -p)
        skip_value=1
        continue
        ;;
      --*=* | -*)
        continue
        ;;
    esac
    printf '%s\n' "$argument"
    return 0
  done
  printf '\n'
}

# is_recovery_command names the subcommands that only ever stop things.
#
# They are the ones that have to work on an install that is already broken, so
# nothing this script checks before launching may stand in their way — not the
# bind sources, which exist to stop a launch mounting something unsafe, and not
# the vault path, which only decides what the client displays. Refusing to stop
# a container because of a mount is refusing to fix the thing being complained
# about.
is_recovery_command() {
  case "$(compose_subcommand "$@")" in
    down | stop | rm | kill) return 0 ;;
    *) return 1 ;;
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
  if ! is_recovery_command "$@"; then
    validate_memory_display_root
    validate_sandbox_bind_source
    validate_skills_bind_source
    validate_memory_bind_source
    validate_mcp_bind_source
    validate_data_bind_source
    exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
      MEMORY_DISPLAY_ROOT="$MEMORY_DISPLAY_ROOT_VALIDATED" \
      docker compose --env-file .env -f infra/docker-compose.yml "$@"
  fi
  exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
    docker compose --env-file .env -f infra/docker-compose.yml "$@"
fi
if ! is_recovery_command "$@"; then
  printf 'Compose launch failed: .env is missing; run ./scripts/init.sh first.\n' >&2
  exit 1
fi

exec env HOST_UID="$current_uid" HOST_GID="$current_gid" \
  docker compose -f infra/docker-compose.yml "$@"
