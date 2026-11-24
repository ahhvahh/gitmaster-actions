#!/usr/bin/env bash
# Verifica a conversão para pares job/command comparando JSON exato.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd)"
BIN="$ROOT/tests/bin/gitmaster-actions"
OUT="$ROOT/tests/work/converter-manual-$$"
if (($# > 0)); then OUT="$1"; fi
mkdir -p -- "$OUT"
[[ -x "$BIN" ]] || bash "$ROOT/tests/build.sh" > "$OUT/build.log" 2>&1

invoke_plan() {
  local fmt="$1" branch="$2" event="$3" yaml="$4"
  if [[ "$branch" == "-" ]]; then
    "$BIN" plan --format "$fmt" --event "$event" --file "$ROOT/$yaml"
  else
    "$BIN" plan --format "$fmt" --event "$event" --branch "$branch" --file "$ROOT/$yaml"
  fi
}

passed=0
while IFS=$'\t' read -r id format branch event yaml expected; do
  [[ -n "$id" && "$id" != \#* ]] || continue
  if ! (cd "$OUT" && invoke_plan "$format" "$branch" "$event" "$yaml" > "$OUT/$id.json" 2> "$OUT/$id.stderr"); then
    cat "$OUT/$id.stderr" >&2
    printf '[plan] ERRO: %s deveria ser válido\n' "$id" >&2
    exit 1
  fi
  if ! diff -u -- "$ROOT/$expected" "$OUT/$id.json" > "$OUT/$id.diff"; then
    cat "$OUT/$id.diff" >&2
    printf '[plan] ERRO: saída divergente: %s\n' "$id" >&2
    exit 1
  fi
  passed=$((passed+1))
done < "$ROOT/tests/plan-cases/valid.tsv"

while IFS=$'\t' read -r id format branch event yaml contains; do
  [[ -n "$id" && "$id" != \#* ]] || continue
  if (cd "$OUT" && invoke_plan "$format" "$branch" "$event" "$yaml" > "$OUT/$id.json" 2> "$OUT/$id.stderr"); then
    printf '[plan] ERRO: YAML inválido foi aceito: %s\n' "$id" >&2
    exit 1
  fi
  if ! grep -Fq -- "$contains" "$OUT/$id.stderr"; then
    printf '[plan] ERRO: diagnóstico incorreto em %s: esperado %s\n' "$id" "$contains" >&2
    cat "$OUT/$id.stderr" >&2
    exit 1
  fi
  passed=$((passed+1))
done < "$ROOT/tests/plan-cases/invalid.tsv"

cat "$ROOT/tests/plan-cases/simple-map.yaml" |
  (cd "$OUT" && "$BIN" plan --stdin --format gitmaster-workflow-v1) > "$OUT/stdin.json"
diff -u "$ROOT/tests/plan-cases/expected/simple-map.json" "$OUT/stdin.json"
if [[ -e "$OUT/actions.db" || -e "$OUT/actions.db.lock" ]]; then
  echo '[plan] ERRO: a conversão abriu SQLite' >&2
  exit 1
fi
printf '[plan] OK: %s casos em arquivos + stdin, sem SQLite ou VM\n' "$passed"
