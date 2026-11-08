<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-GO-001 — API pública da biblioteca GitMaster Actions

**Estado:** proposta, sem implementação. Fonte canônica das assinaturas lógicas públicas.

## Inicialização e propriedade

`New(Options) (*Engine, error)` inicializa o repositório SQLite e configura o uso futuro do adaptador InsulaVM; `Options` contém `DatabasePath` e `InsulaConfigPath` (configuração administrativa confiável). O consumidor não abre nem grava tabelas diretamente. `Engine.Close() error` libera recursos. As consultas podem operar somente com `DatabasePath` e não devem exigir QEMU, SSH nem inicializar uma VM. Para `RunYAML` e `RunFile`, `InsulaConfigPath` é necessário. Não há daemon, HTTP nem processo residente independente do consumidor. Existe uma CLI **opcional** para testes locais, documentada em [INTF-TERM-001](../terminal/INTF-TERM-001-cli-testes.md), que consome exclusivamente estes métodos.

## Execução

| Método lógico | Entrada | Retorno | Efeito |
| --- | --- | --- | --- |
| `RunYAML(ctx, yaml []byte)` | YAML em bytes | `ActionResult, error` | Valida, registra UUID, executa e persiste |
| `RunFile(ctx, path string)` | Caminho de YAML local | `ActionResult, error` | Lê arquivo e delega à mesma rotina de `RunYAML` |
| `CancelAction(ctx, actionID string)` | UUID da Action | `error` | Solicita cancelamento de Action em execução nesta instância |

`RunYAML` e `RunFile` compartilham parser, validação, geração de IDs e persistência. São operações bloqueantes; o consumidor pode executá-las em goroutines. `ActionResult` devolve `ActionID`, `VMID` (se criado), `Status`, `StartedAt` e `FinishedAt` (se disponível). Erros podem acompanhar resultado parcial. O cancelamento por `context.Context` é obrigatório; `CancelAction` é uma conveniência para execuções ativas no mesmo Engine, não uma fila persistente.

## Consultas somente leitura

| Método lógico | Entrada | Retorno |
| --- | --- | --- |
| `GetAction(ctx, actionID)` | UUID | `Action, error` |
| `ListActions(ctx, filter, page)` | Filtros de estado/período e paginação | `[]Action, error` |
| `ListJobs(ctx, actionID)` | UUID Action | `[]Job, error` ordenado por posição |
| `GetJob(ctx, jobID)` | UUID Job | `Job, error` |
| `ListCommands(ctx, jobID)` | UUID Job | `[]Command, error` ordenado por posição |
| `GetCommand(ctx, commandID)` | UUID Comando | `Command, error` |
| `ListCommandOutputs(ctx, commandID, filter, page)` | UUID, tipo de stream e paginação | `[]CommandOutput, error` ordenado por sequência |

As consultas não executam comandos e não alteram estados. `CommandOutput.Payload` é `[]byte`, sem conversão de texto. O campo `StreamType` admite `stdin`, `stdout` e `stderr`; `stdin` representa entrada enviada ao guest, não resposta recebida. Paginação deve ser limitada para evitar carregar logs grandes em memória. Não há método público para inserir/alterar registros arbitrariamente.

## Exemplos conceituais (pseudocódigo, não compilável)

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath: "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
})
if err != nil { return err }
defer engine.Close()

result, err := engine.RunFile(ctx, "/srv/actions/build.yaml")
// Alternativa: engine.RunYAML(ctx, []byte(yamlContent))
if err != nil { return err }
jobs, err := engine.ListJobs(ctx, result.ActionID)
```

## Regras

- A biblioteca gera e persiste UUIDv7 para Action, Job e Comando; o UUID da VM é fornecido pelo InsulaVM.
- A CLI chama os mesmos métodos públicos; não usa acesso direto ao SQLite e não possui parser, gerador de IDs nem orquestrador paralelos.
- `RunFile` resolve caminhos de importação relativos à pasta do manifesto; `RunYAML` não possui diretório-base e exige caminhos de importação absolutos.
- Validar YAML e origem da VM antes da criação do registro. Após aceitação, gravar Action e Jobs antes de iniciar VM; gravar cada comando antes da execução.
- Cada chamada de consulta deve aceitar `context.Context` e propagar erros tipados de ID inválido, não encontrado e banco indisponível.
- Não expor `*sql.DB` nem criar métodos redundantes de persistência. O acesso SQLite é interno à biblioteca.
- Logs em tempo real dependem da futura API InsulaVM com streams independentes; não apresentar como funcionalidade já disponível.

## Referências

- [CONTR-001 — YAML](../../contratos/CONTR-001-configuracao-action.md)
- [PERS-001 — SQLite](../../persistencia/PERS-001-registro-execucoes.md)
- [FLUX-001 — Execução](../../fluxos/FLUX-001-execucao-action.md)
- [INTF-TERM-001 — CLI local](../terminal/INTF-TERM-001-cli-testes.md)