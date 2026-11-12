#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
git rev-parse --is-inside-work-tree >/dev/null
git config --local core.hooksPath scripts/hooks
printf 'Hook pre-commit habilitado neste clone (scripts/hooks).\n'