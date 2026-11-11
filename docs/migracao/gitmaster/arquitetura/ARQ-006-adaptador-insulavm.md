<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-006 — Integração do GitMaster com InsulaVM

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** adaptador Go, worker de fila, execução manual via InsulaVM, TAR.GZ por SHA, respostas individuais e registro de eventos implementados no código `develop`. **Não há confirmação de build/CI aprovada nem execução real em VM** nesta revisão. A biblioteca InsulaVM utilizada está fixada no `go.mod`. Contrato externo é experimental, sem versão estável v1.

**Revisão de dependência (2026-11-10):** GitMaster fixa `github.com/ahhvahh/InsulaVM` em `v0.0.0-20261110231312-983c4a8b46c7`, commit `983c4a8b46c7ee1a2f7ebd3d45de41aa3589b397` da `main`. As assinaturas implementadas de `New`, `GetProfile`, `StartVMWithOptions`, `DeployToVM`, `ExecVM`, `StopVM` e `Configuration` foram verificadas no código; build e integração KVM ainda não foram validados localmente.

**Filtros de jobs por branch (branch `gm-atualizar-insulavm`):** cada `jobs.<nome>.branches` pode conter `include` e `exclude` de nomes exatos de branch. A descoberta usa o YAML do SHA do commit e registra jobs excluídos como `skipped` com `skip_reason` no SQLite, incluindo dependentes de jobs excluídos. Não inicia VM quando nenhum job fica ativo. O filtro `triggers[].branches` por workflow continua válido. Detalhes em [GUIA-007](../guias/GUIA-007-workflows-por-branch.md).
## Publicação após validação de actions

A API MCP `public` aplica gate obrigatório antes de `gitops.Push` usando os resultados permanentes dos workflows que correspondem ao HEAD imutável. Não confundir uma tentativa de validação `push` com evento de transmissão já ocorrida: o SQLite mantém o estado `validating` até o resultado real do envio. A ausência de YAML é bloqueio por padrão; `queued` não é sucesso. A única interface MCP de publicação é `public`. Veja [CONTR-010](../contratos/sockets/CONTR-010-publicar.md).

## Decisão arquitetural

**O InsulaVM é o único responsável pela virtualização e operações internas de VM**: perfis, QEMU/libvirt, Cloud-Init, SSH, importação, execução e descarte. O GitMaster **não** executa `virsh`, `qemu-img`, `ssh` ou `scp`, não gera XML de domínio, não faz polling de DHCP e não manipula overlays QCOW2.

O GitMaster é o orquestrador: interpreta YAML, captura o SHA, monta TAR.GZ do snapshot exato, conserva eventos e fila, controla dependências `needs`, persiste comandos/status/logs no SQLite e permite auditoria via MCP. A integração ocorre pela **biblioteca Go no mesmo processo**, não via HTTP ou outro daemon.

| Responsabilidade | GitMaster | InsulaVM |
| --- | --- | --- |
| Evento commit ou push, SHA e branch | **Sim** | Não |
| Ler e validar `.gitmaster/workflow/*.yaml` do commit | **Sim** | Não |
| TAR.GZ da árvore Git, sem dados não commitados | **Sim** | Recebe |
| Lista de jobs, `needs`, ID de step e status | **Sim** | Executa um por vez |
| Perfil de SO/QCOW2, CPU/RAM/disco | Referencia nome | **Fonte de verdade** |
| Inicializar, isolar, autenticar e encerrar VM | Requisita pela API | **Sim** |
| Executar script de instalação durante Cloud-Init | Indica caminho de script administrativo por perfil | **Sim, antes do deploy** |
| Importar e extrair TAR.GZ + verificar deploy | Prepara pacote/script de verificação | **Sim** |
| Executar comandos no guest | Encaminha comando/working dir | **Sim** |
| Logs físicos da infraestrutura VM | Apenas lê eventos retornados | **Sim** |
| Resultados e respostas por `action_run_id/job/command_id` | **SQLite do GitMaster** | Fornece fluxo e erro |

