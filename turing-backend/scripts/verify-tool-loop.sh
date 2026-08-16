#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

env_value() {
  local name="$1"
  if [[ ! -f .env ]]; then
    return 0
  fi
  awk -F= -v key="$name" '
    $1 == key {
      value = substr($0, index($0, "=") + 1)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      gsub(/^["'\'']|["'\'']$/, "", value)
      print value
      exit
    }
  ' .env
}

configured_ollama_url="${OLLAMA_BASE_URL:-$(env_value OLLAMA_BASE_URL)}"
configured_ollama_url="${configured_ollama_url:-http://localhost:11434}"
ollama_base_url="${TURING_VERIFY_OLLAMA_URL:-${configured_ollama_url/host.docker.internal/localhost}}"
if [[ -n "${TURING_VERIFY_OLLAMA_CONTAINER_URL:-}" ]]; then
  compose_ollama_url="$TURING_VERIFY_OLLAMA_CONTAINER_URL"
else
  compose_ollama_url="$ollama_base_url"
  compose_ollama_url="${compose_ollama_url/127.0.0.1/host.docker.internal}"
  compose_ollama_url="${compose_ollama_url/localhost/host.docker.internal}"
fi
export OLLAMA_BASE_URL="$compose_ollama_url"
if ! curl -sf -m 3 "${ollama_base_url}/api/tags" >/dev/null; then
  echo "Ollama is not reachable at ${ollama_base_url}. It runs on the HOST, not in Compose. Start it and retry." >&2
  exit 2
fi

model="${TURING_VERIFY_MODEL:-${OLLAMA_MODEL:-$(env_value OLLAMA_MODEL)}}"
model="${model:-qwen2.5:7b}"
if ! curl -sf -m 10 \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"${model}\"}" \
  "${ollama_base_url}/api/show" >/dev/null; then
  echo "Ollama model ${model} is not available at ${ollama_base_url}. Pull it and retry." >&2
  exit 2
fi
export TURING_VERIFY_MODEL="$model"

attempts="${TURING_VERIFY_ATTEMPTS:-3}"
if [[ ! "$attempts" =~ ^[1-9][0-9]*$ ]]; then
  echo "TURING_VERIFY_ATTEMPTS must be a positive integer, got: ${attempts}" >&2
  exit 2
fi

if ! ./scripts/init.sh; then
  echo "Could not initialize the verification environment." >&2
  exit 2
fi

compose() {
  ./scripts/compose.sh "$@"
}

client_dir="$(mktemp -d)"
client_bin="${client_dir}/grpc-smoke-client"
compose_started=0
cleanup() {
  status=$?
  if [[ "$compose_started" -eq 1 ]]; then
    compose down >/dev/null 2>&1 || true
  fi
  rm -f "$client_bin"
  rmdir "$client_dir" 2>/dev/null || true
  exit "$status"
}
trap cleanup EXIT

if ! go build -o "$client_bin" ./scripts/grpc-smoke-client.go; then
  echo "Could not build the gRPC verification client." >&2
  exit 2
fi

compose_started=1
if ! compose up --build -d --wait --wait-timeout 60; then
  echo "Compose could not start the verification stack." >&2
  exit 2
fi

ready=0
for _ in $(seq 1 60); do
  if "$client_bin" -health-only; then
    ready=1
    break
  fi
  sleep 1
done

if [[ "$ready" -ne 1 ]]; then
  echo "gRPC health check did not become ready after 60 seconds" >&2
  exit 2
fi

"$client_bin" -model-driven -attempts "$attempts"
