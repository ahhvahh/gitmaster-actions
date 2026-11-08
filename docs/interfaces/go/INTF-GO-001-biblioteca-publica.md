<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-GO-001 — API pública da biblioteca GitMaster Actions

**Estado:** proposta, sem implementação. Fonte canônica das assinaturas lógicas públicas.

## Inicialização e propriedade

`New(Options) (*Engine, error)` inicializa o repositório SQLite e configura o uso futuro do adaptador InsulaVM; `Options` contém `DatabasePath` e `InsulaConfigPath` (configuração administrativa confiável). O consumidor não abre nem grava tabelas diretamente. `Engine.Close() error` libera recursos. As consultas podem operar somente com `DatabasePath` e não devem exigir QEMU, SSH nem inicializar uma VM. Para `RunYAML` e `RunFile`, `InsulaConfigPath` é necessário. Não há daemon, HTTP nem processo residente independente do consumidor. Existe uma CLI **opcional** para testes locais, documentada em [INTF-TERM-001](../terminal/INTF-TERM-001-cli-testes.md), que consome exclusivamente estes métodos.

## Execução e consultas por ID

| Método proposto no Engine | Retorno | Responsabilidade |
| --- | --- | --- |
| `RunYAML(ctx, yaml []byte)` | `*Action, error` | Validar, gerar UUIDv7, executar e persistir |
| `RunFile(ctx, path string)` | `*Action, error` | Ler YAML local e delegar ao mesmo fluxo |
| `GetAction(ctx, actionID string)` | `*Action, error` | Localizar Action registrada |
| `ListActions(ctx, filter, page)` | `[]*Action, error` | Consultar Actions filtradas |
| `GetJob(ctx, jobID string)` | `*Job, error` | Localizar Job por ID para consulta pontual |
| `GetCommand(ctx, commandID string)` | `*Command, error` | Localizar Command por ID para consulta pontual |
| `CancelAction(ctx, actionID string)` | `error` | Cancelar execução ativa no mesmo Engine |

`RunFile` e `RunYAML` são bloqueantes; a aplicação pode usar goroutines. Se uma execução falhar **após criar o registro**, o retorno pode conter `*Action` não nulo **e** `error`, para preservar o UUID; erro antes da aceitação retorna Action nula. `Action` contém `ID`, `Status`, `StartedAt` e `FinishedAt` (se encerrada). `VMID` é um campo transitório somente no objeto retornado pela execução atual; `GetAction` recupera o histórico sem esse identificador, pois ele não está no SQLite.

## Navegação por objetos

| Objeto receptor | Método | Retorno |
| --- | --- | --- |
| `*Action` | `Jobs(ctx)` | `[]*Job, error`, na ordem da Action |
| `*Job` | `Commands(ctx)` | `[]*Command, error`, com status e exit code |
| `*Command` | `Logs(ctx, skipLines ...uint64)` | `[]CommandLog, error`, linhas incrementais |
| `*Command` | `Outputs(ctx, filter, page)` | `[]CommandOutput, error`, chunks brutos |

O objeto `Command` é o **receptor** de `Logs` em Go, portanto já identifica o comando. Não criar `Engine.ListJobs`, `Engine.ListCommands` ou `Engine.ListCommandOutputs`: os objetos fazem a navegação, sem redundância. `GetJob` e `GetCommand` permitem localizar uma entidade isolada por UUID, sobretudo pela CLI.

## Leitura incremental de logs

Go não suporta sobrecarga de métodos por assinatura. Para admitir as duas chamadas solicitadas, o contrato usa **um argumento variádico opcional**:

```go
logs, err := command.Logs(ctx)        // Primeiras linhas (cursor 0)
logs, err = command.Logs(ctx, 150)    // Somente linhas após a nº 150
```

