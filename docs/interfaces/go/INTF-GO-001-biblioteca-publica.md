<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-GO-001 — API pública GitMaster Actions

**Estado:** API implementada em Go 1.27, com SQLite exclusivo. Testes simulados disponíveis; testes reais InsulaVM/KVM não executados.

## Inicialização e isolamento

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath:     "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
    Workers:          2,
})
if err != nil { return err }
defer engine.Close()
```

`New` precisa obter lock exclusivo de `<DatabasePath>.lock`. Dois Engines não devem abrir o mesmo arquivo ao mesmo tempo. `Close` retorna erro se houver Actions ativas. `RuntimeFactory` injeta runtime simulado em testes. `StartupScripts` deve ser administrado pelo host, nunca pelo YAML.

## Métodos e contratos

| API | Comportamento |
| --- | --- |
| `SubmitYAML(ctx, SubmitRequest)` | Valida formato, origem, perfil e snapshot; cria Action/Jobs/Commands antes de agendar o worker; retorna `pending`. |
| `RunYAML(ctx, []byte)` / `RunFile(ctx, path)` | Compatibilidade síncrona para `action-v1`: usa `SubmitYAML` + `WaitAction`, normalizando caminhos relativos do arquivo. |
| `WaitAction(ctx, id)` | Aguarda estado terminal em SQLite. Timeout do contexto afeta a espera; no wrapper síncrono o cancelamento do contexto cancela a Action. |
| `CancelAction(ctx, id)` | Cancela uma Action ativa no mesmo processo. |
| `GetAction(ctx, id)` / `ListActions(ctx, ActionFilter, Page)` | Recuperam cabeçalhos de Actions, com filtro e paginação. |
| `GetActionDetails(ctx, id)` / `ListActionDetails(ctx, filter, page)` | Recuperam origem, VM, cleanup, Jobs, Commands e eventos. |
| `GetJob(ctx, id)` / `ListJobs(ctx, actionID)` | Consultas de Jobs. |
| `GetCommand(ctx, id)` / `ListCommands(ctx, jobID)` | Consultas de Commands. |
| `ListCommandLogs(ctx, commandID, cursor)` | Até 500 linhas por chamada; cursor `LineNumber` monotônico. |
| `ListCommandOutputs(ctx, commandID, OutputFilter, Page)` | Chunks registrados por stream, paginados por ID. |
| `ListRuntimeEvents(ctx, actionID, Page)` | Eventos de runtime por cursor. |
| `RegisterVMProfile(ctx, ProfileRequest)` | Cadastro de profile InsulaVM usando imagem QCOW2 autorizada. |
| `ListVMProfiles(ctx)` / `GetVMProfile(ctx, name)` | Consulta perfis InsulaVM. |
| `ListVMImages(ctx)` | Lista imagens regulares na pasta administrativa. |
| `ReconcileInterrupted(ctx)` | Tenta cleanup das VMs de Actions terminais com estado pendente/falho. |
| `ImportLegacyHistory(ctx, path)` | Arquiva e normaliza as cinco tabelas legadas do SQLite anterior, sem alterá-lo. |
| `ListLegacyRecords(ctx, source, table, Page)` | Consulta os dados originais tipados. |
| `GetLegacyActionID(ctx, source, oldID)` | Resolve ID legado para UUIDv7 consumível por `GetActionDetails`. |

## Formatos, resultados e erros

Use `SubmitRequest.Format=FormatGitMasterV1` para YAML `version: 1` de `.gitmaster/workflow/`; `FormatActionV1` seleciona `schema_version: 1`. Não existe detecção ou tradução implícita. `ActionOrigin` descreve `clone_id`, SHA completo, branch, tipo de evento, `event_id` e workflow.

`SubmitYAML` retorna um ID persistido; sucesso somente após `WaitAction` devolver `Status=succeeded`. Falhas de VM e comandos tornam o status `failed`, mas a API assíncrona **não transforma automaticamente um estado final de falha em erro Go**. Verifique `Action.Status` e `GetActionDetails`. Wrappers síncronos retornam Action e `error` em falha terminal.

`Page{Limit, AfterID}` usa limite 1..500, cursor por ID; `Action.CursorID` suporta paginação de lista decrescente. O runtime preserva `vm_id` para diagnóstico e estado de cleanup. `*sql.DB` não é exposto pela API `Engine`.

## Referências

[Contrato de workflow](../../contratos/CONTR-003-gitmaster-workflow.md) · [Manifesto local](../../contratos/CONTR-001-configuracao-action.md) · [SQLite](../../persistencia/PERS-001-registro-execucoes.md) · [Guia](../../guias/GUIA-008-integracao-gitmaster.md)