# GitMaster Actions

Biblioteca Go para registrar, executar e consultar Actions em VMs isoladas do [InsulaVM](https://github.com/ahhvahh/InsulaVM), usando um **SQLite próprio**. O GitMaster fornece o YAML, o commit SHA e o TAR.GZ do commit; esta biblioteca não executa operações Git.

**Integração InsulaVM verificada no código-fonte:** `main` em `983c4a8b46c7ee1a2f7ebd3d45de41aa3589b397`. O `go.mod` atual fixa esse código via pseudoversão. **Status de validação:** a implementação requer Go 1.27 declarado no módulo; testes com KVM/Libvirt exigem ambiente autorizado. Não confundir inspeção estática com teste de integração.

## Componentes

- `Engine`: `New`, `SubmitYAML`, `WaitAction`, `CancelAction`, `Close`, consultas e perfis.
- Adaptadores explícitos `action-v1` (legado local) e `gitmaster-workflow-v1` (YAML de `.gitmaster/workflow/`). Formatos inválidos ou misturados são rejeitados.
- SQLite `actions.db` independente: Action, Jobs, Commands, saídas, eventos, SHA do material, VM e cleanup.
- Workers limitados por `Options.Workers` (padrão 2). Toda Action é cadastrada integralmente antes de iniciar uma VM.
- Pacote `legacy`: **compatibilidade temporária** com registros existentes do GitMaster, ainda dependente de armazenamento externo. Não usar `legacy.NewStore` para novas Actions. O importador `ImportLegacyHistory` copia registros para um arquivo de histórico próprio, sem alterar o banco de origem.

## Exemplo Go: submissão assíncrona

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath:     "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
    ImagesDir:        "/var/lib/insulavm/images",
    Workers:          2,
})
if err != nil { return err }
defer engine.Close()

content, err := os.ReadFile(".gitmaster/workflow/go-ci.yaml")
if err != nil { return err }
action, err := engine.SubmitYAML(ctx, gitmasteractions.SubmitRequest{
    YAML: content,
    Format: gitmasteractions.FormatGitMasterV1,
    Origin: gitmasteractions.ActionOrigin{
        CloneID: cloneID, CommitSHA: commitSHA, EventID: eventID,
        EventType: "commit", Branch: "develop", WorkflowName: "go-ci",
        WorkflowPath: ".gitmaster/workflow/go-ci.yaml",
    },
    ArchivePath: "/var/lib/gitmaster/snapshots/commit.tar.gz",
})
if err != nil { return err }
finished, err := engine.WaitAction(ctx, action.ID)
if err != nil { return err }
if finished.Status != "succeeded" { return fmt.Errorf("action: %s", finished.Status) }
```

O consumidor seleciona o arquivo na árvore Git do SHA. Não forneça YAML vindo de uma working tree mutável se a Action validar um commit. O TAR.GZ precisa ser autorizado pelo consumidor; a biblioteca valida estrutura, tamanho e caminhos e cria uma cópia privada antes de enfileirar. O SHA256 dessa cópia permanece no histórico.

## CLI

A CLI existente continua oferecendo `run --file`, `actions list`, `jobs list`, `commands list`, `logs list` e operações de perfis. `RunFile`/`RunYAML` usam o manifesto local `action-v1` e são **síncronos**; `SubmitYAML` é a API assíncrona recomendada ao GitMaster.

## Documentação

[Índice](docs/README.md) · [Contrato YAML GitMaster](docs/contratos/CONTR-003-gitmaster-workflow.md) · [API Go](docs/interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Persistência](docs/persistencia/PERS-001-registro-execucoes.md) · [Integração](docs/guias/GUIA-008-integracao-gitmaster.md)

**Limites:** o console `ExecVM` do InsulaVM é texto decorado, não streams binários nativos. A recuperação após interrupção identifica registros pendentes e disponibiliza cleanup das VMs conhecidas, sem retomar Jobs a partir do meio. A conversão do histórico legado arquivado para as entidades novas não é automática. Não há testes KVM locais nesta entrega.