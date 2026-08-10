#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SNAPSHOT="$(mktemp -d "${TMPDIR:-/tmp}/turing-proto-check.XXXXXX")"
trap 'rm -rf -- "$SNAPSHOT"' EXIT

mkdir -p "$SNAPSHOT/turing-client/turing_app/lib"
cp -R "$ROOT/proto" "$SNAPSHOT/proto"
cp -R "$ROOT/gen" "$SNAPSHOT/gen"
cp -R "$ROOT/turing-client/turing_app/lib/generated" "$SNAPSHOT/turing-client/turing_app/lib/generated"

"$ROOT/tools/proto/generate.sh"

changed=0
for path in proto gen turing-client/turing_app/lib/generated; do
  if ! diff -qr "$SNAPSHOT/$path" "$ROOT/$path" >/dev/null; then
    changed=1
    diff -qr "$SNAPSHOT/$path" "$ROOT/$path" >&2 || true
  fi
done

if [[ "$changed" -ne 0 ]]; then
  echo "generated proto output is not deterministic or not committed" >&2
  exit 1
fi
