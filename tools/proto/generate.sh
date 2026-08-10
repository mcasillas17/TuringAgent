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

if ! command -v protoc-gen-dart >/dev/null 2>&1; then
  echo "missing required tool: protoc-gen-dart; install it with: dart pub global activate protoc_plugin 22.5.0" >&2
  exit 127
fi

dart_plugin_version="$(dart pub global list 2>/dev/null | awk '$1 == "protoc_plugin" { print $2; exit }')"
if [[ "$dart_plugin_version" != "22.5.0" ]]; then
  echo "protoc-gen-dart requires protoc_plugin 22.5.0 (found: ${dart_plugin_version:-not globally activated}); run: dart pub global activate protoc_plugin 22.5.0" >&2
  exit 1
fi

mkdir -p "$OUT_DIR/go" "$OUT_DIR/dart" "$OUT_DIR/swift" "$OUT_DIR/csharp" "$OUT_DIR/kotlin" "$FLUTTER_OUT_DIR"

protoc -I "$PROTO_DIR" \
  --go_out="$OUT_DIR/go" --go_opt=paths=source_relative \
  --go-grpc_out="$OUT_DIR/go" --go-grpc_opt=paths=source_relative \
  "$PROTO_DIR"/turing/v1/*.proto

protoc -I "$PROTO_DIR" --dart_out=grpc:"$FLUTTER_OUT_DIR" "$PROTO_DIR"/turing/v1/*.proto

if command -v protoc-gen-swift >/dev/null 2>&1 && command -v protoc-gen-grpc-swift >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --swift_out="$OUT_DIR/swift" --grpc-swift_out="$OUT_DIR/swift" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "Swift protoc plugins not installed; skipping Swift generation" >&2
fi

if command -v grpc_csharp_plugin >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --csharp_out="$OUT_DIR/csharp" --grpc_out="$OUT_DIR/csharp" --plugin=protoc-gen-grpc="$(command -v grpc_csharp_plugin)" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "grpc_csharp_plugin not installed; skipping C# generation" >&2
fi

if command -v protoc-gen-grpc-java >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --java_out="$OUT_DIR/kotlin" --grpc-java_out="$OUT_DIR/kotlin" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "protoc-gen-grpc-java not installed; skipping Android Java/Kotlin-compatible generation" >&2
fi
