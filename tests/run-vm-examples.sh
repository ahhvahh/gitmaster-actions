#!/usr/bin/env bash
# Suite integral de workflows reais: CLI -> InsulaVM -> SQLite -> verificador.
# Execucao de VM apenas com --insula-config + --profiles-config explicitados.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd)"
MANIFEST="$ROOT/tests/vm-examples/cases.tsv"
CASE="all"
CONFIG=""
PROFILES=""
BIN=""
WORK=""
DRY_RUN=false
LIST=false

usage() {
  cat <<'HELP'
Uso: bash tests/run-vm-examples.sh [--case all|NOME] \
       --insula-config /etc/insulavm/config.yaml \
       --profiles-config /etc/gitmaster-actions/profiles.yaml \
       [--output-dir DIR] [--bin ARQUIVO]

  --list          Lista os exemplos sem iniciar VM
  --dry-run       Valida todos os YAMLs e resultados esperados sem VM/SQLite
  --case NOME     Executa somente um cenario do arquivo tests/vm-examples/cases.tsv
  --case all      Executa todos os 7 cenarios (padrao)
  --output-dir    Mantem todos os resultados nesse diretorio (deve estar vazio)
  --bin           Binario pre-compilado gitmaster-actions; sem isso, executa tests/build.sh

O runner monta um ZIP com README.txt e src/payload.txt ficticios, chama
gitmaster-actions run --details e verifica Action, Jobs, Commands, exit_code,
skip_reason, VM cleanup, integridade SQLite, outputs e linhas de logs.
Uma falha de Job com exit 17 e Action failed e sucesso do TESTE, se esperado.

Exige Linux com /dev/kvm, InsulaVM/Libvirt, Go, zip e perfil ci-smoke
funcional com SSH/Cloud-Init e unzip instalado no convidado.
HELP
}

