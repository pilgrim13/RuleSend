#!/usr/bin/env bash
set -euo pipefail

rulesend_repo_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
rulesend_data_dir="${RULESEND_PROTOTYPE_DATA:-${XDG_DATA_HOME:-$HOME/.local/share}/rulesend-prototype}"
rulesend_listen="${RULESEND_PROTOTYPE_LISTEN:-127.0.0.1:8787}"
rulesend_go_bin="${RULESEND_GO_BIN:-go}"

cd "$rulesend_repo_dir"
if ! command -v "$rulesend_go_bin" >/dev/null 2>&1; then
  echo "Go 1.25 or newer is required. Set RULESEND_GO_BIN if Go is not on PATH." >&2
  exit 1
fi
mkdir -p dist
"$rulesend_go_bin" build -trimpath -o dist/rulesend ./cmd/rulesend
./dist/rulesend hub init --data "$rulesend_data_dir"
printf '\nStarting Prototype 1. Press Ctrl+C to stop the Hub.\n'
exec ./dist/rulesend hub serve --data "$rulesend_data_dir" --listen "$rulesend_listen"
