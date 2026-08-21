#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

generate_secret() {
  openssl rand -hex 32
}

generate_client_key() {
  printf 'tk_%s\n' "$(openssl rand -hex 32)"
}

ensure_var() {
  local name="$1"
  local value="$2"
  if ! grep -q "^${name}=" .env || grep -q "^${name}=$" .env; then
    if grep -q "^${name}=" .env; then
      sed -i.bak "s|^${name}=.*|${name}=${value}|" .env
    else
      printf '%s=%s\n' "$name" "$value" >> .env
    fi
  fi
}

set_var() {
  local name="$1"
  local value="$2"
  if grep -q "^${name}=" .env; then
    sed -i.bak "s|^${name}=.*|${name}=${value}|" .env
  else
    printf '%s=%s\n' "$name" "$value" >> .env
  fi
}

is_positive_id() {
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

validate_env_file() {
  if [[ -L .env ]]; then
    printf 'Initialization failed: .env must be a regular file, not a symlink.\n' >&2
    return 1
  fi
  if [[ -e .env && ! -f .env ]]; then
    printf 'Initialization failed: .env must be a regular file.\n' >&2
    return 1
  fi
}

configure_host_identity() {
  local current_uid="$1"
  local current_gid="$2"
  set_var HOST_IDENTITY_MODE auto
  set_var HOST_UID "$current_uid"
  set_var HOST_GID "$current_gid"
}

validate_sandbox_entries() {
  local directory="$1"
  local sandbox_path="$2"
  local entry
  local relative

  for entry in "$directory"/* "$directory"/.[!.]* "$directory"/..?*; do
    if [[ ! -e "$entry" && ! -L "$entry" ]]; then
      continue
    fi
    if [[ -L "$entry" ]]; then
      continue
    fi
    relative="${entry#"$sandbox_path"/}"
    if [[ -d "$entry" ]]; then
      if is_group_or_world_writable "$entry"; then
        printf 'Initialization failed: legacy sandbox directory must not be group- or world-writable: %s\n' \
          "$relative" >&2
        return 1
      fi
      if [[ ! -O "$entry" || ! -r "$entry" || ! -w "$entry" || ! -x "$entry" ]]; then
        printf 'Initialization failed: legacy sandbox directory is not readable, writable, and traversable: %s\n' \
          "$relative" >&2
        return 1
      fi
      if ! validate_sandbox_entries "$entry" "$sandbox_path"; then
        return 1
      fi
    elif [[ -f "$entry" ]]; then
      if is_group_or_world_writable "$entry"; then
        printf 'Initialization failed: legacy sandbox file must not be group- or world-writable: %s\n' \
          "$relative" >&2
        return 1
      fi
      if [[ ! -O "$entry" || ! -r "$entry" || ! -w "$entry" ]]; then
        printf 'Initialization failed: legacy sandbox file is not readable and writable: %s\n' \
          "$relative" >&2
        return 1
      fi
    fi
  done
}

provision_sandbox() {
  local sandbox_path="$PWD/sandbox"

  if [[ -L "$sandbox_path" ]]; then
    printf 'Initialization failed: sandbox must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ -e "$sandbox_path" && ! -d "$sandbox_path" ]]; then
    printf 'Initialization failed: sandbox must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -e "$sandbox_path" ]] && ! (umask 077 && mkdir -m 0700 -- "$sandbox_path"); then
    printf 'Initialization failed: could not create sandbox directory.\n' >&2
    return 1
  fi
  if [[ -L "$sandbox_path" || ! -d "$sandbox_path" ]]; then
    printf 'Initialization failed: sandbox must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -O "$sandbox_path" || ! -r "$sandbox_path" || ! -w "$sandbox_path" || ! -x "$sandbox_path" ]]; then
    printf 'Initialization failed: sandbox is not owned, readable, writable, and traversable by the host user.\n' >&2
    return 1
  fi
  if is_group_or_world_writable "$sandbox_path"; then
    printf 'Initialization failed: sandbox must not be group- or world-writable.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$sandbox_path")" != "700" ]] && ! chmod 0700 "$sandbox_path"; then
    printf 'Initialization failed: could not secure sandbox directory.\n' >&2
    return 1
  fi
  validate_sandbox_entries "$sandbox_path" "$sandbox_path"
}

provision_skills() {
  local skills_path="$PWD/skills"

  if [[ -L "$skills_path" ]]; then
    printf 'Initialization failed: skills must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ -e "$skills_path" && ! -d "$skills_path" ]]; then
    printf 'Initialization failed: skills must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -e "$skills_path" ]] && ! (umask 077 && mkdir -m 0700 -- "$skills_path"); then
    printf 'Initialization failed: could not create skills directory.\n' >&2
    return 1
  fi
  if [[ -L "$skills_path" || ! -d "$skills_path" ]]; then
    printf 'Initialization failed: skills must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -O "$skills_path" || ! -r "$skills_path" || ! -w "$skills_path" || ! -x "$skills_path" ]]; then
    printf 'Initialization failed: skills is not owned, readable, writable, and traversable by the host user.\n' >&2
    return 1
  fi
  if ! chmod 0700 "$skills_path"; then
    printf 'Initialization failed: could not secure skills directory.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$skills_path")" != "700" ]]; then
    printf 'Initialization failed: skills must have mode 0700.\n' >&2
    return 1
  fi
}

provision_mcp_config() {
  local mcp_path="$PWD/mcp"
  local config_path="$mcp_path/mcp.json"

  if [[ -L "$mcp_path" ]]; then
    printf 'Initialization failed: mcp must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ -e "$mcp_path" && ! -d "$mcp_path" ]]; then
    printf 'Initialization failed: mcp must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -e "$mcp_path" ]] && ! (umask 077 && mkdir -m 0700 -- "$mcp_path"); then
    printf 'Initialization failed: could not create mcp directory.\n' >&2
    return 1
  fi
  if [[ ! -O "$mcp_path" ]] || ! chmod 0700 "$mcp_path"; then
    printf 'Initialization failed: mcp must be owned by the host user and securable.\n' >&2
    return 1
  fi
  if [[ -e "$config_path" || -L "$config_path" ]]; then
    if [[ -L "$config_path" || ! -f "$config_path" || ! -O "$config_path" ]]; then
      printf 'Initialization failed: mcp/mcp.json must be an owned regular file, not a symlink.\n' >&2
      return 1
    fi
    if ! chmod 0600 "$config_path" || [[ "$(path_mode "$config_path")" != "600" ]]; then
      printf 'Initialization failed: mcp/mcp.json must have mode 0600.\n' >&2
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

secure_database_file() {
  local database_path="$1"
  local relative="${database_path#"$PWD/data/"}"
  if [[ ! -e "$database_path" && ! -L "$database_path" ]]; then
    return 0
  fi
  if [[ -L "$database_path" ]]; then
    printf 'Initialization failed: database file must be a regular file, not a symlink: %s\n' \
      "$relative" >&2
    return 1
  fi
  if [[ ! -f "$database_path" ]]; then
    printf 'Initialization failed: database file must be a regular file: %s\n' "$relative" >&2
    return 1
  fi
  if [[ ! -O "$database_path" ]]; then
    printf 'Initialization failed: database file is not owned by the host user: %s\n' "$relative" >&2
    return 1
  fi
  if ! chmod 0600 "$database_path"; then
    printf 'Initialization failed: could not secure database file: %s\n' "$relative" >&2
    return 1
  fi
  if [[ "$(path_mode "$database_path")" != "600" ]]; then
    printf 'Initialization failed: database file must have mode 0600: %s\n' "$relative" >&2
    return 1
  fi
}

provision_data() {
  local data_path="$PWD/data"
  local database_name
  local database_path

  if [[ -L "$data_path" ]]; then
    printf 'Initialization failed: data must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ -e "$data_path" && ! -d "$data_path" ]]; then
    printf 'Initialization failed: data must be a real directory.\n' >&2
    return 1
  fi
  if [[ ! -e "$data_path" ]] && ! (umask 077 && mkdir -m 0700 -- "$data_path"); then
    printf 'Initialization failed: could not create data directory.\n' >&2
    return 1
  fi
  if [[ -L "$data_path" || ! -d "$data_path" ]]; then
    printf 'Initialization failed: data must be a real directory, not a symlink.\n' >&2
    return 1
  fi
  if [[ ! -O "$data_path" ]]; then
    printf 'Initialization failed: data is not owned by the host user.\n' >&2
    return 1
  fi
  if ! chmod 0700 "$data_path"; then
    printf 'Initialization failed: could not secure data directory.\n' >&2
    return 1
  fi
  if [[ "$(path_mode "$data_path")" != "700" || ! -r "$data_path" || ! -w "$data_path" || ! -x "$data_path" ]]; then
    printf 'Initialization failed: data must have mode 0700 and be accessible by the host user.\n' >&2
    return 1
  fi

  for database_path in \
    "$data_path"/*.db "$data_path"/*.db-journal "$data_path"/*.db-shm "$data_path"/*.db-wal \
    "$data_path"/*.sqlite "$data_path"/*.sqlite-journal "$data_path"/*.sqlite-shm "$data_path"/*.sqlite-wal \
    "$data_path"/*.sqlite3 "$data_path"/*.sqlite3-journal "$data_path"/*.sqlite3-shm "$data_path"/*.sqlite3-wal; do
    secure_database_file "$database_path"
  done
  database_name="$(configured_database_name)"
  if [[ -n "$database_name" ]]; then
    secure_database_file "$data_path/$database_name"
    secure_database_file "$data_path/$database_name-journal"
    secure_database_file "$data_path/$database_name-shm"
    secure_database_file "$data_path/$database_name-wal"
  fi
}

current_uid="$(id -u 2>/dev/null || true)"
current_gid="$(id -g 2>/dev/null || true)"
if ! is_positive_id "$current_uid" || ! is_positive_id "$current_gid"; then
  printf 'Initialization failed: init.sh must be run by a non-root host user with canonical UID/GID values.\n' >&2
  exit 1
fi
provision_sandbox
provision_skills
provision_mcp_config

validate_env_file
if [[ ! -e .env ]]; then
  (umask 077 && cp .env.example .env)
fi
validate_env_file
chmod 600 .env
validate_env_file

ensure_var TURING_CLIENT_API_KEY "$(generate_client_key)"
# Separate least-privilege identities: the runtime (agent-runtime-go) may
# claim jobs and read session history; the approval consumer (mcp-files) may
# only consume approvals. A shared secret here would let a compromised
# approval consumer present the runtime's own credential and reach methods it
# has no business calling.
ensure_var TURING_RUNTIME_TOKEN "$(generate_secret)"
ensure_var TURING_APPROVAL_CONSUMER_TOKEN "$(generate_secret)"
ensure_var TURING_MCP_FILES_CLEANUP_TOKEN "$(generate_secret)"
ensure_var MCP_SYSTEM_TOKEN_GENERAL "$(generate_secret)"
ensure_var MCP_FILES_TOKEN_GENERAL "$(generate_secret)"
ensure_var TURING_APPROVAL_JWT_SECRET "$(generate_secret)"
ensure_var TURING_EGRESS_SIGNING_SECRET "$(generate_secret)"
ensure_var TURING_CURSOR_HMAC_SECRET "$(generate_secret)"
ensure_var TURING_INTEGRATION_KEY "$(generate_secret)"
configure_host_identity "$current_uid" "$current_gid"
provision_data
rm -f .env.bak

client_key="$(grep '^TURING_CLIENT_API_KEY=' .env | cut -d= -f2-)"
printf 'TuringAgent backend initialized.\n'
printf 'Flutter client API key: %s\n' "$client_key"
