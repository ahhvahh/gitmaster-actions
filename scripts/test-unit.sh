#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
printf '[unit] Go toolchain: '; go version
printf '[unit] go mod tidy -diff\n'
go mod tidy -diff
printf '[unit] go test -race -count=1 ./...\n'
go test -race -count=1 ./...
printf '[unit] go vet ./...\n'
go vet ./...
printf '[unit] OK\n'