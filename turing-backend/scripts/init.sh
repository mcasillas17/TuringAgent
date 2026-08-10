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
      if [[ ! -O "$entry" || ! -r "$entry" || ! -w "$entry" || ! -x "$entry" ]]; then
        printf 'Initialization failed: legacy sandbox directory is not readable, writable, and traversable: %s\n' \
          "$relative" >&2
        return 1
      fi
      if ! validate_sandbox_entries "$entry" "$sandbox_path"; then
        return 1
      fi
    elif [[ -f "$entry" ]] && {
      [[ ! -O "$entry" ]] || [[ ! -r "$entry" ]] || [[ ! -w "$entry" ]]
    }; then
      printf 'Initialization failed: legacy sandbox file is not readable and writable: %s\n' \
        "$relative" >&2
      return 1
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
  if [[ ! -e "$sandbox_path" ]] && ! mkdir -- "$sandbox_path"; then
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
  validate_sandbox_entries "$sandbox_path" "$sandbox_path"
}

current_uid="$(id -u 2>/dev/null || true)"
current_gid="$(id -g 2>/dev/null || true)"
if ! is_positive_id "$current_uid" || ! is_positive_id "$current_gid"; then
  printf 'Initialization failed: init.sh must be run by a non-root host user with canonical UID/GID values.\n' >&2
  exit 1
fi
provision_sandbox

if [[ ! -f .env ]]; then
  (umask 077 && cp .env.example .env)
fi
chmod 600 .env

ensure_var TURING_CLIENT_API_KEY "$(generate_client_key)"
ensure_var TURING_INTERNAL_TOKEN "$(generate_secret)"
ensure_var MCP_SYSTEM_TOKEN_GENERAL "$(generate_secret)"
ensure_var MCP_FILES_TOKEN_GENERAL "$(generate_secret)"
ensure_var TURING_APPROVAL_JWT_SECRET "$(generate_secret)"
configure_host_identity "$current_uid" "$current_gid"
rm -f .env.bak
mkdir -p data

client_key="$(grep '^TURING_CLIENT_API_KEY=' .env | cut -d= -f2-)"
printf 'TuringAgent backend initialized.\n'
printf 'Flutter client API key: %s\n' "$client_key"
