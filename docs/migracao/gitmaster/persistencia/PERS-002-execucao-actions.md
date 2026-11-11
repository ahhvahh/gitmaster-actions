<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# PERS-002 — Persistência de GitMaster Actions, jobs e comandos

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** tabelas, eventos Git, fila, worker InsulaVM, logs por comando e vínculo de VM implementados no código `develop`. **Sem validação de VM real e sem CI aprovada nesta revisão.** O worker só opera com `insula_config_path` configurado.

## Estado de tentativa de publicação

`action_events` recebe agora colunas aditivas `state TEXT NOT NULL DEFAULT 'recorded'` e `validation_digest TEXT NOT NULL DEFAULT ''`, migradas preservando os registros existentes. O estado `validating` identifica tentativa de envio com testes em andamento; `blocked` indica nenhuma publicação autorizada; `published` só pode ser registrado após `git push` efetivo. A coluna `validation_digest` impede reutilização de action aprovada quando InsulaVM, perfil e bootstrap administrativo mudarem para o mesmo SHA. Os resultados detalhados continuam em `action_runs`, `action_jobs`, `action_job_commands`, `action_command_responses` e `action_runtime_messages`.

## Separação entre dados duráveis e logs temporários

Os estados e resultados dos workflows permanecem no SQLite após reiniciar. **As respostas de stdout/stderr, eventos e erros da VM são gravadas permanentemente em `action_command_responses`.** Em paralelo, as mensagens resumidas de diagnóstico MCP permanecem em `tool_executions` e `tool_execution_messages`, com retenção padrão de 5h e limpeza dessas duas tabelas ao iniciar o serviço.

O campo `tool_executions.log_type TEXT NOT NULL DEFAULT 'operation'` classifica os registros como `operation` ou `action`. Há migração aditiva para bases normalizadas anteriores. `action_job_commands.log_execution_id` possui FK `ON DELETE SET NULL` para preservar status, código de saída e datas mesmo depois da exclusão dos logs.

## Tabela `action_events`

| Coluna | Tipo / significado |
| --- | --- |
| `event_id` | TEXT PK; UUIDv7 do evento |
| `clone_id` | TEXT FK → `clones.id` |
| `event_type` | TEXT: `commit` ou `push` |
| `commit_sha` | TEXT: SHA da árvore Git associada |
| `branch` | TEXT: branch de origem |
| `created_at` | TEXT UTC |

Unicidade `(clone_id,event_type,commit_sha)` para evitar disparos duplicados em novas chamadas MCP. `commit` e `push` sobre o mesmo SHA representam **eventos diferentes**.

## Tabela `action_runs`

| Campo | Tipo e comportamento |
| --- | --- |
| `action_run_id` | TEXT PK, UUIDv7 |
| `clone_id` | TEXT FK para `clones.id`, exclusão por cascata quando clone é explicitamente eliminado |
| `workflow_path` | TEXT NOT NULL: arquivo sob `.gitmaster/workflow/` |
| `commit_sha` | TEXT NOT NULL; revisão exata do código |
| `branch` | TEXT NOT NULL |
| `vm_profile` | TEXT NOT NULL; **uma VM por action**, compartilhada pelos jobs |
| `event_id` | TEXT NULL FK → `action_events.event_id`; `NULL` para execução manual sem evento |
| `insula_vm_id` | TEXT NULL, UUID da instância descartável retornada por InsulaVM; nunca representa recurso criado diretamente pelo GitMaster |
| `scope`, `owner_uid` | TEXT / INTEGER: autorização de consulta por socket e identidade Linux |
| `status` | queued, running, succeeded, failed, cancelled, interrupted |
| `created_at`, `started_at`, `finished_at` | TEXT UTC; fim/início nulos enquanto não aplicáveis |

