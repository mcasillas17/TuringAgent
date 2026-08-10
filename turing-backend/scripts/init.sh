#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

generate_secret() {
  openssl rand -hex 32
}

generate_client_key() {
  printf 'tk_%s\n' "$(openssl rand -hex 32)"
}

if [[ ! -f .env ]]; then
  (umask 077 && cp .env.example .env)
fi
chmod 600 .env

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

read_var() {
  local name="$1"
  sed -n "s/^${name}=//p" .env | tail -n 1
}

is_positive_id() {
  local value="$1"
  [[ "$value" =~ ^[1-9][0-9]{0,9}$ ]] &&
    ((10#$value <= 2147483647))
}

configure_host_identity() {
  local current_uid="$1"
  local current_gid="$2"
  local mode
  local uid
  local gid
  mode="$(read_var HOST_IDENTITY_MODE)"
  if [[ "$mode" == "manual" ]]; then
    uid="$(read_var HOST_UID)"
    gid="$(read_var HOST_GID)"
    if is_positive_id "$uid" && is_positive_id "$gid"; then
      selected_uid="$uid"
      selected_gid="$gid"
      return
    fi
    printf 'Invalid manual HOST_UID/HOST_GID; using safe fallback 1000:1000.\n' >&2
    uid=1000
    gid=1000
  else
    set_var HOST_IDENTITY_MODE auto
    uid="$current_uid"
    gid="$current_gid"
    if ! is_positive_id "$uid" || ! is_positive_id "$gid"; then
      uid=1000
      gid=1000
    fi
  fi

  set_var HOST_UID "$uid"
  set_var HOST_GID "$gid"
  selected_uid="$uid"
  selected_gid="$gid"
}

provision_sandbox() {
  local current_uid="$1"
  local current_gid="$2"
  local uid="$3"
  local gid="$4"
  local sandbox_path="$PWD/sandbox"

  mkdir -p "$sandbox_path"
  if [[ "$current_uid" == "0" ]]; then
    if ! chown "$uid:$gid" "$sandbox_path"; then
      printf 'Initialization failed: failed to set sandbox ownership to %s:%s.\n' "$uid" "$gid" >&2
      return 1
    fi
    return
  fi
  if [[ "$current_uid" != "$uid" || "$current_gid" != "$gid" ]]; then
    printf 'Initialization failed: cannot safely provision sandbox ownership for %s:%s as %s:%s.\n' \
      "$uid" "$gid" "$current_uid" "$current_gid" >&2
    return 1
  fi
  if [[ ! -O "$sandbox_path" || ! -w "$sandbox_path" ]]; then
    printf 'Initialization failed: sandbox is not owned and writable by the selected host identity.\n' >&2
    return 1
  fi
}

ensure_var TURING_CLIENT_API_KEY "$(generate_client_key)"
ensure_var TURING_INTERNAL_TOKEN "$(generate_secret)"
ensure_var MCP_SYSTEM_TOKEN_GENERAL "$(generate_secret)"
ensure_var MCP_FILES_TOKEN_GENERAL "$(generate_secret)"
ensure_var TURING_APPROVAL_JWT_SECRET "$(generate_secret)"
current_uid="$(id -u 2>/dev/null || true)"
current_gid="$(id -g 2>/dev/null || true)"
selected_uid=
selected_gid=
configure_host_identity "$current_uid" "$current_gid"
rm -f .env.bak
mkdir -p data
provision_sandbox "$current_uid" "$current_gid" "$selected_uid" "$selected_gid"

client_key="$(grep '^TURING_CLIENT_API_KEY=' .env | cut -d= -f2-)"
printf 'TuringAgent backend initialized.\n'
printf 'Flutter client API key: %s\n' "$client_key"