while (($#)); do
  case "$1" in
    --case) (($#>=2)) || { usage >&2; exit 2; }; CASE="$2"; shift 2 ;;
    --insula-config) (($#>=2)) || { usage >&2; exit 2; }; CONFIG="$2"; shift 2 ;;
    --profiles-config) (($#>=2)) || { usage >&2; exit 2; }; PROFILES="$2"; shift 2 ;;
    --output-dir) (($#>=2)) || { usage >&2; exit 2; }; WORK="$2"; shift 2 ;;
    --bin) (($#>=2)) || { usage >&2; exit 2; }; BIN="$2"; shift 2 ;;
    --list) LIST=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) printf '[vm-examples] Opcao desconhecida: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

# Check the selected name without provisioning anything.
selected=0
while IFS=$'\t' read -r name branch yaml spec; do
  [[ -n "$name" && "$name" != \#* ]] || continue
  if [[ "$LIST" == true ]]; then printf '%-19s branch=%-14s %s\n' "$name" "$branch" "$yaml"; fi
  if [[ "$CASE" == all || "$CASE" == "$name" ]]; then selected=$((selected+1)); fi
done < "$MANIFEST"
if [[ "$LIST" == true ]]; then exit 0; fi
if ((selected==0)); then
  printf '[vm-examples] Caso desconhecido: %s (consulte --list)\n' "$CASE" >&2
  exit 2
fi

command -v go >/dev/null || { echo '[vm-examples] Go ausente' >&2; exit 3; }
if [[ "$DRY_RUN" == true ]]; then
  echo '[vm-examples] Validando YAMLs e expectativas sem VM ou SQLite...'
  (cd "$ROOT" && go test -count=1 -run '^TestVMExamplesStatic$' -v ./tests/verifiers/vm-examples)
  exit
fi

command -v zip >/dev/null || { echo '[vm-examples] zip ausente' >&2; exit 3; }
if [[ -z "$CONFIG" || -z "$PROFILES" || ! -r "$CONFIG" || ! -r "$PROFILES" ]]; then
  echo '[vm-examples] --insula-config e --profiles-config devem ser arquivos legiveis' >&2
  exit 2
fi
if [[ "$(uname -s)" != Linux || ! -c /dev/kvm ]]; then
  echo '[vm-examples] Necessario host Linux com /dev/kvm; nenhuma VM iniciada' >&2
  exit 3
fi
CONFIG="$(realpath -- "$CONFIG")"
PROFILES="$(realpath -- "$PROFILES")"
if [[ -z "$WORK" ]]; then
  WORK="$ROOT/tests/work/vm-examples-$(date -u +%Y%m%dT%H%M%SZ)-$$"
fi
if [[ -d "$WORK" ]] && [[ -n "$(find "$WORK" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  echo "[vm-examples] Diretorio de resultados nao esta vazio: $WORK" >&2
  exit 2
fi
mkdir -p -- "$WORK"
WORK="$(cd "$WORK" && pwd)"
if [[ -z "$BIN" ]]; then
  bash "$ROOT/tests/build.sh" > "$WORK/build.log" 2>&1 || { cat "$WORK/build.log" >&2; exit 1; }
  BIN="$ROOT/tests/bin/gitmaster-actions"
fi
[[ -x "$BIN" ]] || { printf '[vm-examples] Binario invalido: %s\n' "$BIN" >&2; exit 2; }
BIN="$(realpath -- "$BIN")"
(cd "$ROOT" && go build -o "$WORK/verify-vm" ./tests/verifiers/vm-examples) \
  > "$WORK/build-verifier.log" 2>&1 || { cat "$WORK/build-verifier.log" >&2; exit 1; }

# Material de teste intencionalmente inocuo: nunca empacotar um clone real
# ou a pasta de trabalho inteira, que poderiam conter segredos.
mkdir -p -- "$WORK/fixture/src"
cp -- "$ROOT/tests/fixtures/vm-examples/README.txt" "$WORK/fixture/README.txt"
cp -- "$ROOT/tests/fixtures/vm-examples/src/payload.txt" "$WORK/fixture/src/payload.txt"
(cd "$WORK/fixture" && zip -q "$WORK/snapshot.zip" README.txt src/payload.txt)
echo "[vm-examples] ZIP de teste: $WORK/snapshot.zip"
echo "[vm-examples] Cenarios selecionados: $selected"

tested=0
failures=0
while IFS=$'\t' read -r name branch yaml spec; do
  [[ -n "$name" && "$name" != \#* ]] || continue
  [[ "$CASE" == all || "$CASE" == "$name" ]] || continue
  tested=$((tested+1))
  folder="$WORK/$name"
  mkdir -p -- "$folder"
  echo "[vm-examples] Execucao $tested/$selected: $name (branch $branch)"
  rc=0
  "$BIN" run \
    --db "$folder/actions.db" \
    --insula-config "$CONFIG" \
    --profiles-config "$PROFILES" \
    --format gitmaster-workflow-v1 \
    --branch "$branch" \
    --file "$ROOT/$yaml" \
    --archive "$WORK/snapshot.zip" \
    --details > "$folder/action.json" 2> "$folder/stderr.log" || rc=$?
  printf '%s\n' "$rc" > "$folder/cli-exit-code.txt"

  if "$WORK/verify-vm" "$folder/action.json" "$folder/actions.db" "$ROOT/$spec" "$rc" \
    > "$folder/verify.log" 2>&1; then
    cat "$folder/verify.log"
    echo "[vm-examples] APROVADO: $name"
  else
    failures=$((failures+1))
    echo "[vm-examples] REPROVADO: $name" >&2
    cat "$folder/verify.log" >&2
    cat "$folder/stderr.log" >&2
  fi
done < "$MANIFEST"

printf '[vm-examples] Total=%d aprovados=%d falhas=%d\n' "$tested" "$((tested-failures))" "$failures"
printf '[vm-examples] Evidencias: %s\n' "$WORK"
if ((failures>0)); then exit 1; fi
