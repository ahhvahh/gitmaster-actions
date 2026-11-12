#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG=""
ISO=""
PROFILE="gitmaster-actions-test-500m"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/gitmaster-actions/integration"
DB=""
usage() {
  cat >&2 <<'USAGE'
Uso: scripts/test-integration.sh --insula-config /caminho/config.yaml [opções]
  --profile NOME    Perfil de teste (padrão gitmaster-actions-test-500m)
  --iso CAMINHO     ISO Debian 13 mini.iso local (sem esta opção, download oficial com SHA256)
  --cache-dir DIR   Diretório de download verificado
  --db ARQUIVO      SQLite de teste (padrão: diretório temporário privado)
Exige Linux x86_64, KVM/Libvirt e ferramentas de instalação do InsulaVM.
Instala via ISO com 1024 MiB; registra perfil de EXECUÇÃO 500 MiB/1 vCPU.
USAGE
}
while (($#)); do
  case "$1" in
    --insula-config) [[ $# -ge 2 ]] || { usage; exit 2; }; CONFIG="$2"; shift 2;;
    --profile) [[ $# -ge 2 ]] || { usage; exit 2; }; PROFILE="$2"; shift 2;;
    --iso) [[ $# -ge 2 ]] || { usage; exit 2; }; ISO="$2"; shift 2;;
    --cache-dir) [[ $# -ge 2 ]] || { usage; exit 2; }; CACHE_DIR="$2"; shift 2;;
    --db) [[ $# -ge 2 ]] || { usage; exit 2; }; DB="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) usage; exit 2;;
  esac
done
if [[ -z "$CONFIG" || ! -f "$CONFIG" ]]; then
  printf 'Informe --insula-config com um arquivo existente.\n' >&2
  exit 2
fi
if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 || ! -c /dev/kvm ]]; then
  printf 'Teste integral exige Linux x86_64 e /dev/kvm; nada foi provisionado.\n' >&2
  exit 3
fi
for tool in go virsh virt-install qemu-img guestmount guestunmount openssl; do
  if ! command -v "$tool" >/dev/null; then
    printf 'Dependência ausente no host: %s\n' "$tool" >&2
    exit 3
  fi
done
if [[ -z "$DB" ]]; then
  TMP_DB_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gm-actions-integral.XXXXXXXX")"
  chmod 700 "$TMP_DB_DIR"
  DB="$TMP_DB_DIR/actions.db"
  trap 'rm -rf -- "$TMP_DB_DIR"' EXIT
fi
cd "$ROOT"
args=(--config "$CONFIG" --profile "$PROFILE" --cache-dir "$CACHE_DIR" --db "$DB" --yaml "${ROOT}/scripts/fixtures/integration-three-steps.yaml")
if [[ -n "$ISO" ]]; then args+=(--iso "$ISO"); fi
printf '[integral] Preparar perfil e executar três comandos em uma VM real...\n'
go run ./src/cmd/integration-test "${args[@]}"