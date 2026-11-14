#!/usr/bin/env bash
# Integration test requiring a prepared ci-smoke QCOW2 and KVM/Libvirt.
# Usage: bash scripts/tests/workflow-branch-filter-smoke.sh /etc/insulavm/config.yaml /etc/gitmaster-actions/profiles.yaml
set -Eeuo pipefail
if (($# != 2)); then
  echo "Usage: $0 <insula-config.yaml> <profiles.yaml>" >&2
  exit 2
fi
ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
INSULA="$(realpath -- "$1")"
PROFILES="$(realpath -- "$2")"
test -r "$INSULA" && test -r "$PROFILES"
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT
printf 'branch-filter smoke test\n' > "$TMP/README.md"
(cd "$TMP" && zip -q snapshot.zip README.md)
(cd "$ROOT" && go build -o "$TMP/gitmaster-actions" ./src/cmd/gitmaster-actions)
for BRANCH in develop main; do
  echo "=== Verificando branch $BRANCH ==="
  set +e
  "$TMP/gitmaster-actions" run \
    --db "$TMP/actions.db" \
    --insula-config "$INSULA" \
    --profiles-config "$PROFILES" \
    --format gitmaster-workflow-v1 \
    --branch "$BRANCH" \
    --file "$ROOT/examples/workflow/branch-filter-smoke.yaml" \
    --archive "$TMP/snapshot.zip" \
    --details > "$TMP/$BRANCH.json"
  RC=$?
  set -e
  cat "$TMP/$BRANCH.json"
  go run "$ROOT/tests/verifiers/branch/verify.go" "$BRANCH" "$TMP/$BRANCH.json" "$RC"
done
echo "OK: jobs filtrados por branch para develop e main"
