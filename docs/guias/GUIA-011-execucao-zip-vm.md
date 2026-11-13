<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-011 — Processar snapshot ZIP na VM

## Pré-requisitos

- Host Linux com InsulaVM, QEMU/Libvirt e configuração administrativa `/etc/insulavm/config.yaml`.
- Uma ou várias imagens QCOW2 de VMs previamente preparadas e congeladas, com **`unzip` e Cloud-Init/SSH gerenciado instalados** (modo `direct`/QGA sem SSH não é suportado no executor atual).
- Catálogo local `/etc/gitmaster-actions/profiles.yaml` conforme [CONTR-004](../contratos/CONTR-004-perfis-congelados.md).
- Workflow que seleciona um dos perfis por `vm_profile`; ZIP do repositório correspondente à branch recebida.

## Workflow

```yaml
version: 1
name: go-ci
vm_profile: ci-go
jobs:
  test:
    steps:
      - name: Unit tests
        run: go test ./...
  build:
    needs: [test]
    steps:
      - name: Compile
        run: go build ./...
```

Para execuções por evento commit/push, mantenha `triggers` e envie `ActionOrigin` completo pela API Go. Para uma execução manual, o CLI usa a branch informada nos filtros sem simular evento Git.

## Executar

```bash
gitmaster-actions run \
  --db /var/lib/gitmaster-actions/actions.db \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml \
  --format gitmaster-workflow-v1 \
  --branch develop \
  --file ./workflow.yaml \
  --archive /srv/snapshots/repository.zip
```

O processamento segue: validar YAML e perfil → validar e copiar ZIP para área privada → cadastrar Action, Jobs e Commands em SQLite → iniciar VM descartável → importar ZIP por InsulaVM → extrair com `unzip` em `/tmp/workspace` (configurável por perfil) → executar cada step no diretório configurado → registrar código, stdout/stderr e eventos → desligar VM e registrar resultado.

## Segurança e diagnósticos

O validador ZIP recusa travessia de diretório, caminhos duplicados, symlinks, entradas especiais, entradas criptografadas, nomes reservados para credenciais e arquivos demasiadamente grandes. O ZIP comprimido é limitado a 100 MiB, o conteúdo descomprimido a 256 MiB, até 15.000 arquivos e 20.000 entradas. O chamador deve filtrar quaisquer outros segredos e autorizar os comandos. Os comandos do YAML executam **dentro da VM**, nunca no host.

Para consultar, utilize `actions list|get`, `jobs list|get`, `commands list|get`, `logs list` e `outputs list`; os métodos equivalentes da biblioteca são `GetActionDetails`, `ListJobs`, `ListCommands` e `ListCommandLogs`. O estado terminal pode ser `succeeded`, `failed` ou `cancelled`; uma ação interrompida não é retomada automaticamente.

## Testes de dependências e paralelismo

Use [dependency-parallel-smoke.yaml](../../examples/workflow/dependency-parallel-smoke.yaml) com `--format gitmaster-workflow-v1 --branch develop --details` para observar Jobs executados e pulados. Use `--branch teste-falha` para verificar uma falha intencional (exit 17) e o bloqueio de dependentes. O `max_parallel_jobs: 4` permite vários Jobs ativos; o InsulaVM serializa seus comandos no mesmo convidado. Em caso de `ErrVMExecBusy` externo, o executor tenta novamente até cinco vezes com jitter de 10 a 1.000 ms.

### Preparar ZIP mínimo e executar na CLI

O ZIP deve conter arquivos na raiz, não uma pasta adicional do repositório. O workflow de teste não precisa compilar um projeto: executa comandos POSIX básicos no convidado e cria seus próprios resultados no `workspace`.

```bash
mkdir -p /tmp/gitmaster-smoke/repo
printf 'gitmaster-actions smoke test\n' > /tmp/gitmaster-smoke/repo/README.md
(cd /tmp/gitmaster-smoke/repo && zip -q ../repo.zip README.md)

# Executar um fluxo que deve terminar succeeded com alguns Jobs skipped
bash scripts/tests/workflow-parallel-smoke.sh \
  /etc/insulavm/config.yaml \
  /etc/gitmaster-actions/profiles.yaml \
  /tmp/gitmaster-smoke/repo.zip develop

# Executar falha intencional (exit 17) e validar o skip do dependente
bash scripts/tests/workflow-parallel-smoke.sh \
  /etc/insulavm/config.yaml \
  /etc/gitmaster-actions/profiles.yaml \
  /tmp/gitmaster-smoke/repo.zip teste-falha
```

**Resultado esperado (branch `develop`):** `preparar`, `paralelo_a`, `paralelo_b`, `unir_paralelos`, `somente_develop` e `finalizar` como `succeeded`; `somente_main`, `bloqueado_por_filtro`, `falha_intencional`, `bloqueado_por_falha` como `skipped`.

**Resultado esperado (branch `teste-falha`):** `falha_intencional` como `failed` com exit code 17; `bloqueado_por_falha` como `skipped` com motivo `dependency_not_succeeded:falha_intencional`; os dois caminhos paralelos e o Job final devem concluir normalmente; a Action inteira termina `failed` como previsto.

O script executa a CLI com `--details`, confere os estados persistidos e trata a falha planejada como sucesso do **teste** (a Action de falha, porém, permanece `failed`). Requer `go`, `zip`, um host KVM/Libvirt funcional e um perfil `ci-smoke` apto a SSH/Cloud-Init.
