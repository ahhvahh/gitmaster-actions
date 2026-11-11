<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-006 — Preparar InsulaVM e executar testes pelo GitMaster

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** comandos e configuração verificados no código GitMaster/InsulaVM; validação real de hipervisor/guest não concluída. Requer Linux com KVM/libvirt/Cloud-Init, rede e permissões adequadas.

## 1. Preparar o InsulaVM

O InsulaVM precisa estar instalado **no mesmo host do daemon GitMaster**, ou em ambiente que exponha os recursos de virtualização local à biblioteca Go. Não há transporte remoto HTTP/MCP entre os processos neste fluxo.

Em `/etc/insulavm/config.yaml` (exemplo de localização), configure a raiz, rede Libvirt e o diretório de trabalho da VM:

```yaml
storage:
  root: /var/lib/insulavm
  profiles: profiles
  runs: runs
  logs: logs
  database: insula.db
guest:
  destination: /tmp/gmaster
virtualization:
  provider: kvm
  libvirt_uri: qemu:///system
  network: default
  default_disk_gb: 16
ssh:
  user: insula
  connect_timeout_seconds: 60
  sudo_mode: disabled
```

Verifique as permissões da identidade Linux que executa o GitMaster sobre o armazenamento do InsulaVM, `qemu:///system` e a rede desejada. A biblioteca usa suas próprias credenciais por VM. **Não** configure `--ssh-key`, `--known-hosts` ou `--network` no GitMaster.

O perfil deve ter imagem QCOW2 com Linux, Cloud-Init, SSH, `bash`, **`tar`** e `gzip`; uma ISO instaladora não é um perfil executável pronto. Para criar um perfil a partir de imagem já instalada:

```bash
insula profile register -config /etc/insulavm/config.yaml \
  -name ci-go -image /srv/imagens/debian13-go.qcow2 \
  -memory-mib 4096 -vcpus 2 -disk-gb 20 -os debian
insula profile list -config /etc/insulavm/config.yaml
insula profile show -config /etc/insulavm/config.yaml -name ci-go
```

Outra opção é iniciar uma VM gerenciada no InsulaVM, instalar os SDKs e usar `insula vm freeze -id UUID -name ci-go`. Isso produz uma imagem-base reutilizável e **encerra a VM de origem**. O InsulaVM suporta agora `VMStartOptions.StartupScriptPath`: o script administrativo é entregue no momento da criação, executado por Cloud-Init como root e precisa terminar antes de o GitMaster iniciar os testes. O arquivo `profile.yaml` não guarda esse script; sua associação ao perfil fica no JSON administrativo GitMaster.

## 2. Conectar o GitMaster

Adicionar ao arquivo de configuração JSON do GitMaster, mantendo as demais opções obrigatórias:

```json
{
  "insula_config_path": "/etc/insulavm/config.yaml",
  "insula_startup_scripts": {
    "ci-go": "/etc/a-bioma/gitmaster/bootstrap/ci-go.sh"
  }
}
```

O script `ci-go.sh` precisa ser um arquivo regular, absoluto e protegido contra alterações por usuários do repositório. O InsulaVM rejeita symlink, script vazio, NUL e conteúdo maior que 256 KiB. O GitMaster o passa **à criação da VM**, não durante o upload do projeto. Se o mapa não tiver entrada para um perfil, a VM inicia com a imagem-base sem script extra.

A propriedade `insula_config_path` pode estar ausente para operações de leitura e commits locais; nesse caso, `criar_commit` ainda registra actions `queued`, porém o método `public` **bloqueia a publicação** sem validação InsulaVM. Quando configurada, o daemon inicia um worker InsulaVM, que processa uma action por vez, verificando a fila a cada ~2 segundos.

A administração `listar_perfis_vm` consulta agora a biblioteca InsulaVM. `salvar_perfil_vm` registra um **perfil novo** no InsulaVM a partir de uma imagem QCOW2 aprovada no diretório `vm_images_dir`; não edita um perfil existente nem altera a imagem-base. A tabela GitMaster `vm_profiles` antiga não governa o worker.

### Script de instalação por perfil

Prepare no host `/etc/a-bioma/gitmaster/bootstrap/ci-go.sh`, mantido somente pelo administrador:

```bash
#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates git make gcc tar gzip
# Instale aqui a versão de Go/SDK necessária ao seu projeto.
```

O script é incorporado ao seed Cloud-Init da nova VM e executado como **root durante a primeira inicialização**, antes do upload do projeto. Caso falhe, a criação não entrega uma VM pronta e nenhum job é iniciado. Não enviar scripts de instalação provenientes do YAML do repositório.