## Biblioteca e método usado

Pacote: `github.com/ahhvahh/InsulaVM/insulavm`. Wrapper e worker no código [insula_worker.go](../../src/go/internal/mcpserver/insula_worker.go).

1. `insulavm.New(insula_config_path)` carrega a configuração global.
2. `Engine.GetProfile(vm_profile)` valida a existência do perfil já preparado.
3. `Engine.StartVMWithOptions(ctx,VMStartOptions{Profile:vm_profile,StartupScriptPath:script_path})` cria a VM descartável com o script administrativo incorporado ao **Cloud-Init da primeira inicialização**. O InsulaVM espera o script terminar com sucesso **antes de retornar**. GitMaster persiste `insula_vm_id` e os eventos de bootstrap.
4. `Engine.DeployToVM(ctx,VMDeployRequest{SourcePath:material.tar.gz,ScriptPath:deploy.sh,Extract:true})` envia o **TAR.GZ**, extrai nativamente pelo próprio InsulaVM em `guest.destination` e executa um pequeno script de verificação. Esse `deploy.sh` **não é o script de instalação**; a instalação já foi executada pelo Cloud-Init na criação.
5. Para cada step, `Engine.ExecVM(ctx,ExecRequest{VMID,Workspace,Command,Output})` executa o comando remotamente. Cada chamada aguarda a anterior e devolve sucesso ou erro; o adaptador extrai `ssh.ExitError.ExitStatus()` quando disponível. `stderr` isolado não significa falha.
6. `Engine.StopVM(ctx,insula_vm_id)` encerra e remove a instância. Em erro, o GitMaster conserva diagnóstico e marca a action como falha; a reinicialização tenta reconciliar somente os IDs associados a actions interrompidas.

**Não usar `Engine.Run` como caixa-preta** para jobs GitMaster: ele é bloqueante e agregaria controle por job fora do esquema atual. A execução pelo par `DeployToVM`/`ExecVM` preserva `command_id`, `step_order`, exit code e logs permanentes de cada step.

## Origem imutável

Quando o evento é automático, [action_archive.go](../../src/go/internal/gitops/action_archive.go) executa `git archive --format=tar <commit_sha>` e comprime com GZIP para produzir **material.tar.gz** da árvore identificada pelo SHA. Antes, rejeita symlinks e submódulos, e limita o TAR.GZ a 256 MiB. O pacote não contém `.git` nem mudanças não commitadas. `git` é usado **somente no host para criar material**, nunca para controlar VM.

Na CLI manual, o GitMaster prepara snapshot local ou baixa **TAR.GZ** de um branch remoto, materializa o conteúdo e gera um **TAR.GZ** verificado do snapshot, identificado por checksum; esse checksum não é um SHA de commit Git.

## Perfis reutilizáveis

`vm_profile` do YAML é o **nome do perfil InsulaVM**, não mais um registro autônomo `vm_profiles` que governa provisionamento.

O InsulaVM guarda `profile.yaml` + imagem QCOW2 (Debian/Go/testes) + valores de RAM, vCPUs e disco. `FreezeVM` cria um perfil novo a partir de VM preparada, para reutilização por várias instâncias independentes. A API `StartVMWithOptions` suporta overrides de RAM/vCPUs e **`StartupScriptPath` no momento da criação**; o adaptador passa o perfil e o script administrativo configurado no GitMaster, mantendo RAM/vCPUs do perfil. **Não há override de disco por execução.**

**Script de instalação no momento da criação:** o formato `profile.yaml` continua sem `bootstrap_script` nativo, mas o GitMaster associa cada nome de perfil a um caminho absoluto em `insula_startup_scripts` (JSON administrativo) e entrega esse arquivo **na chamada `StartVMWithOptions`**. O InsulaVM incorpora o conteúdo ao seed Cloud-Init, executa como root no primeiro boot e só libera a VM após confirmação. O script nunca vem do YAML ou do TAR.GZ do repositório. Os jobs de testes começam após bootstrap e transferência do material.

