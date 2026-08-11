#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROTO_DIR="$ROOT/proto"
OUT_DIR="$ROOT/gen/turing/v1"
FLUTTER_OUT_DIR="$ROOT/turing-client/turing_app/lib/generated"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 127
  fi
}

require protoc
require protoc-gen-go
require protoc-gen-go-grpc
require dart

protoc_version="$(protoc --version 2>/dev/null || true)"
if [[ "$protoc_version" != "libprotoc 34.1" ]]; then
  echo "protoc 34.1 is required (found: ${protoc_version:-unknown}); install protoc 34.1 and ensure it is first on PATH" >&2
  exit 1
fi

if [[ -n "${PUB_CACHE:-}" ]]; then
  dart_pub_cache="$PUB_CACHE"
elif [[ "${OS:-}" == "Windows_NT" && -n "${LOCALAPPDATA:-}" ]]; then
  dart_pub_cache="$LOCALAPPDATA/Pub/Cache"
else
  dart_pub_cache="$HOME/.pub-cache"
fi
if [[ "$dart_pub_cache" =~ ^[A-Za-z]:[\\/] ]] && command -v cygpath >/dev/null 2>&1; then
  dart_pub_cache="$(cygpath -u "$dart_pub_cache")"
elif [[ "$dart_pub_cache" != /* ]]; then
  dart_pub_cache="$PWD/$dart_pub_cache"
fi

dart_plugin=""
for candidate in \
  "$dart_pub_cache/bin/protoc-gen-dart" \
  "$dart_pub_cache/bin/protoc-gen-dart.bat" \
  "$dart_pub_cache/bin/protoc-gen-dart.cmd" \
  "$dart_pub_cache/bin/protoc-gen-dart.exe"; do
  if [[ -x "$candidate" || ( "${OS:-}" == "Windows_NT" && -f "$candidate" ) ]]; then
    dart_plugin="$candidate"
    break
  fi
done

if [[ -z "$dart_plugin" ]]; then
  echo "missing required Dart protobuf plugin under $dart_pub_cache/bin; install it with: PUB_CACHE=\"$dart_pub_cache\" dart pub global activate protoc_plugin 22.5.0" >&2
  exit 127
fi

dart_plugin_version="$(PUB_CACHE="$dart_pub_cache" dart pub global list 2>/dev/null | awk '$1 == "protoc_plugin" { print $2; exit }')"
if [[ "$dart_plugin_version" != "22.5.0" ]]; then
  echo "protoc-gen-dart requires protoc_plugin 22.5.0 in $dart_pub_cache (found: ${dart_plugin_version:-not globally activated}); run: PUB_CACHE=\"$dart_pub_cache\" dart pub global activate protoc_plugin 22.5.0" >&2
  exit 1
fi

mkdir -p "$OUT_DIR/go" "$FLUTTER_OUT_DIR"

protoc -I "$PROTO_DIR" \
  --go_out="$OUT_DIR/go" --go_opt=paths=source_relative \
  --go-grpc_out="$OUT_DIR/go" --go-grpc_opt=paths=source_relative \
  "$PROTO_DIR"/turing/v1/*.proto

protoc -I "$PROTO_DIR" --plugin=protoc-gen-dart="$dart_plugin" --dart_out=grpc:"$FLUTTER_OUT_DIR" "$PROTO_DIR"/turing/v1/*.proto
