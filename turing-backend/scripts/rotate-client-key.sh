#!/usr/bin/env bash
# Kept so existing habits work; the key is rotated by `./turing connect --rotate-key`.
set -euo pipefail
exec "$(dirname "$0")/../turing" connect --rotate-key "$@"
