<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-005 — Executar workflow GitMaster manualmente usando InsulaVM

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** código Go adicionado; integração ainda sem validação E2E em VM real.

O GitMaster **não** cria VMs com `virsh`/`qemu-img`, não usa `ssh`/`scp` próprios e não administra IP, overlay ou chaves. Essas atividades pertencem ao **InsulaVM**, conforme [GUIA-006](GUIA-006-configurar-insulavm.md).

## Preparação

1. Configurar o InsulaVM e registrar um perfil QCOW2 preparado, como `ci-go`, com Cloud-Init, SSH, Bash, **tar** e **gzip** e ferramentas dos testes.
2. Definir o campo `insula_config_path` no JSON do GitMaster com caminho absoluto para o arquivo `config.yaml` do InsulaVM, ou fornecer `--insula-config`.
3. Criar YAML `workflow.yml` com `vm_profile: ci-go`, `jobs`, `steps` e `needs` opcionais.

## Execução local

```bash
gitmaster action run --file ./workflow.yml --local /srv/projeto \
  --config /etc/a-bioma/gitmaster.json \
  --insula-config /etc/insulavm/config.yaml \
  --log-file ./execucao.log
```

## Execução a partir de GitHub

```bash
gitmaster action run --file ./workflow.yml \
  --repo OWNER/REPO --branch develop \
  --config /etc/a-bioma/gitmaster.json \
  --insula-config /etc/insulavm/config.yaml \
  --log-file ./execucao.log
```

O GitMaster faz uma cópia/snapshot dos arquivos, gera `material.tar.gz`, registra `clone_id` e `action_run_id`, inicia uma action manual via biblioteca InsulaVM, envia o TAR.GZ e o `deploy.sh` após o script de instalação do Cloud-Init, e executa **um step por vez**, respeitando dependências. O checksum manual SHA-256 **não é um SHA Git**. Os resultados ficam no arquivo `--log-file` e no SQLite do GitMaster.

**Diagnóstico:** consultar `listar_actions(clone_id)` para obter jobs, `step_order` e `command_id`, e `listar_respostas_comando(clone_id,command_id)` para respostas duráveis, inclusive passos anteriores ao erro. `runtime_events` registra etapas de inicialização e deploy.

**O comando não aceita mais:** `--ssh-user`, `--ssh-key`, `--known-hosts`, `--ssh-port`, `--ssh-host-alias`, `--network`, `--vm-disks-dir`. A biblioteca InsulaVM controla essas configurações.

**Documentação principal:** [GUIA-006](GUIA-006-configurar-insulavm.md), [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md), [INTF-TERM-003](../interfaces/terminal/INTF-TERM-003-executar-action-manual.md).
