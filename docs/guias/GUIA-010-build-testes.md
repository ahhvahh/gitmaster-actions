<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-010 — Compilação, testes por commit e integração com VM real

## Objetivo

Manter código de biblioteca, testes e CLIs em `/src`, documentação em `/docs`, e scripts operacionais em `/scripts`. A API importada pelo GitMaster **não muda**: `github.com/ahhvahh/gitmaster-actions` é uma fachada compatível sobre `github.com/ahhvahh/gitmaster-actions/src`. O pacote `legacy` na raiz permanece como fachada de compatibilidade. Não instalar um novo módulo Go em `/src`.

## Estrutura

```text
gitmaster-actions/
├── api.go                       # fachada pública, mantém import path
├── legacy/api.go                # fachada do antigo package legacy
├── src/                         # engine, runtime, YAML, SQLite e testes _test.go
│   ├── legacy/                  # implementação do legado
│   └── cmd/
│       ├── gitmaster-actions/   # CLI de uso normal
│       └── integration-test/    # driver do teste KVM autorizado
├── scripts/
│   ├── build.sh
│   ├── test-unit.sh
│   ├── test-integration.sh
│   ├── install-hooks.sh
│   ├── hooks/pre-commit
│   └── fixtures/integration-three-steps.yaml
├── docs/
└── go.mod
```

## Compilar binário

Exige Go 1.27.4 conforme `go.mod` e dependências acessíveis.

```bash
# Interativo: pergunta o destino completo quando houver terminal
bash scripts/build.sh

# Automação: arquivo exato de saída
bash scripts/build.sh --output /tmp/bin/gitmaster-actions
```

`build.sh` cria o diretório de destino e compila `./src/cmd/gitmaster-actions` com `-trimpath`, `CGO_ENABLED=0`. O arquivo informado é o binário final; não altera o projeto GitMaster.

## Testes unitários em cada commit

```bash
bash scripts/test-unit.sh
```

Executa em ordem: `go mod tidy -diff`, `go test -race -count=1 ./...`, `go vet ./...`; falha interrompe o script. O workflow [Go 1.27](../../.github/workflows/go127.yml) executa este script em **todo push** e PR; um commit que não foi enviado ao GitHub não pode disparar CI remoto. Para validar **antes de cada commit local**, habilite o hook uma vez em cada clone:

```bash
bash scripts/install-hooks.sh
```

O hook `pre-commit` usa `scripts/test-unit.sh`, e só está ativo em clones que executaram a instalação. A configuração `core.hooksPath` é local e não modifica credenciais nem branches.

## Teste integral KVM/Libvirt — execução manual autorizada

```bash
bash scripts/test-integration.sh \
  --insula-config /etc/insulavm/config.yaml \
  --db /var/tmp/gitmaster-actions-test/actions.db
```

Opções: `--profile gitmaster-actions-test-500m` (padrão), `--iso /abs/debian-mini.iso` (opcional), `--cache-dir` (opcional), `--db` (opcional; se omitido o arquivo temporário é removido após o teste).

**Antes da Action**, o driver:

1. Confere KVM x86_64, Libvirt e ferramentas requeridas pelo InsulaVM (`virt-install`, `qemu-img`, `guestmount`, `guestunmount`, `openssl`). Sem KVM, falha sem provisionar VM.
2. Reutiliza o perfil Debian validado com **500 MiB / 1 vCPU** se já existir.
3. Caso contrário, baixa `mini.iso` Debian 13 do espelho oficial HTTPS, com verificação do SHA256 publicado pelo Debian, **ou** utiliza ISO local explicitamente indicada. A verificação de integridade via HTTPS não equivale à verificação de assinatura GPG.
4. Chama **InsulaVM `CreateProfileFromISO`**, que exige **1024 MiB** durante a instalação e instala Debian 13 em QCOW2 independente; não é possível instalar uma ISO de Arch/Alpine por essa API hoje. Instalação requer rede da VM e repositórios Debian acessíveis.
5. Registra, por `InsulaVM.RegisterProfile`, um **novo perfil** com 500 MiB e 1 vCPU, copiando o QCOW2 instalado; perfis existentes não são sobrescritos. O perfil intermediário (1 GiB) permanece disponível para reuso.
6. Envia o YAML [integration-three-steps.yaml](../../scripts/fixtures/integration-three-steps.yaml) por `SubmitYAML(action-v1)` a **uma VM isolada**, com três comandos independentes: imprimir marcador 1; verificar `uname -s` igual a `Linux` e imprimir marcador 2; verificar `/tmp` e imprimir marcador 3.
7. Aguarda a Action finalizar; consulta SQLite via API pública e exige três Commands com `status=succeeded`, `exit_code=0` e saídas persistidas contendo `GM_STEP_1_OK`, `GM_STEP_2_OK`, `GM_STEP_3_OK`.
8. Exige `cleanup_status=succeeded`, VMID persistido e `LibvirtState=absent` consultado **via InsulaVM**, confirmando `StopVM`. Só então imprime `GITMASTER_ACTIONS_INTEGRATION_OK` e termina com código zero.

**Segurança:** execução real *não* é disparada pelo CI de unidades. Rodar apenas em host KVM de testes, com permissão explícita do operador, perfis dedicados, rede controlada e armazenamento separado. Nunca executar a instalação sob os mesmos nomes de perfis de produção. A VM de Action é descartável; os perfis QCOW2 registrados **não** são apagados automaticamente.

## Resultado e limitações

A criação dos scripts e compilação dos pacotes Go não significa que o teste integral tenha sido executado no host real. O resultado será comprovado apenas quando o script imprimir o marcador final com código zero. O transporte atual `ExecVM` fornece linhas de console decoradas, não streams binários nativos.

## Referências

- [API pública](../interfaces/go/INTF-GO-001-biblioteca-publica.md)
- [YAML de Action](../contratos/CONTR-001-configuracao-action.md)
- [Persistência](../persistencia/PERS-001-registro-execucoes.md)
- [Validação Go 1.27](GUIA-009-validacao-go127.md)
- [InsulaVM `CreateProfileFromISO`](https://github.com/ahhvahh/InsulaVM/blob/983c4a8b46c7ee1a2f7ebd3d45de41aa3589b397/insulavm/create_profile_iso.go)
## Estrutura atual de testes em `/tests`

Também estão disponíveis `tests/build.sh` e `tests/run.sh` para execução local consolidada. O build produz `tests/bin/gitmaster-actions` e o runner mantém ZIP, bancos SQLite, JSON e logs em `tests/work/<execucao>/`.

```bash
bash tests/build.sh
bash tests/run.sh --mode unit
bash tests/run.sh --mode vm --case all \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml
```

O CI executa `tests/run.sh --mode unit` sem KVM. `--mode vm` só funciona em host KVM/Libvirt autorizado; `--mode all` executa os dois conjuntos. O runner preserva evidências e não versiona arquivos binários ou bancos. Consulte [tests/README.md](../../tests/README.md).
