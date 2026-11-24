<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-005 — Conversor determinístico de YAML para Job/Command

## Objetivo

Isolar as regras de interpretação dos YAMLs e produzir a sequência **potencial** de comandos por Job, sem execução, banco de dados ou máquina virtual. Os mesmos parsers `gitmaster-workflow-v1` e `action-v1` são usados pelo executor. As saídas, golden files e erros são testáveis independentemente.

## API

```go
type CommandPair struct {
    Job     string `json:"job"`
    Command string `json:"command"`
}

type ConvertOptions struct {
    Format    YAMLFormat
    Branch    string
    EventType string
}

func ConvertYAML(yamlData []byte, opts ConvertOptions) ([]CommandPair, error)
```

Retorno ilustrativo:

```json
[
  {"job": "lint", "command": "go vet ./..."},
  {"job": "test", "command": "go test ./..."}
]
```

## Ordem e regras de negócio

1. YAML deve obedecer a um dos dois formatos **declarados explicitamente**. Não há parser genérico para YAMLs arbitrários; a estrutura segue [CONTR-003](CONTR-003-gitmaster-workflow.md) ou [CONTR-001](CONTR-001-configuracao-action.md).
2. No GitMaster, `triggers` podem ser validados via `EventType: commit|push` e `Branch`; `manual` ou vazio simula uma prévia sem filtrar pelo trigger. `branches.exclude` precede `include`.
3. Jobs excluídos por branch e os Jobs dependentes dos excluídos são omitidos da lista. Se nenhum for elegível, retorna `[]`, sem erro. A criação real da Action via `SubmitYAML` ainda exige pelo menos um Job executável.
4. `needs` determina uma ordem **topológica determinística**. A implementação atual visita os Jobs por nome em ordem alfabética e resolve dependências por DFS na ordem declarada em `needs`. Jobs independentes podem concorrer; a ordem da lista é uma linearização válida para inspeção, **não** a ordem de conclusão na VM.
5. Comandos dentro do mesmo Job preservam a ordem de declaração em `steps[]` (GitMaster) ou `commands[]` (action-v1); o conteúdo de `run` é retornado literalmente, sem alteração, interpolações ou inclusão de `working_directory`.
6. `action-v1` tem Jobs em lista, por isso preserva a ordem de cada Job. Imports são validados sintaticamente, mas o converter **não exige a existência física** do arquivo (somente a execução usa snapshot existente).
7. O converter não estima erros de shell, bloqueios em decorrência de falhas futuras, `allow_failure`, duração, execução paralela real nem reenvios `busy`. Esses resultados pertencem ao runtime.

## CLI

```bash
gitmaster-actions plan --format gitmaster-workflow-v1 --branch develop --file ./workflow.yaml
gitmaster-actions plan --format gitmaster-workflow-v1 --branch main --event push --stdin < ./workflow.yaml
gitmaster-actions plan --format action-v1 --file ./action.yaml
```

A CLI devolve somente um array JSON com `job`/`command` na saída padrão; erros na saída de erro e exit code diferente de zero. **Não** inicializa SQLite, InsulaVM, KVM ou recursos do host. A prévia aceita apenas YAML com até 1 MiB e aplica limites de validação dos parsers do executor.

## Matriz de testes

- `tests/plan-cases/valid.tsv`: YAMLs válidos e arrays JSON golden, em formatos e estruturas variadas.
- `tests/plan-cases/invalid.tsv`: YAMLs inválidos e mensagens esperadas (ciclos, necessidades inexistentes, campos desconhecidos, gatilhos etc.).
- `src/converter_test.go`: valida os casos pelos mesmos parsers da biblioteca e cobre 2.048 combinações com DAGs acíclicos de quatro Jobs, filtros e branches.
- `tests/test-converter.sh`: processa cada caso pelo comando CLI, compara com o JSON golden e assegura que não foi criado banco SQLite.
- `bash tests/run.sh --mode plan`: compila e executa somente a matriz do conversor, sem KVM nem credenciais.

Não é possível testar literalmente todas as configurações possíveis de YAML; a matriz e os testes combinatórios aumentam a cobertura. Novos formatos devem ser adicionados como versões declaradas, com contrato, adaptador e fixtures específicos.
