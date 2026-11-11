<!-- A-BIOMA-DOC-STANDARD -->

# PERS-001 — SQLite exclusivo para Actions

## Objetivo

O `Engine` gerencia `actions.db`, sem fornecer `*sql.DB` ao consumidor. `New` aplica migrações **aditivas** sobre as tabelas anteriores, preservando IDs e saídas preexistentes. A única responsabilidade do GitMaster é fornecer YAML e metadados de origem.

## Tabelas

| Tabela | Chave | Conteúdo |
| --- | --- | --- |
| `actions` | `id` UUIDv7 | Nome, status, timestamps, erro, `clone_id`, `commit_sha`, `event_id`, `event_type`, `branch`, `workflow_name`, `workflow_path`, `yaml_format`, `vm_profile`, `vm_id`, SHA256 do material, caminho privado temporário e estado/erro de cleanup |
| `jobs` | `id` UUIDv7 | `action_id`, nome, ordem, dependências JSON, filtro/skip, timeout, status e timestamps |
| `commands` | `id` UUIDv7 | `job_id`, step, diretório relativo, comando autorizado, posição, status, exit code, failure_kind, erro e timestamps |
| `command_outputs` | `id` INTEGER | Chunks capturados de stdout/stderr (limitação: transporte textual decorado) |
| `command_log_lines` | `(command_id,line_no)` | Projeção de linhas com cursores incrementais |
| `runtime_events` | `id` INTEGER | Fases da execução, diagnóstico e cleanup por `action_id` |
| `legacy_archive` | `id` INTEGER | Linhas das cinco tabelas legadas importadas, com valores tipados em JSON e BLOBs base64 |

Relacionamentos: `actions 1:N jobs 1:N commands 1:N outputs/log_lines`; `actions 1:N runtime_events`. SQLite ativa `foreign_keys`, WAL, `busy_timeout` e usa uma conexão serializada. Os nomes das tabelas de dados legados ficam isolados no arquivo `actions.db`, sem foreign keys para clones de GitMaster.

`idx_actions_origin_event` é UNIQUE em `(clone_id, commit_sha, workflow_path, event_type)` para origens informadas. Uma submissão idempotente retorna o mesmo ID e não inicia outra VM. `AfterID` em outputs/eventos representa cursor, não OFFSET.

## Interrupção e retenção

No próximo `New`, registros `pending`, `starting` ou `running` anteriores passam a `failed` com diagnóstico de interrupção. VMs cujos IDs foram persistidos ficam com `cleanup_status=pending` até `ReconcileInterrupted` tentar finalização. A biblioteca **não retoma** steps interrompidos e não garante cleanup da VM quando o ID nunca chegou a ser persistido.

O material de execução é copiado antes do enqueue para `actions-material/` com permissões restritas e é removido ao concluir a Action. Permanece seu hash SHA256, não uma cópia do repositório. O consumidor deve fornecer snapshot imutável e seguro; políticas de retenção de logs e autorização de leitura cabem ao serviço consumidor.

## Migração do histórico

`ImportLegacyHistory` abre um **SQLite antigo em modo somente leitura** e importa `action_runs`, `action_jobs`, `action_job_commands`, `action_command_responses`, `action_runtime_messages` para `legacy_archive`. Importações repetidas atualizam a mesma chave de origem, sem duplicar linhas. A API `ListLegacyRecords` recupera os registros completos, inclusive dados binários em base64. **A normalização desses registros no modelo `ActionDetails` ainda não foi implementada;** não desligar o modo legado do GitMaster antes de adaptar a leitura e validar a migração operacional.

## Referências

[API](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Workflow](../contratos/CONTR-003-gitmaster-workflow.md)