- Sem argumento: `skipLines = 0`. Com `N`: somente linhas cujo `LineNumber > N`. Rejeitar mais de um valor.
- `LineNumber` começa em 1 e é **monotônico, persistido e independente de paginação** para cada comando, compartilhado entre canais. Não renumerar a cada leitura.
- Retornar **até 500 linhas por chamada** em ordem crescente. Para continuar, usar `logs[len(logs)-1].LineNumber` como cursor, somente se `len(logs) > 0`; lista vazia indica ausência de novos registros naquele momento.
- `CommandLog` contém `LineNumber uint64`, `StreamType string`, `Payload []byte` e `CapturedAt time.Time`. `Payload` conserva bytes e terminadores de linha, sem supor UTF-8.
- Para cada canal, combinar chunks até `\n` (incluindo o terminador); ao terminar/cancelar o comando, registrar o fragmento final sem quebra de linha. Um fragmento ainda em andamento pode não aparecer nas consultas até receber `\n` ou ocorrer encerramento.
- `StreamType` admite `stdin`, `stdout`, `stderr`; captura de stdin é opcional. Intercalação entre canais reflete captura no host, sem garantia de ordenação causal no guest.
- `Command.Outputs` é a operação distinta para recuperar chunks binários originais, incluindo dados sem estrutura de linha. A persistência está em [PERS-001](../../persistencia/PERS-001-registro-execucoes.md).
- Consultas são somente leitura, com `context.Context` e erros tipados. Objetos devem tratar Engine fechado e registro inexistente, sem expor `*sql.DB`.

## Exemplo conceitual (pseudocódigo, não compilável)

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath: "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
})
if err != nil { return err }
defer engine.Close()

action, runErr := engine.RunFile(ctx, "/srv/actions/build.yaml")
if action == nil { return runErr }
jobs, err := action.Jobs(ctx)
if err != nil { return err }
for _, job := range jobs {
    commands, err := job.Commands(ctx)
    if err != nil { return err }
    for _, command := range commands {
        var lastLine uint64
        for {
            logs, err := command.Logs(ctx, lastLine)
            if err != nil { return err }
            if len(logs) == 0 { break }
            // Processar logs.
            lastLine = logs[len(logs)-1].LineNumber
        }
    }
}
```

## Regras

- A biblioteca gera e persiste UUIDv7 para Action, Job e Comando; o UUID da VM é fornecido pelo InsulaVM.
- A CLI chama os mesmos métodos públicos; não usa acesso direto ao SQLite e não possui parser, gerador de IDs nem orquestrador paralelos.
- `RunFile` resolve caminhos de importação relativos à pasta do manifesto; `RunYAML` não possui diretório-base e exige caminhos de importação absolutos.
- Validar YAML, perfil local preparado e `action.jobs` antes da criação do registro. Após aceitação, gravar Action e Jobs antes de iniciar VM; gravar cada comando antes da execução.
- Cada chamada de consulta deve aceitar `context.Context` e propagar erros tipados de ID inválido, não encontrado e banco indisponível.
- Não expor `*sql.DB` nem criar métodos redundantes de persistência. O acesso SQLite é interno à biblioteca.
- Linhas indexadas são uma projeção persistida dos streams brutos para paginação eficiente por número. A biblioteca preserva ambos.
- A Action contém Jobs diretamente, sem pipelines; somente `vm.profile` é aceito. Memória e CPUs são opcionais e usam os valores do perfil por padrão.
- O banco da Action não guarda repositório, origem/hash do YAML nem dados da VM. Não há retomada após queda/reinício do servidor.
- Logs em tempo real dependem da futura API InsulaVM com streams independentes; não apresentar como funcionalidade já disponível.

## Referências

- [CONTR-001 — YAML](../../contratos/CONTR-001-configuracao-action.md)
- [PERS-001 — SQLite](../../persistencia/PERS-001-registro-execucoes.md)
- [FLUX-001 — Execução](../../fluxos/FLUX-001-execucao-action.md)
- [INTF-TERM-001 — CLI local](../terminal/INTF-TERM-001-cli-testes.md)