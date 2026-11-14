<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-003 — YAML de workflow GitMaster

## Objetivo

Executar uma Action por workflow e uma VM por Action. A API `SubmitYAML` exige `FormatGitMasterV1`; **não** detecta ou converte automaticamente o manifesto `action-v1` da CLI. O consumidor lê o YAML no commit SHA, prepara o snapshot ZIP (TAR.GZ permanece compatível) e informa origem/branch/evento. O InsulaVM só recebe comandos na VM.

## Exemplo

[Exemplo executável de parsing](../../examples/workflow/go-ci.yaml).

```yaml
version: 1
name: go-ci
vm_profile: ci-go
triggers:
  - event: commit
    branches: [develop, main]
jobs:
  test:
    steps:
      - name: Tests
        working_directory: src/go
        run: go test ./...
  build:
    needs: [test]
    steps:
      - name: Compile
        run: go build ./...
```

## Validação

- `version` deve ser `1`; `name` não pode ficar vazio; `vm_profile` deve identificar perfil registrado.
- Exatamente um documento YAML, sem campos desconhecidos; no máximo 1 MiB, 100 Jobs e 100 Steps por Job.
- `jobs` é mapa por nome. `needs` exige nomes existentes, sem ciclos ou duplicações. O agendador libera Jobs assim que todas as dependências forem concluídas; dependências são verificadas por nome. Independentes podem ficar `running` em paralelo.
- `triggers[].event`: `commit` ou `push`. Em submissões desses eventos, `branch` deve combinar com `triggers[].branches` (vazia significa todas).
- `jobs.<nome>.branches.include/exclude`: filtros exatos; exclusão tem precedência; Jobs filtrados bloqueiam dependentes.
- `working_directory` é relativo à pasta `workspace` do perfil configurado pelo administrador (padrão recomendado: `/tmp/workspace`), sem `..`, caminhos absolutos ou componentes vazios.
- `timeout_seconds` opcional entre 0 e 86400; 0 usa o limite do runtime InsulaVM por comando.
- `run` deve ser não vazio, sem NUL; não há interpolação nem execução no host.

Para integração de eventos Git, `ActionOrigin` exige `clone_id`, SHA completo de 40 caracteres, `branch`, `workflow_path` e `event_type` quando a origem Git é informada. O índice de idempotência usa `(clone_id, commit_sha, workflow_path, event_type)`. Além da chave de identidade, o replay compara SHA256 do YAML, SHA256 do snapshot ZIP/TAR.GZ, `event_id`, branch e formato; divergências retornam conflito e nunca reutilizam uma aprovação anterior.

## Falhas e segurança

YAML inválido impede criação da Action. Após a aceitação, falhas de VM são persistidas e retornadas em `WaitAction` como estado final `failed` ou `cancelled`; não são falsamente interpretadas como validação aprovada. `stdout`/`stderr` são inferidos dos prefixos textuais de `ExecVM`, sujeitos às limitações do transporte. Os validadores ZIP/TAR.GZ recusa caminhos típicos de credenciais (como `.env`, `.ssh`, `.aws`, arquivos `.key` e `.p12`), além de links e entradas inseguras. Limita o arquivo comprimido a 100 MiB, o material descomprimido a 256 MiB, 15.000 arquivos e 20.000 entradas totais. Essa lista não detecta todos os segredos; o consumidor ainda deve autorizar comandos e materiais e executar sua própria política de exclusão de credenciais.

## Referências

[API Go](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Persistência](../persistencia/PERS-001-registro-execucoes.md)
## Execução manual

O CLI aceita origem manual contendo somente `branch` validada, sem inventar SHA ou ID de clone. Executa o mesmo workflow/Jobs via `--format gitmaster-workflow-v1 --branch <nome> --archive <arquivo.zip>`; a integração GitMaster mantém os metadados de evento completos e imutáveis. O ZIP passa por verificação de caminhos, modos, arquivos de credenciais e tamanho antes da transferência. A VM deve possuir `unzip` instalado no QCOW2 congelado.

## Paralelismo, ordenação e BUSY

- `max_parallel_jobs`: inteiro opcional de 1 a 16; quando omitido/zero, o padrão é **1**, preservando a execução sequencial antiga. Exemplo de ensaio: `max_parallel_jobs: 4`.
- Jobs sem `needs` são elegíveis imediatamente. Um Job com `needs: [a, b]` somente inicia depois de **ambos** terem `succeeded`. Jobs pendentes com dependência falha ou filtrada são marcados `skipped` com `skip_reason=dependency_not_succeeded:<nome>`.
- `jobs.<nome>.branches.include/exclude` exclui Jobs antes da execução. Excluir um Job por branch também bloqueia seus dependentes, mas não equivale a erro de comando: workflows sem falha real podem terminar `succeeded` mesmo com Jobs `skipped`.
- O paralelismo controla a **elegibilidade e execução dos Jobs**, não a concorrência de shells dentro da mesma VM. Como o InsulaVM permite um `ExecVM` por VM, os comandos compartilham um token de exclusão dentro da Action; cada Job mantém seus próprios comandos **sequenciais**. A execução em um mesmo convidado não é CPU-paralela; Jobs podem aparecer simultaneamente como `running` enquanto aguardam o token.
- Em concorrência com outro processo, `errors.Is(err, insulavm.ErrVMExecBusy)` provoca **5 novas tentativas**, além da original (6 no total), com atrasos independentes aleatórios de **10–1.000 ms**, respeitando cancelamento e timeout. Só se repete `busy`, nunca falhas normais de um comando, para não duplicar efeitos colaterais. Cada nova tentativa é registrada em `runtime_events` como `vm_busy_retry`. Após o limite, falha de infraestrutura.
- Jobs independentes continuam mesmo que outro Job falhe; dependentes de falhas são pulados. Cancelamento/erro de infraestrutura termina a execução e aguarda os Jobs ativos antes de parar a VM.
- O CLI `run --details` apresenta o grafo persistido, estados `succeeded/failed/skipped`, códigos de retorno, mensagens e eventos de retry.

Para um ensaio reprodutível use [dependency-parallel-smoke.yaml](../../examples/workflow/dependency-parallel-smoke.yaml).

## Projeção de plano para testes

`ConvertYAML` e `gitmaster-actions plan` reutilizam o parser deste contrato para extrair os comandos executáveis após filtros de branch e dependências estáticas. A lista de pares possui apenas os campos `job` e `command`; não é o histórico real da VM. Ver [CONTR-005](CONTR-005-conversor-yaml.md).

## Consulta de dependências e prontidão

O `JobGraph` expõe as arestas diretas e transitivas derivadas de `jobs.<name>.needs`, o caminho inverso dos dependentes e a avaliação de prontidão por status. O agendador reutiliza suas decisões. A validação de ciclos, nomes inexistentes e filtros permanece no parser YAML comum. [CONTR-006](CONTR-006-grafo-dependencias-jobs.md).