Unicidade `(event_id,workflow_path)` para ações disparadas por eventos, permitindo **commit e push do mesmo SHA** sem conflito. Para execuções manuais (`event_id IS NULL`), permanece um índice único parcial em `(clone_id,commit_sha,workflow_path)`. Índices por status, evento e clone ajudam consultas filtradas. Migração aditiva/reconstrução da tabela legada preserva jobs, comandos e respostas e executa `PRAGMA foreign_key_check`.

## Tabela `action_runtime_messages`

Os eventos de **preparação e ciclo da VM InsulaVM** ficam numa tabela durável, separada das respostas de comandos.

| Campo | Tipo |
| --- | --- |
| `id` | INTEGER PK AUTOINCREMENT, sequência de gravação |
| `action_run_id` | TEXT FK → `action_runs.action_run_id` ON DELETE CASCADE |
| `stage` | TEXT: perfil, arquivo, start, deploy, jobs, stop, error, recovery e canais do deploy |
| `message` | TEXT, mensagem de runtime sanitizada |
| `recorded_at` | TEXT UTC |

`listar_actions` inclui `runtime_events`, limitada atualmente às primeiras 500 mensagens da action. Ainda falta paginação/retensão própria para históricos muito grandes. Falhas antes de iniciar qualquer step ficam nesta tabela, evitando diagnóstico vazio no comando.

## Tabela `action_jobs`

| Campo | Tipo e comportamento |
| --- | --- |
| `id` | INTEGER PK AUTOINCREMENT |
| `action_run_id` | TEXT FK → `action_runs.action_run_id` ON DELETE CASCADE |
| `job_name` | TEXT NOT NULL; único dentro da action |
| `needs_json` | TEXT NOT NULL; dependências definidas no YAML |
| `skip_reason` | TEXT NOT NULL DEFAULT ''; `branch_filter` ou `dependency_filtered:<job>` para jobs excluídos por branch, com migração aditiva |
| `status` | queued, running, succeeded, failed, skipped, cancelled, interrupted |
| `exit_code` | INTEGER NULL; o executor atual grava o código do comando que falhou, quando disponível; não significa conclusão do processo VM em todas as exceções |
| `started_at`, `finished_at` | TEXT UTC NULL |

Cada action possui um ou mais jobs. **Jobs não possuem VM separada**; todos usam a instância da action.

## Tabela `action_job_commands`

| Campo | Tipo e comportamento |
| --- | --- |
| `command_id` | TEXT PK, UUIDv7 |
| `job_id` | INTEGER FK → `action_jobs.id` ON DELETE CASCADE |
| `step_order` | INTEGER NOT NULL; índice sequencial no job |
| `step_name` | TEXT NOT NULL |
| `working_directory` | TEXT NOT NULL; relativo ao diretório do clone dentro do guest |
| `command_text` | TEXT NOT NULL; script interno do step, **não exposto pela consulta MCP** |
| `status` | queued, running, succeeded, failed, skipped, cancelled, interrupted |
| `exit_code` | INTEGER NULL; código retornado pelo processo guest |
| `started_at`, `finished_at` | TEXT UTC NULL |
| `log_execution_id` | TEXT NULL, FK → `tool_executions.execution_id` ON DELETE SET NULL |

Unicidade `(job_id,step_order)` e índice `(job_id,step_order)`. Cada comando iniciado possui log UUIDv7 próprio do tipo `action`; linhas de stdout/stderr são armazenadas em `tool_execution_messages` por ordem `id`, até 2.000 mensagens por execução. A chamada `detalhes_execucao` recupera o log pelo identificador respeitando UID/socket.

Os scripts ficam no banco e devem ser tratados como código não confiável; nunca devem ser executados diretamente no host. A VM só executará workflows aprovados pelo parser versionado.

## Tabela `action_command_responses`

| Campo | Tipo e comportamento |
| --- | --- |
| `id` | INTEGER PK AUTOINCREMENT, ordem global de gravação |
| `command_id` | TEXT FK → `action_job_commands.command_id` ON DELETE CASCADE |
| `sequence` | INTEGER NOT NULL, ordem incremental da resposta dentro do comando |
| `stream` | TEXT: `stdout`, `stderr`, `system` ou `error` |
| `message` | TEXT NOT NULL, linha sanitizada (inclusive vazia) |
| `recorded_at` | TEXT NOT NULL, data UTC |

