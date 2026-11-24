<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-006 — Grafo de dependências entre Jobs

## Objetivo

Organizar a relação `Job -> Jobs necessários` e o caminho inverso `Job -> Jobs dependentes`, permitindo consultar se um Job pode iniciar sem disparar uma VM, abrir SQLite ou executar comandos.

## Modelo

`NewJobGraph(yamlData []byte, opts ConvertOptions) (*JobGraph, error)` constrói um grafo DAG imutável, utilizando exatamente o parser de workflow da biblioteca (GitMaster workflow-v1 ou action-v1). O mesmo grafo também é construído internamente pelo agendador. Valida nomes, filtros, dependências inexistentes, dependências duplicadas e ciclos.

Cada `GraphJob` possui:

| Propriedade | Significado |
| --- | --- |
| `job` | Nome do Job |
| `needs` | Lista de dependências diretas, na ordem declarada |
| `dependents` | Jobs que dependem diretamente deste Job, em ordem topológica |
| `has_dependencies` | Se o Job declara pelo menos uma dependência |
| `skip_reason` | Se o Job foi desativado pelo filtro de branch ou pela dependência filtrada |

## Regras de prontidão

1. **Sem dependências**: um Job com `needs: []`, estado `pending` e branch permitida tem `can_run: true`. Não confundir independência estrutural com autorização para executar.
2. **Com dependências**: `can_run: true` somente se todas as dependências tiverem estado `succeeded`. Uma dependência `running` ou `pending` mantém o Job em `waiting`.
3. **Falha de dependência**: dependência `failed`, `skipped` ou `cancelled` bloqueia o Job; enquanto outras dependências ainda estiverem pendentes, o resultado é `waiting`. Quando todas terminarem, o estado calculado é `blocked`, com `reason: dependency_not_succeeded:<nome>`.
4. **Filtragem por branch**: Job filtrado e seus dependentes filtrados têm `state: skipped`, `can_run: false`, mesmo sem dependências. `branches.exclude` prevalece sobre `include`.
5. **Job já iniciado ou terminado**: `running`, `succeeded`, `failed`, `skipped` e `cancelled` nunca autorizam uma segunda execução.
6. **Ordem**: `Jobs()`, `ReadyJobs()` e `Evaluate()` retornam os Jobs em ordem topológica determinística. O grafo não representa tempo de execução; Jobs independentes podem ser iniciados simultaneamente respeitando `max_parallel_jobs` e o gate de comandos do InsulaVM.

Estados fornecidos pelo chamador: `map[string]string` com valores `pending|running|succeeded|failed|skipped|cancelled`; entrada omitida significa `pending`. Nome ou status desconhecido retorna erro. A estrutura do grafo não guarda nem altera os estados do runtime.

## API Go

```go
g, err := gitmasteractions.NewJobGraph(yamlBytes, gitmasteractions.ConvertOptions{
    Format: gitmasteractions.FormatGitMasterV1, Branch: "develop",
})
if err != nil { return err }

jobs := g.Jobs()                         // []GraphJob (cópias independentes)
needs, err := g.Dependencies("test")     // []string
children, err := g.Dependents("build")  // []string
has, err := g.HasDependencies("test")   // bool
depends, err := g.DependsOn("release", "build") // bool (transitiva)
state := map[string]string{"lint": "succeeded"}
decision, err := g.Decision("build", state)  // JobDecision
ready, err := g.ReadyJobs(state)             // []string
all, err := g.Evaluate(state)                // []JobDecision
```

`JobDecision` inclui os campos de `GraphJob`, `state`, `can_run` e, quando aplicável, `waiting_for`, `blocked_by` e `reason`. A API é pura e segura para múltiplas leituras simultâneas, desde que o chamador não modifique os mapas de estados durante a consulta.

## CLI

```bash
gitmaster-actions graph --format gitmaster-workflow-v1 --branch develop --file workflow.yaml

printf '{"lint":"succeeded","build":"succeeded"}\n' > /tmp/job-statuses.json
gitmaster-actions graph --file workflow.yaml --statuses /tmp/job-statuses.json

gitmaster-actions graph --format action-v1 --file action.yaml
```

A CLI retorna JSON com o grafo e decisões para todos os Jobs. `--statuses` é opcional; sem ele todos os Jobs começam como `pending`. `--file` pode ser substituído por `--stdin`. O comando não cria arquivos SQLite nem inicializa o InsulaVM.

## Testes

- `src/job_graph_test.go`: dependências diretas/transitivas, dependentes, cópias de slices, transições de `ready`/`waiting`/`blocked`, branches `develop`/`main`, entradas inválidas e action-v1.
- `job_graph_public_test.go`: acesso a partir do caminho público de importação.
- `tests/test-job-graph.sh`: cinco testes pela CLI, incluindo mudança de estado e falha de dependência, sem VM nem SQLite.
- `bash tests/run.sh --mode graph`: roda apenas casos de CLI do grafo. O modo `unit` inclui os testes Go e os casos CLI.

Esta API **não** verifica recursos da VM, exclusão mútua entre processos, concorrência máxima, resultados futuros de comandos ou readiness dinâmica por alterações em arquivos externos. Essas decisões permanecem na execução real.
