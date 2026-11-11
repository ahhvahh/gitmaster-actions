<!-- A-BIOMA-DOC-STANDARD -->

# CONTR-003 — YAML de workflow GitMaster

## Objetivo

Executar uma Action por workflow e uma VM por Action. A API `SubmitYAML` exige `FormatGitMasterV1`; **não** detecta ou converte automaticamente o manifesto `action-v1` da CLI. O consumidor lê o YAML no commit SHA, prepara o snapshot TAR.GZ e informa origem/branch/evento. O InsulaVM só recebe comandos na VM.

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
- `jobs` é mapa por nome. `needs` exige nomes existentes, sem ciclos ou duplicações. Ordem de execução: topológica, com desempate por nome.
- `triggers[].event`: `commit` ou `push`. Em submissões desses eventos, `branch` deve combinar com `triggers[].branches` (vazia significa todas).
- `jobs.<nome>.branches.include/exclude`: filtros exatos; exclusão tem precedência; Jobs filtrados bloqueiam dependentes.
- `working_directory` é relativo à pasta `guest.destination` configurada pelo administrador, sem `..`, caminhos absolutos ou componentes vazios.
- `timeout_seconds` opcional entre 0 e 86400; 0 usa o limite do runtime InsulaVM por comando.
- `run` deve ser não vazio, sem NUL; não há interpolação nem execução no host.

`ActionOrigin` exige `clone_id`, SHA completo de 40 caracteres, `branch`, `workflow_path` e `event_type` quando a origem Git é informada. O índice de idempotência usa `(clone_id, commit_sha, workflow_path, event_type)`.

## Falhas e segurança

YAML inválido impede criação da Action. Após a aceitação, falhas de VM são persistidas e retornadas em `WaitAction` como estado final `failed` ou `cancelled`; não são falsamente interpretadas como validação aprovada. `stdout`/`stderr` são inferidos dos prefixos textuais de `ExecVM`, sujeitos às limitações do transporte. O pacote não aplica política de segredos específica de cada cliente; o consumidor deve autorizar comandos e materiais.

## Referências

[API Go](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Persistência](../persistencia/PERS-001-registro-execucoes.md)