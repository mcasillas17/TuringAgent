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
  [[ "$1" =~ ^[0-9]+$ && "$1" != "0" ]]
}

configure_host_identity() {
  local mode
  local uid
  local gid
  mode="$(read_var HOST_IDENTITY_MODE)"
  if [[ "$mode" == "manual" ]]; then
    uid="$(read_var HOST_UID)"
    gid="$(read_var HOST_GID)"
    if is_positive_id "$uid" && is_positive_id "$gid"; then
      return
    fi
    printf 'Invalid manual HOST_UID/HOST_GID; using safe fallback 1000:1000.\n' >&2
    set_var HOST_UID 1000
    set_var HOST_GID 1000
    return
  fi

  set_var HOST_IDENTITY_MODE auto
  uid="$(id -u 2>/dev/null || true)"
  gid="$(id -g 2>/dev/null || true)"
  if ! is_positive_id "$uid" || ! is_positive_id "$gid"; then
    uid=1000
    gid=1000
  fi
  set_var HOST_UID "$uid"
  set_var HOST_GID "$gid"
}

ensure_var TURING_CLIENT_API_KEY "$(generate_client_key)"
ensure_var TURING_INTERNAL_TOKEN "$(generate_secret)"
ensure_var MCP_SYSTEM_TOKEN_GENERAL "$(generate_secret)"
ensure_var MCP_FILES_TOKEN_GENERAL "$(generate_secret)"
ensure_var TURING_APPROVAL_JWT_SECRET "$(generate_secret)"
configure_host_identity
rm -f .env.bak
mkdir -p data sandbox

client_key="$(grep '^TURING_CLIENT_API_KEY=' .env | cut -d= -f2-)"
printf 'TuringAgent backend initialized.\n'
printf 'Flutter client API key: %s\n' "$client_key"
