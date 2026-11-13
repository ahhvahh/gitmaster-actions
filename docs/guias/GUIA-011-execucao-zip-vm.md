<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-011 — Processar snapshot ZIP na VM

## Pré-requisitos

- Host Linux com InsulaVM, QEMU/Libvirt e configuração administrativa `/etc/insulavm/config.yaml`.
- Uma ou várias imagens QCOW2 de VMs previamente preparadas e congeladas, com **`unzip` instalado**.
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
