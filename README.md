# GitMaster Actions

Biblioteca Go que recebe YAML de Action, registra Action → Jobs → Commands no **SQLite próprio**, executa os comandos dentro de uma VM descartável pelo [InsulaVM](https://github.com/ahhvahh/InsulaVM), armazena saídas e acompanha cleanup e estados. O **GitMaster** permanece responsável por GitHub, clones, commits, SHA, snapshots e pushes.

**Toolchain:** Go 1.27 (`go.mod`), CI Go 1.27.4. **InsulaVM fixado:** `983c4a8b46c7ee1a2f7ebd3d45de41aa3589b397`. Testes de unidade/race/vet com mocks não substituem validação autorizada em KVM/Libvirt.

## Componentes

- `Engine`: `New`, `Close`, `SubmitYAML`, `WaitAction`, `CancelAction`, consultas e profiles. Um único Engine por `actions.db`; um lock advisory Linux em `<DatabasePath>.lock` impede dupla posse.
- `action-v1` (arquivo local/CLI) e `gitmaster-workflow-v1` (GitMaster) são formatos distintos e selecionados explicitamente.
- `SubmitYAML` cria Action, Jobs e Commands **antes** de iniciar worker e VM. Jobs executam por dependência topológica; commands são sequenciais na mesma VM.
- `RunFile` e `RunYAML` são wrappers **síncronos** do mesmo executor. Logs, VM ID, erros e cleanup usam o mesmo esquema de persistência.
- Reenvio da mesma origem/evento só é idempotente com hashes idênticos de YAML e TAR.GZ; conteúdo divergente retorna conflito.
- `ImportLegacyHistory` importa o SQLite anterior em modo leitura, preserva dados tipados em `legacy_archive` **e normaliza** o grafo e suas saídas no banco independente. `GetLegacyActionID` resolve IDs antigos para a nova API.
- `legacy.NewStore` permanece apenas como adaptador de compatibilidade para consumidores ainda não migrados; não deve ser usado em novas integrações.

## Exemplo de submissão

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath:     "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
    Workers:          2,
})
if err != nil { return err }
defer engine.Close()

action, err := engine.SubmitYAML(ctx, gitmasteractions.SubmitRequest{
    YAML: workflowFromCommit,
    Format: gitmasteractions.FormatGitMasterV1,
    Origin: gitmasteractions.ActionOrigin{
        CloneID: cloneID, CommitSHA: sha, Branch: branch,
        EventID: eventID, EventType: "commit",
        WorkflowName: "go-ci", WorkflowPath: ".gitmaster/workflow/go-ci.yaml",
    },
    ArchivePath: "/srv/snapshots/commit.tar.gz",
})
if err != nil { return err }
completed, err := engine.WaitAction(ctx, action.ID)
if err != nil { return err }
if completed.Status != "succeeded" { return fmt.Errorf("action %s", completed.Status) }
```

O consumidor deve ler o YAML da **árvore do SHA imutável** e fornecer TAR.GZ autorizado. A biblioteca valida o arquivo, cria cópia privada e registra seu SHA256; isso não substitui a política de exclusão de segredos do consumidor.

## Testes

```bash
go mod download
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
```

## Limites e documentação

`ExecVM` do InsulaVM devolve console textual decorado, não streams binários nativos. Após uma queda do host, o executor marca Actions interrompidas e oferece `ReconcileInterrupted`, mas **não retoma comandos**. A integração antiga do GitMaster precisa ser atualizada em outro repositório para deixar de usar o pacote `legacy`.

[Índice](docs/README.md) · [Arquitetura](docs/arquitetura/ARQ-001-executor-actions.md) · [YAML GitMaster](docs/contratos/CONTR-003-gitmaster-workflow.md) · [API Go](docs/interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Persistência](docs/persistencia/PERS-001-registro-execucoes.md) · [Migração](docs/guias/GUIA-008-integracao-gitmaster.md)