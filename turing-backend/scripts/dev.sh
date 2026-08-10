#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
./scripts/init.sh
LOG_PRETTY=1 exec ./scripts/compose.sh up --build
