# Testes locais — GitMaster Actions

Esta pasta centraliza compilação, execução e arquivos de trabalho. O modo padrão não inicia VMs.

## Estrutura

```text
tests/
├── build.sh                      # gera o binário com o nome do projeto
├── run.sh                        # runner de testes
├── fixtures/README.txt           # material de origem do ZIP mínimo
├── verifiers/
│   ├── branch/verify.go          # 2 Jobs: um executa, outro é ignorado
│   └── parallel/verify.go        # dependências, falha, skips e paralelismo
├── bin/gitmaster-actions         # criado pelo build (ignorado pelo Git)
└── work/<execucao>/              # arquivos gerados (ignorados pelo Git)
    ├── build.log
    ├── unit.log                   # quando modo unit/all
    ├── snapshot.zip               # quando modo vm/all
    ├── branch/<branch>/
    │   ├── actions.db
    │   ├── action.json
    │   ├── stderr.log
    │   └── verify.log
    └── parallel/<branch>/
        └── ...
```

## Compilar

```bash
bash tests/build.sh
./tests/bin/gitmaster-actions --help
```

Reaproveita o build oficial (`scripts/build.sh`) com `CGO_ENABLED=0`, gerando sempre `tests/bin/gitmaster-actions`.

## Testes automáticos (sem VM)

```bash
bash tests/run.sh
# ou
bash tests/run.sh --mode unit
```

Executa `go mod tidy -diff`, `go test -race -count=1 ./...` e `go vet ./...` além do build. Verifica a lógica da biblioteca com runtimes simulados.

## Testes integrais com VM descartável

Pré-requisitos: Linux com KVM/Libvirt, Go, zip e perfil `ci-smoke` configurado no catálogo de imagens QCOW2. A imagem precisa ter SSH/Cloud-Init, `unzip` e `/tmp/workspace` acessível.

```bash
# Filtro de branch: develop executa job_develop e ignora job_main; main faz o inverso
bash tests/run.sh --mode vm --case branch \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Dependências e paralelismo, incluindo falha intencional na branch teste-falha
bash tests/run.sh --mode vm --case parallel \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Todos os testes: Go e CLI em VM
bash tests/run.sh --mode all \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml
```

Os YAMLs canônicos continuam em `examples/workflow/`. O runner usa esses mesmos arquivos, sem manter cópias divergentes. A CLI grava Action, Jobs e Commands no SQLite e retorna JSON com `--details`. Os verificadores exigem os status, códigos de saída e `skip_reason` previstos.

Na branch `teste-falha`, a Action deve terminar como `failed` (comando `exit 17`), e seu dependente como `skipped`. Esse resultado é **sucesso do teste** quando o verificador confirma a falha prevista.

## Diagnóstico

Cada execução mantém seus artefatos em `tests/work/<data>-<pid>/`, sem sobrescrever resultados anteriores. Abra `unit.log`, `build.log`, `action.json`, `verify.log` e `stderr.log` para investigar falhas. Nenhum deles é enviado ao Git graças a `tests/.gitignore`.

Para limpar apenas os artefatos gerados:

```bash
rm -rf tests/bin tests/work
```

Scripts antigos em `scripts/tests/` permanecem compatíveis. A execução com VM real não é realizada pelos runners de CI sem KVM.

## Motor de conversão YAML e matriz de variações

O conversor devolve uma lista plana `[ {"job": "...", "command": "..."} ]` sem abrir o SQLite, importar ZIP ou iniciar VM. Os parsers suportados são `gitmaster-workflow-v1` e `action-v1`; nomes de Jobs, `needs`, filtros `include/exclude`, gatilhos `commit/push`, sequência dos passos, arrays do legado, YAML em linha, aliases e comandos multilinha são cobertos.

```bash
bash tests/run.sh --mode plan

# Ou inspecione qualquer arquivo de forma independente
bash tests/build.sh
./tests/bin/gitmaster-actions plan --format gitmaster-workflow-v1 \
  --branch develop --file examples/workflow/branch-filter-smoke.yaml

# O arquivo também pode vir por stdin
cat tests/plan-cases/needs-dag.yaml | ./tests/bin/gitmaster-actions plan --stdin

# Formato action-v1 (lista ordenada de jobs e strings de comandos)
./tests/bin/gitmaster-actions plan --format action-v1 --file tests/plan-cases/legacy.yaml

# Validar gatilhos de um evento: --event commit|push
./tests/bin/gitmaster-actions plan --branch develop --event commit --file tests/plan-cases/triggers.yaml
```

