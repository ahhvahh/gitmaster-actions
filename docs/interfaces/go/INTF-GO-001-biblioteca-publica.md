<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-GO-001 — API pública GitMaster Actions

**Estado:** API implementada em Go 1.27, com SQLite exclusivo. Testes simulados disponíveis; testes reais InsulaVM/KVM não executados.

## Inicialização e isolamento

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath:     "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
    ProfilesConfigPath: "/etc/gitmaster-actions/profiles.yaml",
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
## Perfis congelados e snapshots ZIP

`Options.ProfilesConfigPath` aponta um YAML confiável no servidor que declara um ou vários perfis (`name`, `image_path`, `memory_mib`, `vcpus`, `workspace`). `LoadVMProfilesConfig(path)` valida esse catálogo. `NewRuntimeWithProfiles(insulaConfig, profilesConfig)` inicia o adaptador. Ao selecionar um perfil ainda não registrado, o InsulaVM copia o QCOW2 para seu armazenamento gerenciado; imagens já registradas não são substituídas automaticamente.

`SubmitRequest.ArchivePath` recebe o caminho **absoluto** do ZIP do repositório ou TAR.GZ legado. `ActionOrigin.Branch` habilita os filtros de branch. Os Jobs/Commands são persistidos antes de executar a VM. O workspace por perfil é usado na extração e na execução de cada comando. `DeployToVM` da InsulaVM recebe apenas TAR.GZ; para ZIP o adaptador usa `ImportToVM` e `ExecVM` para executar `unzip` no convidado. `unzip` deve existir na imagem congelada.

## Conversor de YAML em pares Job/Command

API **pura** `ConvertYAML(yamlData []byte, opts ConvertOptions) ([]CommandPair, error)` disponível no módulo raiz e em `/src`; não necessita `Engine`, SQLite, snapshot, InsulaVM nem credenciais.

```go
pairs, err := gitmasteractions.ConvertYAML(yamlBytes, gitmasteractions.ConvertOptions{
  Format: gitmasteractions.FormatGitMasterV1,
  Branch: "develop",
  EventType: "manual",
})
// []CommandPair{ {Job: "job_develop", Command: "..."} }
```

`CommandPair` contém exclusivamente `Job string` e `Command string`, com propriedades JSON `job` e `command`. O formato deve ser indicado; `ConvertOptions.EventType` permite pré-validar gatilhos `commit`/`push` usando apenas a branch, sem uma origem Git completa. Em `action-v1`, não há filtros nem gatilhos, e a ordem original dos Jobs e Commands é mantida.

O planejamento respeita `needs` e filtros, mas **não prevê o resultado de comandos que ainda não foram executados**. Para Jobs sem dependência, a ordem é determinística conforme o parser do executor; trabalhos paralelos poderão executar em outra ordem real. [CONTR-005](../../contratos/CONTR-005-conversor-yaml.md).

## Grafo imutável de dependências Job → Job

```go
graph, err := gitmasteractions.NewJobGraph(yamlData, gitmasteractions.ConvertOptions{
    Format: gitmasteractions.FormatGitMasterV1, Branch: "develop",
})
if err != nil { return err }
needs, err := graph.Dependencies("build")       // dependências diretas
dependents, err := graph.Dependents("lint")      // Jobs que dependem de lint
independent, err := graph.HasDependencies("lint") // false se não houver needs
transitive, err := graph.DependsOn("release", "lint")
decision, err := graph.Decision("build", map[string]string{"lint": "succeeded"})
ready, err := graph.ReadyJobs(map[string]string{"lint": "succeeded"})
all, err := graph.Evaluate(map[string]string{"lint": "succeeded"})
```

`JobGraph.Jobs()` retorna a estrutura em ordem topológica determinística, com cópia das listas; nenhuma consulta altera o grafo. `Decision` considera status persistidos (`pending`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`; ausente = `pending`), filtros de branch e `needs`; `CanRun` só é verdadeiro quando o Job está pendente, permitido pela branch, e todas as dependências estão `succeeded`. Outros estados são explicitamente `waiting`, `blocked`, `skipped` ou o próprio status corrente. A execução real usa o mesmo motor para decidir a prontidão.

Referência: [CONTR-006](../../contratos/CONTR-006-grafo-dependencias-jobs.md).
