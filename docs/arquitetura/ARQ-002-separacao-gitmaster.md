<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-002 — Integração entre GitMaster e GitMaster Actions

**Estado:** adaptador Go implementado na branch de migração do GitMaster; build e VM real pendentes.

## Fronteira arquitetural

O GitMaster não usa diretamente `insulavm.New`, `StartVMWithOptions`, `DeployToVM`, `ExecVM`, `StopVM`, SSH ou comandos QEMU/Libvirt. Isso pertence ao runtime em GitMaster Actions, cujo único contato de baixo nível é a biblioteca InsulaVM.

A entrada de material é `legacy.ArchiveProvider`, callback injetado pelo consumidor que produz TAR.GZ de um SHA imutável. A biblioteca valida o arquivo e o envia à VM; o consumidor nunca escreve comandos de guest fora do executor.

A descoberta/validação dos workflows na árvore Git, filtros de branch, commit/push e autorização permanecem em GitMaster. O pacote `legacy` executa o grafo `needs` e os steps e persiste todos os estados/outputs.

## Dois modelos de persistência

- `Engine` com `New(Options)` administra seu próprio `actions.db` com entidades Action/Job/Command/Logs por UUIDv7.
- `legacy.NewStore(db, logs)` reutiliza o arquivo SQLite existente do GitMaster, **mas todas as instruções SQL de action_runs, jobs, comandos e runtime pertencem ao GitMaster Actions**.
- O GitMaster mantém o esquema Git `clones`, `action_events`, `publications` e o log de operações MCP via interface `ExecutionLogAdapter`. A saída de uma action não deve ser apagada quando a aplicação encerrar.

É necessária uma migração futura com dual-read e validação de contagens/IDs antes de trocar o armazenamento histórico para banco separado. **Não remover as tabelas antigas sem plano de migração.**

## Contrato de execução

```text
GitMaster: commit -> SHA -> YAML da árvore Git -> plano + evento -> fila
GitMaster Actions: fila -> Profile -> StartVM -> Deploy TAR.GZ -> Jobs/needs/steps
GitMaster Actions: logs persistidos -> StopVM/cleanup -> status final
GitMaster: validação do mesmo SHA/contexto -> autoriza public -> push
```

A biblioteca não levanta daemon próprio. O worker roda no contexto do serviço GitMaster, com operações fornecidas pelo pacote `legacy`. O `public` é bloqueado em falha, cancelamento, incompletude ou cleanup não confirmado.

**Limitações:** compilação e testes de KVM ainda não executados; console textual do InsulaVM não é API de streams binários; sem mecanismo de rede por perfil ou clones não destrutivos.
