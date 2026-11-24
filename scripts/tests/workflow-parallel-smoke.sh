#!/usr/bin/env bash
# Teste INTEGRAL no host com QEMU/Libvirt (não é executado no runner sem KVM).
# Uso:
#   bash scripts/tests/workflow-parallel-smoke.sh \
#     /etc/insulavm/config.yaml /etc/gitmaster-actions/profiles.yaml /caminho/snapshot.zip [develop|main|teste-falha]
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
if (($# < 3 || $# > 4)); then
  echo "Uso: $0 <insula-config.yaml> <profiles.yaml> <repo.zip> [branch]" >&2
  exit 2
fi
INSULA="$1"
PROFILES="$2"
ZIP="$(realpath -- "$3")"
BRANCH="${4:-develop}"
test -r "$INSULA" && test -r "$PROFILES" && test -r "$ZIP"
DB="${GITMASTER_ACTIONS_SMOKE_DB:-/tmp/gitmaster-actions-smoke-$(id -u).db}"
BIN="${GITMASTER_ACTIONS_BIN:-$ROOT/.smoke-gitmaster-actions}"
if [[ ! -x "$BIN" ]]; then
  (cd "$ROOT" && go build -o "$BIN" ./src/cmd/gitmaster-actions)
fi
TMP_JSON="$(mktemp)"
trap 'rm -f "$TMP_JSON"' EXIT
set +e
"$BIN" run \
  --db "$DB" \
  --insula-config "$INSULA" \
  --profiles-config "$PROFILES" \
  --format gitmaster-workflow-v1 \
  --branch "$BRANCH" \
  --file "$ROOT/examples/workflow/dependency-parallel-smoke.yaml" \
  --archive "$ZIP" \
  --details > "$TMP_JSON"
RC=$?
set -e
cat "$TMP_JSON"
go run "$ROOT/tests/verifiers/parallel/verify.go" "$BRANCH" "$TMP_JSON" "$RC"
echo "Validação concluída para a branch $BRANCH."