## Destino dos arquivos

Configurar em `InsulaVM/config.yaml`:

```yaml
guest:
  destination: /tmp/gmaster
```

O destino é global do InsulaVM e pertence ao usuário SSH preparado. O deploy extrai em `/tmp/gmaster`; `ExecVM.Workspace` utiliza essa raiz e aplica os `working_directory` relativos do YAML. Cada instância usa um overlay independente; não compartilhar instância entre actions.

**Pré-requisito:** a imagem-base do perfil deve ter `bash`, `tar`, `gzip`, Cloud-Init, SSH e suporte a rede previstos no InsulaVM. O script administrativo pode instalar SDKs e ferramentas de teste na primeira inicialização. Não executar repositórios não confiáveis com `sudo_mode: passwordless`, rede irrestrita, credenciais de host ou segredos de outros serviços.

## Persistência e recuperação

- `action_runs`: `insula_vm_id`, `event_id`, status, datas e workflow.
- `action_runtime_messages`: eventos ordenados de perfil, arquivo, VM, deploy, erro e limpeza; até 500 incluídos em cada retorno `listar_actions` (a paginação deste histórico é futura).
- `action_jobs` e `action_job_commands`: estados, `step_order`, `command_id` e `exit_code`.
- `action_command_responses`: stdout, stderr, system e error, sequenciais e permanentes, paginadas pela tool `listar_respostas_comando`.
- `tool_executions`/`tool_execution_messages`: diagnósticos MCP temporários, distintos dos eventos duráveis da action.

Uma ação enfileirada é reivindicada no SQLite **antes** do provisionamento para reduzir VMs duplicadas. O worker é único por instância do daemon e processa uma action por vez, verificando a fila a cada 2 segundos. Na reinicialização, actions que estavam `running` viram `interrupted`; o serviço solicita ao InsulaVM limpeza apenas dos IDs vinculados. Não há retomada automática silenciosa de ações interrompidas.

**Configuração obrigatória para worker:** `insula_config_path` no JSON do GitMaster, caminho absoluto válido para `config.yaml` do InsulaVM. Sem esse campo, o serviço mantém commit/push e enfileiramento mas **não executa** VMs. Nenhuma chamada de commit fica esperando a VM terminar.

## Limitações e pendências

- CI do GitMaster precisa de compilação, formatação e validação de dependência do InsulaVM; não há teste de hipervisor real nesta revisão.
- O InsulaVM ainda não retorna stdout/stderr estruturados por callback e código de saída como campo numérico público. O adaptador lê os prefixos textuais de `Output io.Writer` e extrai `*ssh.ExitError` do erro; esse parsing requer contrato estável futuro.
- Sem quota global de VMs, sem mascaramento completo de segredos em logs, sem política de retenção da tabela `action_runtime_messages`. O `profile.yaml` não persiste o script: ele é associado pelo mapa administrativo `insula_startup_scripts` e enviado no início da VM.
- `vm_profiles` SQLite legado do GitMaster permanece para migração, mas **não é consultado pelo executor**; o cadastro/listagem administrativa foi redirecionado ao InsulaVM. Perfis pré-existentes devem ser cadastrados no InsulaVM para ficarem executáveis.
- Não validar a execução dos jobs somente por eventos `queued`; consultar ações/comandos e seus códigos de saída.

**Relacionados:** [FLUX-006](../fluxos/FLUX-006-commit-action-insulavm.md), [GUIA-006](../guias/GUIA-006-configurar-insulavm.md), [CONTR-021](../contratos/sockets/CONTR-021-eventos-commit-push.md), [PERS-002](../persistencia/PERS-002-execucao-actions.md), [REF-003](../referencia/REF-003-estado-real-do-codigo.md).

**InsulaVM:** [API Go](https://github.com/ahhvahh/InsulaVM/blob/main/docs/arquitetura/ARQ-004-biblioteca-go.md) e [DeployToVM](https://github.com/ahhvahh/InsulaVM/blob/main/insulavm/deploy.go).
