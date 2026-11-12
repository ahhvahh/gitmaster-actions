<!-- A-BIOMA-DOC-STANDARD -->

# GUIA-009 — Validação do GitMaster Actions com Go 1.27

## Objetivo

Verificar o módulo Go localmente com a versão suportada e acompanhar o resultado complementar da integração contínua. Testes com mocks não substituem a integração autorizada em KVM/Libvirt.

## Pré-requisitos

- Go **1.27.4** instalado (revisão estável de 10/11/2026).
- Acesso aos módulos fixados em [go.mod](../../go.mod), inclusive InsulaVM.
- Diretório do repositório no commit desejado, sem modificar o GitMaster.

## Execução local

```bash
go version
go mod download
bash scripts/test-unit.sh
```

**Resultado esperado:** testes dos pacotes Go concluídos sem erro. Os testes com runtime simulado não precisam de Libvirt/KVM real. Não considere `queued` como aprovação de Action e não marque validação bem-sucedida se o comando não terminar com código zero.

## Integração contínua

O arquivo [`.github/workflows/go127.yml`](../../.github/workflows/go127.yml) executa `scripts/test-unit.sh` em `ubuntu-24.04`, com `actions/setup-go@v7` e Go 1.27.4. O workflow é disparado em todo push de qualquer branch e em PRs; commits apenas locais não chegam ao CI. O hook `scripts/install-hooks.sh` pode executar a suíte antes dos commits locais. Permissão do token: somente leitura de conteúdo.

A automação remota é uma confirmação complementar, **não um substituto da execução local** nem uma prova de funcionamento KVM/Libvirt.

## Diagnóstico

Se o host usar Go anterior a 1.27 e não puder acessar `proxy.golang.org`, a instalação automática da toolchain falhará. Nesse cenário, registre o bloqueio e execute a suíte em um host com Go 1.27.4 e rede habilitada; não altere a diretiva `go` de `go.mod` para contornar o requisito.

## Referências

- [Documentação do módulo](../README.md)
- [Integração com o GitMaster](GUIA-008-integracao-gitmaster.md)
- [Histórico oficial do Go](https://go.dev/doc/devel/release)
- [Configuração oficial do setup-go](https://github.com/actions/setup-go)

Para o teste integral de VM, siga [GUIA-010](GUIA-010-build-testes.md); não o inclua no CI unitário sem host KVM autorizado.
