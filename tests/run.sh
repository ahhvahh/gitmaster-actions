#!/usr/bin/env bash
# Compila, executa testes Go e opcionalmente testa a CLI usando VM real.
# Preserva ZIP, SQLite, JSON e logs sob tests/work/<execucao>.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd)"
MODE="unit"
CASE="all"
CONFIG=""
PROFILES=""
BIN="$ROOT/tests/bin/gitmaster-actions"

usage() {
  cat <<'HELP'
Uso: bash tests/run.sh [--mode unit|plan|graph|vm|all] [--case branch|parallel|all]
                       [--insula-config /abs/config.yaml] [--profiles-config /abs/profiles.yaml]

unit      Compila, testa Go e também conversor YAML/CLI (padrão; sem VM)
plan      Compila e executa somente casos de conversão (sem VM/SQLite)
graph     Compila e testa o grafo de dependências, sem VM/SQLite
vm        Compila, executa smoke tests reais por CLI na VM descartável
all       Testes Go e depois smoke tests reais
branch    Dois Jobs: apenas o correspondente à branch deve executar
parallel  Jobs paralelos/dependentes, filtros, falha intencional e skips
Resultados: tests/bin/gitmaster-actions e tests/work/<execucao>/.
HELP
}

while (($# > 0)); do
  case "$1" in
    --mode) (($# >= 2)) || { usage >&2; exit 2; }; MODE="$2"; shift 2 ;;
    --case) (($# >= 2)) || { usage >&2; exit 2; }; CASE="$2"; shift 2 ;;
    --insula-config) (($# >= 2)) || { usage >&2; exit 2; }; CONFIG="$2"; shift 2 ;;
    --profiles-config) (($# >= 2)) || { usage >&2; exit 2; }; PROFILES="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) printf '[tests] Opção desconhecida: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$MODE" in unit|plan|graph|vm|all) ;; *) echo "[tests] --mode inválido: $MODE" >&2; exit 2 ;; esac
case "$CASE" in branch|parallel|all) ;; *) echo "[tests] --case inválido: $CASE" >&2; exit 2 ;; esac
command -v go >/dev/null || { echo "[tests] Go não encontrado no PATH" >&2; exit 3; }

if [[ "$MODE" == vm || "$MODE" == all ]]; then
  command -v zip >/dev/null || { echo "[tests] zip não encontrado" >&2; exit 3; }
  if [[ -z "$CONFIG" || -z "$PROFILES" ]]; then
    echo "[tests] --insula-config e --profiles-config são obrigatórios com VM" >&2
    exit 2
  fi
  [[ -r "$CONFIG" && -f "$CONFIG" ]] || { echo "[tests] InsulaVM config ausente: $CONFIG" >&2; exit 2; }
  [[ -r "$PROFILES" && -f "$PROFILES" ]] || { echo "[tests] YAML de perfis ausente: $PROFILES" >&2; exit 2; }
  CONFIG="$(realpath -- "$CONFIG")"
  PROFILES="$(realpath -- "$PROFILES")"
  if [[ "$(uname -s)" != Linux || ! -c /dev/kvm ]]; then
    echo "[tests] Teste integral exige Linux e /dev/kvm" >&2
    exit 3
  fi
fi

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
WORK="$ROOT/tests/work/$RUN_ID"
mkdir -p -- "$WORK"
printf '[tests] Modo: %s; casos: %s\n[tests] Evidências: %s\n' "$MODE" "$CASE" "$WORK"
trap 'rc=$?; if ((rc != 0)); then printf "[tests] FALHOU (%s), logs em %s\n" "$rc" "$WORK" >&2; fi' EXIT

bash "$ROOT/tests/build.sh" > "$WORK/build.log" 2>&1 || { cat "$WORK/build.log" >&2; exit 1; }
cat "$WORK/build.log"

if [[ "$MODE" == unit || "$MODE" == all ]]; then
  printf '[tests] Testes unitários, race detector e vet...\n'
  (cd "$ROOT" && bash scripts/test-unit.sh) > "$WORK/unit.log" 2>&1 || { cat "$WORK/unit.log" >&2; exit 1; }
  cat "$WORK/unit.log"
fi

run_vm_case() {
  local suite="$1"
  local branch="$2"
  local workflow="$3"
  local verifier="$4"
  local folder="$WORK/$suite/$branch"
  local rc=0
  mkdir -p -- "$folder"
  printf '[tests] CLI: %s / %s\n' "$suite" "$branch"
  "$BIN" run \
    --db "$folder/actions.db" \
    --insula-config "$CONFIG" \
    --profiles-config "$PROFILES" \
    --format gitmaster-workflow-v1 \
    --branch "$branch" \
    --file "$workflow" \
    --archive "$WORK/snapshot.zip" \
    --details > "$folder/action.json" 2> "$folder/stderr.log" || rc=$?

  # Até a falha esperada (exit 17) deve ser confirmada pelo verificador.
  if ! go run "$verifier" "$branch" "$folder/action.json" "$rc" > "$folder/verify.log" 2>&1; then
    cat "$folder/verify.log" >&2
    cat "$folder/stderr.log" >&2
    printf '[tests] JSON: %s\n' "$folder/action.json" >&2
    return 1
  fi
  cat "$folder/verify.log"
}

if [[ "$MODE" == unit || "$MODE" == plan || "$MODE" == all ]]; then
  printf '[tests] Validando conversor YAML pela CLI, sem VM/SQLite...\n'
  if ! bash "$ROOT/tests/test-converter.sh" "$WORK/converter" > "$WORK/converter.log" 2>&1; then
    cat "$WORK/converter.log" >&2
    exit 1
  fi
  cat "$WORK/converter.log"
fi

if [[ "$MODE" == unit || "$MODE" == graph || "$MODE" == all ]]; then
  printf '[tests] Validando grafo de dependências e prontidão pela CLI...\n'
  if ! bash "$ROOT/tests/test-job-graph.sh" "$WORK/graph" > "$WORK/graph.log" 2>&1; then
    cat "$WORK/graph.log" >&2
    exit 1
  fi
  cat "$WORK/graph.log"
fi

if [[ "$MODE" == vm || "$MODE" == all ]]; then
  mkdir -p "$WORK/fixture"
  cp -- "$ROOT/tests/fixtures/README.txt" "$WORK/fixture/README.txt"
  (cd "$WORK/fixture" && zip -q "$WORK/snapshot.zip" README.txt)
  printf '[tests] ZIP: %s\n' "$WORK/snapshot.zip"

  if [[ "$CASE" == branch || "$CASE" == all ]]; then
    for branch in develop main; do
      run_vm_case branch "$branch" \
        "$ROOT/examples/workflow/branch-filter-smoke.yaml" \
        "$ROOT/tests/verifiers/branch/verify.go"
    done
  fi

  if [[ "$CASE" == parallel || "$CASE" == all ]]; then
    for branch in develop main teste-falha; do
      run_vm_case parallel "$branch" \
        "$ROOT/examples/workflow/dependency-parallel-smoke.yaml" \
        "$ROOT/tests/verifiers/parallel/verify.go"
    done
  fi
fi

printf '[tests] OK. Evidências: %s\n' "$WORK"
