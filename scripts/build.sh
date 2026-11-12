#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
usage() { printf 'Uso: %s [--output /caminho/completo/gitmaster-actions]\n' "$0" >&2; }
if [[ $# -eq 2 && "$1" == "--output" ]]; then
  output="$2"
elif [[ $# -eq 0 && -t 0 ]]; then
  read -r -p 'Destino do binário (arquivo completo): ' output
else
  usage
  exit 2
fi
if [[ -z "$output" || "$output" == */ ]]; then
  printf 'Informe um caminho de arquivo, não somente um diretório.\n' >&2
  exit 2
fi
mkdir -p -- "$(dirname -- "$output")"
output="$(cd -- "$(dirname -- "$output")" && pwd)/$(basename -- "$output")"
cd "$ROOT"
CGO_ENABLED=0 go build -trimpath -o "$output" ./src/cmd/gitmaster-actions
printf 'Binário gerado: %s\n' "$output"