## 3. Criar workflow no repositório

Criar e commitar `.gitmaster/workflow/go-ci.yml`:

```yaml
version: 1
name: go-ci
vm_profile: ci-go
triggers:
  - event: commit
    branches: [develop]
jobs:
  unit:
    steps:
      - name: Dependencias
        working_directory: src/go
        run: go mod download
      - name: Testes
        working_directory: src/go
        run: go test ./...
  build:
    needs: [unit]
    steps:
      - name: Compilar
        working_directory: src/go
        run: go build ./...
```

O nome `ci-go` deve existir no catálogo do InsulaVM. Cada YAML corresponde a **uma VM**, compartilhada por seus jobs; cada step tem comando, código de saída e histórico próprios.

## 4. Solicitar commit ao GitMaster

Após preparar alterações por `aplicar_alteracoes`, chamar:

```json
{
  "clone_id": "<clone-id>",
  "batch_id": "<batch-id>",
  "expected_head_sha": "<head-atual>",
  "title": "test: adicionar verificacoes",
  "description": "Atualiza a pipeline de testes automatizados."
}
```

A tool `criar_commit` retorna `commit_sha`, `event_id`, `event_status`, `action_detected` e `action_run_ids[]`. **Não** espere conclusão da VM nessa chamada; `queued` só confirma persistência da fila.

O worker prepara `material.tar.gz` do novo SHA e solicita ao InsulaVM `StartVMWithOptions(Profile:"ci-go", StartupScriptPath:"/etc/a-bioma/gitmaster/bootstrap/ci-go.sh")`. **Somente depois que o Cloud-Init confirma a instalação**, envia o TAR.GZ e `deploy.sh`; o InsulaVM extrai nativamente em `/tmp/gmaster` e executa os três comandos, **um por vez**, respeitando `needs`. O InsulaVM encerra a VM no final. O GitMaster não chama QEMU/SSH diretamente.

## 5. Consultar status, falha e o step anterior

```bash
gitmaster call listar_actions '{"clone_id":"<clone-id>"}'
gitmaster call listar_actions '{"clone_id":"<clone-id>","status":"failed"}'
gitmaster call listar_respostas_comando '{"clone_id":"<clone-id>","command_id":"<step-falho>","after_sequence":0,"limit":100}'
gitmaster call listar_respostas_comando '{"clone_id":"<clone-id>","command_id":"<step-anterior>","after_sequence":0,"limit":100}'
```

A resposta de `listar_actions` inclui `insula_vm_id`, `runtime_events` de bootstrap/limpeza, jobs e comandos. Os logs por comando são duráveis e têm campo `sequence`. Uma falha de transferência ou boot fica em `runtime_events`, mesmo se nenhum step tiver sido iniciado.

## 6. Testar manualmente por console

Para testar sem esperar commit:

```bash
gitmaster action run --file ./workflow.yml --local /srv/projeto \
  --config /etc/a-bioma/gitmaster.json \
  --insula-config /etc/insulavm/config.yaml \
  --log-file ./execucao.log
```

Ou buscar conteúdo de uma branch GitHub:

```bash
gitmaster action run --file ./workflow.yml --repo OWNER/REPO --branch develop \
  --config /etc/a-bioma/gitmaster.json \
  --insula-config /etc/insulavm/config.yaml \
  --log-file ./execucao.log
```

A CLI registra action manual e usa **o mesmo adaptador InsulaVM** do worker. O arquivo de log contém mensagens; o SQLite conserva os IDs e resultados. TAR.GZ manual é materializado da pasta/branch selecionada e recebe checksum SHA-256, **não o SHA de um commit Git**.

## 7. Verificar requisitos antes da produção

- Confirmar `go mod tidy`, `gofmt`, `go test ./...` e testes integrados no GitMaster após a dependência do InsulaVM ser resolvida.
- Executar um teste real com VM `ci-go` e Cloud-Init, verificar a execução do script de instalação e a extração nativa do TAR.GZ; verificar que o primeiro, segundo e terceiro step geram logs separados.
- Testar falha de teste (exit code !=0), erro de bootstrap, cancelamento, timeout, cleanup e reinicialização do daemon.
- Nunca permitir que workflows de repositório decidam imagens/libvirt/redes ou executem comandos como root do host.

**Documentação técnica:** [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md), [FLUX-006](../fluxos/FLUX-006-commit-action-insulavm.md), [CONTR-021](../contratos/sockets/CONTR-021-eventos-commit-push.md), [Perfil InsulaVM](https://github.com/ahhvahh/InsulaVM/blob/main/docs/arquitetura/ARQ-002-perfis-vm.md).
