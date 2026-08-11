#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
./scripts/init.sh

compose() {
  ./scripts/compose.sh "$@"
}

compose up --build -d --wait --wait-timeout 60
trap 'compose down' EXIT

ready=0
for _ in $(seq 1 60); do
  if go run ./scripts/grpc-smoke-client.go -health-only; then
    ready=1
    break
  fi
  sleep 1
done

if [[ "$ready" -ne 1 ]]; then
  echo "gRPC health check did not become ready after 60 seconds" >&2
  exit 1
fi

go run ./scripts/grpc-smoke-client.go
