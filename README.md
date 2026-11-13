# GitMaster Actions

Biblioteca Go que recebe YAML de Action, registra Action → Jobs → Commands no **SQLite próprio**, executa os comandos dentro de uma VM descartável pelo [InsulaVM](https://github.com/ahhvahh/InsulaVM), armazena saídas e acompanha cleanup e estados. O **GitMaster** permanece responsável por GitHub, clones, commits, SHA, snapshots e pushes.

**Toolchain:** Go 1.27 (`go.mod`), CI Go 1.27.4. **InsulaVM/main fixado:** `1ced9ce8b5b8cdd4b52c903e7949dfd8c1fb3abf`. Testes de unidade/race/vet com mocks não substituem validação autorizada em KVM/Libvirt.

## Componentes

- `Engine`: `New`, `Close`, `SubmitYAML`, `WaitAction`, `CancelAction`, consultas e profiles. Um único Engine por `actions.db`; um lock advisory Linux em `<DatabasePath>.lock` impede dupla posse.
- `action-v1` (arquivo local/CLI) e `gitmaster-workflow-v1` (GitMaster) são formatos distintos e selecionados explicitamente.
- `SubmitYAML` cria Action, Jobs e Commands **antes** de iniciar worker e VM. Jobs executam por dependência topológica; commands são sequenciais na mesma VM.
- `RunFile` e `RunYAML` são wrappers **síncronos** do mesmo executor. Logs, VM ID, erros e cleanup usam o mesmo esquema de persistência.
- Reenvio da mesma origem/evento só é idempotente com hashes idênticos de YAML e snapshot `.zip` ou `.tar.gz`; conteúdo divergente retorna conflito.
- `ImportLegacyHistory` importa o SQLite anterior em modo leitura, preserva dados tipados em `legacy_archive` **e normaliza** o grafo e suas saídas no banco independente. `GetLegacyActionID` resolve IDs antigos para a nova API.
- `legacy.NewStore` permanece apenas como adaptador de compatibilidade para consumidores ainda não migrados; não deve ser usado em novas integrações.

## Exemplo de submissão

```go
engine, err := gitmasteractions.New(gitmasteractions.Options{
    DatabasePath:     "/var/lib/gitmaster-actions/actions.db",
    InsulaConfigPath: "/etc/insulavm/config.yaml",
    ProfilesConfigPath: "/etc/gitmaster-actions/profiles.yaml",
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
    ArchivePath: "/srv/snapshots/commit.zip",
})
if err != nil { return err }
completed, err := engine.WaitAction(ctx, action.ID)
if err != nil { return err }
if completed.Status != "succeeded" { return fmt.Errorf("action %s", completed.Status) }
```

O consumidor deve ler o YAML da **árvore do SHA imutável** e fornecer ZIP (ou TAR.GZ legado) autorizado. A biblioteca valida o arquivo, cria cópia privada e registra seu SHA256; isso não substitui a política de exclusão de segredos do consumidor.

## Perfis QCOW2 e entrada por CLI

O catálogo de perfis do **servidor** é `examples/insula/frozen-profiles.yaml` (um ou vários perfis). Cada entrada contém `name`, `image_path` absoluto para um QCOW2 congelado, `memory_mib`, `vcpus` e `workspace`, por exemplo `/tmp/workspace`. O workflow seleciona o perfil por `vm_profile`. O InsulaVM importa a imagem como perfil gerenciado na primeira utilização; cada Action inicia uma VM descartável. A imagem fonte permanece intacta. Perfis já registrados com configurações divergentes não são substituídos automaticamente.

```bash
gitmaster-actions run \
  --db /var/lib/gitmaster-actions/actions.db \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml \
  --format gitmaster-workflow-v1 \
  --branch develop \
  --file .gitmaster/workflow/go-ci.yaml \
  --archive /srv/snapshots/repository.zip
```

O InsulaVM recebe o ZIP, o convidado executa `unzip` dentro do workspace configurado, e o executor persiste todos os Jobs, Commands, códigos de saída e logs no SQLite. **A imagem congelada precisa ter `unzip` instalado.** Veja [Contrato de perfis](docs/contratos/CONTR-004-perfis-congelados.md) e [Guia ZIP](docs/guias/GUIA-011-execucao-zip-vm.md).

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
## Layout e validação automatizada

A implementação, testes e CLIs estão em [`src/`](src/); as fachadas [`api.go`](api.go) e [`legacy/api.go`](legacy/api.go) preservam as importações antigas dos consumidores (`github.com/ahhvahh/gitmaster-actions` e `/legacy`). Contratos e manuais permanecem em [`docs/`](docs/).

```bash
bash scripts/build.sh --output /tmp/gitmaster-actions
bash scripts/test-unit.sh
bash scripts/install-hooks.sh             # opcional: testa antes do commit local
bash scripts/test-integration.sh --insula-config /etc/insulavm/config.yaml
```

O CI executa os testes unitários em cada **push/PR**; o teste integral (ISO Debian 13, perfil 500 MiB/1 vCPU, três comandos, validação de logs e cleanup) requer host KVM autorizado e é explicitamente manual. Veja [GUIA-010](docs/guias/GUIA-010-build-testes.md).
## Smoke test de dependências, skip e concorrência

Use o [YAML com 10 Jobs](examples/workflow/dependency-parallel-smoke.yaml), o [catálogo com perfil ci-smoke](examples/insula/frozen-profiles.yaml) e o [script CLI com verificação de resultados](scripts/tests/workflow-parallel-smoke.sh). Com `--branch develop`, valida Jobs executados e omitidos; com `--branch teste-falha`, produz erro intencional exit 17 e deve pular o Job dependente. Instruções e saída esperada: [GUIA-011](docs/guias/GUIA-011-execucao-zip-vm.md). Não é necessário integrar GitMaster para executar esse ensaio de CLI.

### Teste isolado: um Job executa e outro não, conforme a branch

Use o [workflow mínimo com dois Jobs](examples/workflow/branch-filter-smoke.yaml): em `develop`, somente `job_develop` executa, e `job_main` é persistido como `skipped` (`branch_filter`); em `main`, ocorre o inverso. Os testes Go ([`TestBranchFilterSmokeExecutesOnlyMatchingJob`](src/branch_filter_smoke_test.go)) verificam os dois cenários com runtime simulado, incluindo comandos, código de saída, logs e ausência de chamadas para o Job filtrado. Para ensaio com VM real, rode `bash scripts/tests/workflow-branch-filter-smoke.sh /etc/insulavm/config.yaml /etc/gitmaster-actions/profiles.yaml`: o script prepara um ZIP mínimo, testa as duas branches pela CLI e verifica o JSON persistido.
