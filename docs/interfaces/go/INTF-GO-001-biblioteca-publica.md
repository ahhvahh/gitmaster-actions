<!-- A-BIOMA-DOC-STANDARD -->

# INTF-GO-001 — API pública da biblioteca

## Objetivo

A API Go gerencia o ciclo de vida das Actions, sem expor `*sql.DB`. Todos os comandos dos workflows são enviados ao runtime InsulaVM. O GitMaster mantém a responsabilidade pelo Git, eventos e seleção do snapshot.

## Inicialização

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath: "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
    ImagesDir: "/var/lib/insulavm/images",
    Workers: 2,
})
```

`Close()` recusa fechamento enquanto houver Actions ativas. `RuntimeFactory` é uma alternativa de testes por mocks; não fornece comandos host. `StartupScripts` é fornecido exclusivamente pelo administrador.

## Operações

| Assinatura no `Engine` | Comportamento |
| --- | --- |
| `SubmitYAML(ctx, SubmitRequest) (*Action, error)` | Valida, copia material e persiste Jobs/Commands antes de agendar worker. Exige formato explícito. |
| `WaitAction(ctx, id) (*Action, error)` | Aguarda status `succeeded`, `failed` ou `cancelled`; erro do contexto interrompe apenas a espera. |
| `CancelAction(ctx, id) error` | Cancela Action ativa neste processo e aciona cleanup. |
| `GetAction(ctx, id)`, `ListActions(ctx, filter, page)` | Consultam cabeçalhos de Action. |
| `GetActionDetails(ctx, id)` | Recupera origem, VM, cleanup, Jobs, Commands e eventos (outputs são paginados à parte). |
| `ListActionDetails(ctx, filter, page)` | Lista detalhes a partir dos cabeçalhos paginados. |
| `GetJob(ctx, id)`, `ListJobs(ctx, actionID)` | Consultam Jobs. |
| `GetCommand(ctx, id)`, `ListCommands(ctx, jobID)` | Consultam Commands. |
| `ListCommandLogs(ctx, commandID, cursor)` | Retorna até 500 linhas por comando. |
| `ListCommandOutputs(ctx, commandID, filter, page)` | Recupera saídas persistidas, na ordem dos registros. |
| `ListRuntimeEvents(ctx, actionID, page)` | Lê eventos de lifecycle com cursor monotônico. |
| `ListVMProfiles(ctx)`, `GetVMProfile(ctx, name)` | Consulta perfis atuais via InsulaVM. |
| `RegisterVMProfile(ctx, ProfileRequest)` | Registra profile de imagem QCOW2 autorizada via InsulaVM. |
| `ListVMImages(ctx)` | Enumera imagens regulares na pasta administrativa. |
| `ReconcileInterrupted(ctx)` | Tenta encerrar VMs previamente identificadas após reinício. |
| `ImportLegacyHistory(ctx, sourcePath)` | Copia tabelas legadas do SQLite antigo em modo somente leitura. |
| `ListLegacyRecords(ctx, source, table, page)` | Permite consultar registros importados sem compartilhar o banco antigo. |

`RunYAML(ctx, []byte)` e `RunFile(ctx, path)` permanecem como APIs locais **síncronas** do formato `action-v1`, preservadas para compatibilidade. O pacote `legacy` utiliza esquemas antigos e não é o caminho recomendado para novas integrações.

## Resultado e estado

`SubmitYAML` retorna `Action.ID` UUIDv7 e `Status=pending` após persistência; `pending` significa **enfileirada, não aprovada**. Workers configuráveis usam esse registro e atualizam status e eventos. `WaitAction` retorna uma Action concluída mesmo quando `Status=failed`: consulte `ErrorMessage` e detalhes para diagnóstico.

`Page{Limit, AfterID}` aceita `Limit` até 500; logs usam número de linha `uint64`. Cada VM tem seu próprio ID registrado em `ActionDetails.VMID`. Falhas no teardown geram `CleanupStatus=failed` e falha final da Action.

## Referências

[Contrato GitMaster](../../contratos/CONTR-003-gitmaster-workflow.md) · [SQLite](../../persistencia/PERS-001-registro-execucoes.md) · [Guia](../../guias/GUIA-008-integracao-gitmaster.md)