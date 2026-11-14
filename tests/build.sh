#!/usr/bin/env bash
# Compila o CLI oficial em tests/bin/gitmaster-actions.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd)"
BIN="$ROOT/tests/bin/gitmaster-actions"
mkdir -p -- "$(dirname -- "$BIN")"
bash "$ROOT/scripts/build.sh" --output "$BIN"
[[ -x "$BIN" ]] || { echo "[build] Binário não gerado: $BIN" >&2; exit 1; }
printf '[build] OK: %s\n' "$BIN"
