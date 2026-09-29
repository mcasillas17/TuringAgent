#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
./scripts/init.sh
exec ./scripts/compose.sh up --build
