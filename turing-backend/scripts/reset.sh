#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
./scripts/compose.sh --validate-host-identity
read -r -p "Delete TuringAgent local data and regenerate .env? Type RESET: " answer
if [[ "$answer" != "RESET" ]]; then
  echo "Reset cancelled."
  exit 1
fi
./scripts/compose.sh down --remove-orphans || true
rm -rf data .runtime .env
mkdir -p data
./scripts/init.sh
touch data/.gitkeep sandbox/.gitkeep
