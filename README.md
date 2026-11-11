# GitMaster Actions

Biblioteca Go responsável pela **execução, persistência e consulta de Actions**, gerenciamento de perfis e integração exclusiva com [InsulaVM](https://github.com/ahhvahh/InsulaVM).

**Estado:** implementação inicial publicada em `main`. Go 1.27 e testes KVM reais **ainda não foram executados** neste ambiente; não classificar como validado em produção.

## Responsabilidades

| Componente | Responsável |
| --- | --- |
| GitMaster | Repositório/branch, clone, commit SHA, descoberta de `.gitmaster/workflow`, eventos Git e autorização do push |
| GitMaster Actions | VM por action, workers, dependências entre jobs, execução de comandos, streams, SQLite de actions, consultas e perfis |
| InsulaVM | QEMU/Libvirt, QCOW2, Cloud-Init, guest, SSH, transferência e teardown |

A integração com o GitMaster preserva o esquema histórico de `action_runs` por meio do pacote `legacy`. **O código que grava/consulta esse histórico reside aqui**; o GitMaster continua dono das tabelas Git (`clones`, `action_events`, `publications`) e fornece o TAR.GZ de um SHA imutável por callback. Não existe outro daemon da biblioteca.

## API Go

- `New(Options)`, `RunFile`, `RunYAML`, `CancelAction` e `Close`
- `GetAction`, `ListActions`, `GetJob`, `GetCommand`
- `Action.Jobs(ctx)`, `Job.Commands(ctx)`, `Command.Logs(ctx,skipLines...)`, `Command.Outputs(ctx,filter,page)`
- `NewRuntime`: `Profile`, `Start`, `Deploy`, `Command`, `Stop`, `Workspace`
- `ListVMImages`, `ListVMProfiles`, `RegisterVMProfile`, `ConfigurationFingerprint`
- Pacote `legacy`: `NewStore`, `EnsureActionSchema`, `ExecuteAction`, `RunActionViaInsula`, `StartInsulaWorker` e `ReconcileInsulaVMs`.

O esquema do manifesto independente (`schema_version: 1`, `action.jobs[].commands[]`) é diferente do esquema GitMaster (`triggers`, `needs`, `steps`). O adaptador `legacy` conserva as regras de branch e dependências sem converter YAMLs silenciosamente.

## CLI implementada

```sh
cd gitmaster-actions
go run ./cmd/gitmaster-actions run --file ./action.yaml --db ./actions.db --insula-config /etc/insulavm/config.yaml
go run ./cmd/gitmaster-actions actions list --db ./actions.db
go run ./cmd/gitmaster-actions jobs list --action-id UUID --db ./actions.db
go run ./cmd/gitmaster-actions commands list --job-id UUID --db ./actions.db
go run ./cmd/gitmaster-actions logs list --command-id UUID --skip-lines 0 --db ./actions.db
go run ./cmd/gitmaster-actions profiles list --insula-config /etc/insulavm/config.yaml
go run ./cmd/gitmaster-actions profiles register --insula-config /etc/insulavm/config.yaml --images-dir /srv/images --name ci-go --image base.qcow2 --vcpus 2 --memory-mib 2048 --disk-gib 12
```

A CLI usa exclusivamente a biblioteca Go. Só funciona com InsulaVM instalado e perfil/guest configurados para comandos remotos; consultas ao SQLite não exigem VM.

## Limitações comprovadas pelo código

- Ainda não há teste Go nem integração KVM executados neste ambiente.
- Streams do InsulaVM são um console textual prefixado: **não prometer fidelidade binária ou stdout/stderr nativos**.
- `RunYAML`/ `RunFile` rejeitam sobrescritas de RAM/vCPU diferentes de zero até o suporte completo.
- Criação de perfis com internet configurável e cópia não destrutiva de VM existente seguem propostas, não estão habilitadas.
- O formato histórico `legacy` ainda compartilha o arquivo SQLite do GitMaster por compatibilidade e precisa de migração operacional planejada para DB dedicado.
- Não há recuperação automática de VMs após reinício do processo autônomo.

**Documentação:** [índice](docs/README.md), [separação entre repositórios](docs/arquitetura/ARQ-002-separacao-gitmaster.md), [API](docs/interfaces/go/INTF-GO-001-biblioteca-publica.md) e [contrato YAML](docs/contratos/CONTR-001-configuracao-action.md).