Em `tests/plan-cases/valid.tsv` estão os casos válidos e seus JSONs esperados; `invalid.tsv` contém os YAMLs que devem falhar e suas mensagens esperadas. `tests/test-converter.sh` executa a matriz pela **CLI**; o teste Go `TestConverterFixtureMatrix` usa exatamente os mesmos casos. `TestConverterExhaustiveDAGAndBranchMasks` cobre 2.048 combinações de grafos acíclicos com quatro Jobs, máscaras de branch e dependências.

A saída para Jobs paralelos define apenas uma **linearização topológica válida**, não os instantes reais de disparo ou conclusão. O converter não executa comandos; resultados condicionados ao êxito dos Steps são desconhecidos até o runtime. Jobs excluídos e seus dependentes não produzem linhas. Quando nenhum Job é elegível, a saída é `[]` (a Action real rejeita isso).

Contrato completo: [CONTR-005](../docs/contratos/CONTR-005-conversor-yaml.md).

## Grafo de dependências e estado dos Jobs

`bash tests/run.sh --mode graph` compila o binário e valida o subcomando `graph` em cinco cenários: DAG inicial, dependências concluídas, falha bloqueante, filtro `develop` e filtro `main`. `bash tests/run.sh` inclui estes cenários além dos unitários e da matriz de conversão YAML. O modo `graph` não exige KVM/InsulaVM/SQLite.

```bash
./tests/bin/gitmaster-actions graph --file tests/plan-cases/needs-dag.yaml

printf '{"lint":"succeeded","build":"succeeded","package":"succeeded","verify":"succeeded"}\n' > /tmp/gm-status.json
./tests/bin/gitmaster-actions graph --file tests/plan-cases/needs-dag.yaml --statuses /tmp/gm-status.json

./tests/bin/gitmaster-actions graph --branch develop --file examples/workflow/branch-filter-smoke.yaml
```

Cada Job é listado com `job`, `needs`, `dependents`, `has_dependencies`, `state`, `can_run` e, se houver, `waiting_for`, `blocked_by`, `reason` ou `skip_reason`. Um Job sem dependências executa **somente se** estiver pendente e não estiver excluído pelo filtro de branch. `--statuses` recebe JSON `{ "job": "pending|running|succeeded|failed|skipped|cancelled" }`. Status omitido é interpretado como pendente.

Os arquivos e evidências de `tests/test-job-graph.sh` ficam em `tests/work/<execucao>/graph/`; `src/job_graph_test.go` cobre dependências diretas e transitivas, imutabilidade, condições de prontidão, falhas e branches. A avaliação não reserva slots nem executa comandos: a concorrência máxima continua controlada pelo agendador.

## Suite integral de exemplos de YAML, InsulaVM e SQLite

Em [`examples/workflow/`](../examples/workflow/) estão cinco workflows `smoke-01..05` para testar o executor real. A matriz [`vm-examples/cases.tsv`](vm-examples/cases.tsv) define sete cenários, branches e arquivos JSON esperados em `vm-examples/expected/`.

```bash
bash tests/run-vm-examples.sh --list
bash tests/run-vm-examples.sh --dry-run     # testa YAML e matrizes sem VM

bash tests/run-vm-examples.sh --case all \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Somente dependências, por exemplo
bash tests/run-vm-examples.sh --case dependencias \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Via runner centralizado
bash tests/run.sh --mode vm --case examples \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml
```

`--case all` de `tests/run.sh` executa os testes antigos de branch/paralelismo e também os sete novos exemplos. `--output-dir` do runner independente permite escolher um diretório **vazio**, preservado para investigação. O `--dry-run` não abre banco nem VM; o `go test ./...` do CI inclui `TestVMExamplesStatic`, que valida os sete cenários usando o conversor e JobGraph reais.

O verificador compilado de `tests/verifiers/vm-examples/` consulta `actions.db` diretamente, confere `PRAGMA integrity_check`, status e dados de Actions/Jobs/Commands, `needs_json`, `skip_reason`, códigos de saída, snapshots, `vm_id`, `cleanup_status=succeeded`, streams gravados em `command_outputs` e linhas em `command_log_lines`. A falha intencional (`exit 17`) precisa produzir Action `failed`, Job `falhar=failed` e dependente `nao_executar=skipped`. Ela é esperada e aprova o cenário se os registros estiverem corretos.

Pré-requisitos: host Linux com `/dev/kvm`, QEMU/Libvirt, Go, `zip`, catálogo de imagens QCOW2, perfil `ci-smoke` funcional e imagem convidada com `unzip`, SSH e Cloud-Init. Logs/SQLite evidenciam a execução real; o CI não a substitui. Guia: [GUIA-012](../docs/guias/GUIA-012-testes-reais-insulavm.md).