O índice `(command_id,sequence)` permite leitura ordenada e paginada; a unicidade `(command_id,sequence)` é garantida no banco. A gravação só aceita um comando no estado `running`. As respostas não são limitadas às 2.000 linhas dos logs MCP transitórios, mas precisam de quotas/rotina de retenção antes de produção. **Esse histórico permanente é diferente de `tool_execution_messages`**. O contrato [CONTR-020](../contratos/sockets/CONTR-020-listar-respostas-comando.md) apresenta a consulta por clone, comando, UID, socket e cursor.

**Status:** os enums existentes são mantidos por compatibilidade: `queued` (pendente), `running` (execução), `succeeded` (executado/finalizado com sucesso), `failed` (falha). Para jobs/comandos também há `skipped` e `interrupted`. `exit_code` do job recebe o código do comando que falhou. Uma saída em `stderr` isoladamente não define falha; o código de saída ou erro de transporte define.

## Reinício e recuperação

Na inicialização, `ResetExecutionLogs` limpa somente as duas tabelas temporárias, preservando `action_command_responses`. `InterruptRunningActions` marca `action_runs`, `action_jobs` e `action_job_commands` em execução como `interrupted` e anula automaticamente as referências a logs apagados. Jobs ainda pendentes em uma action interrompida também devem ser marcados como interrompidos, para impedir retomada silenciosa. Actions apenas `queued` permanecem na fila até política de reprocessamento explícita.

Um resultado registrado como `succeeded` **não depende da disponibilidade posterior dos logs**. Não considerar o job finalizado enquanto não se conhecer o código de saída real do guest.

## Execução pela biblioteca InsulaVM

O handler `criar_commit` enfileira actions de commit; `public` enfileira e aguarda a validação local pré-push. Quando `insula_config_path` existe no daemon, `StartInsulaWorker` consome actions do evento, reivindica o status `running`, gera TAR.GZ do SHA, pede `StartVMWithOptions` com `StartupScriptPath` para instalação no Cloud-Init, depois `DeployToVM(Extract:true)` e `ExecVM` para cada step e finaliza com `StopVM`. O UUID da instância fica em `insula_vm_id` e etapas pré-comando em `action_runtime_messages`. **Todas as operações na VM são InsulaVM; não há mecanismo GitMaster próprio de libvirt/SSH.** A execução real e a CI ainda precisam ser validadas. 

A limpeza em startup marca actions em execução como interrompidas e tenta encerrar somente VMs InsulaVM vinculadas a essas actions. Ainda faltam versionamento/digest do perfil, quota de VMs e validação de falhas de teardown.

**Situação:** o agendamento disparado por commit/push cria **um `action_job_commands` por step YAML** e o worker InsulaVM agora pode executá-los, quando habilitado pela configuração. A CLI `gitmaster action run` usa o mesmo adaptador; ambos registram cada step separadamente, com código de saída e histórico durável. Ver [REF-003](../referencia/REF-003-estado-real-do-codigo.md).

**Arquitetura InsulaVM:** [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md). **Relacionados:** [CONTR-021 — eventos e listagem](../contratos/sockets/CONTR-021-eventos-commit-push.md), [CONTR-020 — respostas sequenciais](../contratos/sockets/CONTR-020-listar-respostas-comando.md), [CONTR-019 — resultado por comando](../contratos/sockets/CONTR-019-resultados-comandos-action.md), [PERS-003 — perfis](PERS-003-perfis-vm.md), [FLUX-005 — execução](../fluxos/FLUX-005-executar-workflow-vm.md), [CONTR-016 — consulta por clone](../contratos/sockets/CONTR-016-gitm-action.md).
