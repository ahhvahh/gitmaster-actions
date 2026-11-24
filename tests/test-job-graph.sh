#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd)"
OUT="$ROOT/tests/work/graph-manual-$$"
if (($#>0)); then OUT="$1"; fi
BIN="$ROOT/tests/bin/gitmaster-actions"
mkdir -p "$OUT"
[[ -x "$BIN" ]] || bash "$ROOT/tests/build.sh" > "$OUT/build.log" 2>&1
(cd "$ROOT" && go build -o "$OUT/verify" ./tests/verifiers/graph)
DAG="$ROOT/tests/plan-cases/needs-dag.yaml"
BRANCH="$ROOT/examples/workflow/branch-filter-smoke.yaml"
printf '{"lint":"succeeded","build":"succeeded","package":"succeeded","verify":"succeeded"}\n' > "$OUT/success.json"
printf '{"lint":"succeeded","build":"succeeded","package":"failed","verify":"succeeded"}\n' > "$OUT/failure.json"
"$BIN" graph --file "$DAG" > "$OUT/initial.json"
"$OUT/verify" initial "$OUT/initial.json"
"$BIN" graph --file "$DAG" --statuses "$OUT/success.json" > "$OUT/success-result.json"
"$OUT/verify" success "$OUT/success-result.json"
"$BIN" graph --file "$DAG" --statuses "$OUT/failure.json" > "$OUT/failure-result.json"
"$OUT/verify" failure "$OUT/failure-result.json"
for branch in develop main; do
  "$BIN" graph --branch "$branch" --file "$BRANCH" > "$OUT/$branch.json"
  "$OUT/verify" "$branch" "$OUT/$branch.json"
done
printf '{"unknown_job":"succeeded"}\n' > "$OUT/unknown.json"
if "$BIN" graph --file "$DAG" --statuses "$OUT/unknown.json" > "$OUT/invalid.json" 2> "$OUT/invalid.stderr"; then
  echo '[graph] invalid status accepted' >&2; exit 1
fi
if [[ -e "$OUT/actions.db" || -e "$OUT/actions.db.lock" ]]; then
  echo '[graph] graph created an unexpected SQLite database' >&2; exit 1
fi
echo '[graph] Five CLI scenarios passed without VM or SQLite'
