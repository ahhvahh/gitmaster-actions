<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-001 — CLI opcional de testes

**Estado:** comandos listados abaixo implementados em `cmd/gitmaster-actions/main.go`; não há serviço/daemon nem execução Git.

## Sintaxe disponível

```text
gitmaster-actions run --file ARQUIVO --db actions.db --insula-config config.yaml
gitmaster-actions run --stdin --db actions.db --insula-config config.yaml
gitmaster-actions actions list|get --db actions.db [--id UUID]
gitmaster-actions jobs list|get --db actions.db [--action-id UUID|--id UUID]
gitmaster-actions commands list|get --db actions.db [--job-id UUID|--id UUID]
gitmaster-actions logs list --command-id UUID --db actions.db [--skip-lines N]
gitmaster-actions outputs list --command-id UUID --db actions.db [--stream stdout|stderr] [--limit N]
gitmaster-actions profiles list --insula-config config.yaml
gitmaster-actions profiles images --insula-config config.yaml --images-dir DIR
gitmaster-actions profiles register --insula-config config.yaml --images-dir DIR --name NOME --image NOME.qcow2 --vcpus N --memory-mib N --disk-gib N
```

O manifesto usado por `run` é `action-v1`, descrito em [CONTR-001](../../contratos/CONTR-001-configuracao-action.md). `run --file` resolve importações relativas ao diretório do YAML, chama o executor unificado e aguarda a Action terminar. `run --stdin` lê até 2 MiB de entrada, mas o parser rejeita YAML superior a 1 MiB.

## Saídas e erros

- A CLI usa JSON estruturado em stdout para respostas, e mensagens de erro em stderr; consultas não expõem SQL.
- O código de saída atual é `0` em sucesso e `1` para argumentos inválidos, erro de Action ou erro de infraestrutura. Não há códigos 2/3/4/130 implementados.
- Não existem flags `--format raw`, `--json` ou modo de criação de perfil a partir de VM de origem. Não anunciar suporte inexistente.
- `Ctrl+C` cancela o contexto local; o wrapper síncrono solicita cancelamento da Action e aguarda a limpeza. O retorno do processo não equivale a uma garantia de cleanup em caso de término forçado (`SIGKILL`).

## Referências

[Interface Go](../go/INTF-GO-001-biblioteca-publica.md) · [Fluxo](../../fluxos/FLUX-001-execucao-action.md) · [Profiles](../../contratos/CONTR-002-criacao-perfil.md)