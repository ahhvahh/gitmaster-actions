<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-012 — Testes reais de YAML, InsulaVM e SQLite

## Objetivo

Executar Jobs definidos em YAML dentro de uma VM descartável do InsulaVM e verificar, sem depender apenas do JSON de saída, se os estados dos Jobs, Commands e logs foram **persistidos no SQLite**.

## Requisitos

- Host Linux com `/dev/kvm`, QEMU/Libvirt configurado e permissões de execução para o InsulaVM.
- Go compatível com `go.mod` (CI: Go 1.27.4) e ferramenta `zip` instalados.
- Configuração administrativa `/etc/insulavm/config.yaml` e catálogo `/etc/gitmaster-actions/profiles.yaml` (ajuste os caminhos ao seu host).
- Perfil **`ci-smoke`** disponível no catálogo e imagem QCOW2 já preparada; o convidado deve ter `unzip`, SSH, Cloud-Init e `workspace` configurado (recomendação: `/tmp/workspace`).
- O arquivo ZIP é criado pelo script, contendo somente `README.txt` e `src/payload.txt` fictícios, sem credenciais. Não é necessário copiar um repositório real.

## Cinco arquivos YAML e sete cenários

| Caso | YAML | Branch | Resultado esperado |
| --- | --- | --- | --- |
| `sequencia` | `smoke-01-sequencia.yaml` | develop | Três comandos sequenciais concluídos, com arquivo intermediário |
| `dependencias` | `smoke-02-dependencias.yaml` | develop | Dois Jobs independentes convergem; `publicar` aguarda ambos |
| `branches-develop` | `smoke-03-branches.yaml` | develop | Executa `somente_develop`; ignora `somente_main` e seu dependente |
| `branches-main` | `smoke-03-branches.yaml` | main | Executa `somente_main` e seu dependente; aplica exclusão do Job alternativo |
| `branches-feature` | `smoke-03-branches.yaml` | feature/demo | Somente Job comum executa |
| `falha-intencional` | `smoke-04-falha.yaml` | develop | `falhar` retorna 17; dependente é ignorado; caminho independente conclui; Action termina `failed` |
| `workspace` | `smoke-05-workspace.yaml` | develop | ZIP é extraído; `working_directory: src` permite ler `payload.txt` |

Os cenários e seus JSONs de expectativa estão em `tests/vm-examples/cases.tsv` e `tests/vm-examples/expected/`.

## Pré-validar sem iniciar VM

```bash
bash tests/run-vm-examples.sh --list
bash tests/run-vm-examples.sh --dry-run
```

`--dry-run` executa a validação estática pela biblioteca Go (`NewJobGraph` e `ConvertYAML`) e confronta o grafo/ordem de comandos com as expectativas dos arquivos; **não** confirma integração real.

## Executar apenas um caso com VM

```bash
bash tests/run-vm-examples.sh --case sequencia \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml
```

## Executar todos os casos

```bash
bash tests/run-vm-examples.sh --case all \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Alternativa pelo runner consolidado:
bash tests/run.sh --mode vm --case examples \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml
```

Para verificar também os fluxos anteriores de branch/paralelismo, use `bash tests/run.sh --mode vm --case all` com as mesmas configurações.

## Evidências e critérios de aprovação

Cada execução cria uma pasta sob `tests/work/vm-examples-<data>-<pid>/<cenario>/` com:

- `actions.db`: banco de dados do teste, exclusivo do cenário.
- `action.json`: detalhes retornados pela CLI ao final da Action.
- `stderr.log`: erros e diagnósticos da CLI.
- `verify.log`: comparações feitas pelo verificador Go.
- `cli-exit-code.txt`: retorno bruto da CLI, inclusive erro intencional.

O diretório da suíte guarda também `snapshot.zip`, o verificador compilado e os logs de compilação. Não remove evidências automaticamente e não reutiliza bancos para evitar ambiguidades. O SQLite é examinado diretamente e precisa satisfazer `PRAGMA integrity_check`, um único registro de Action, metadados de VM e checksum de material, `cleanup_status=succeeded`, Jobs e Commands esperados, `needs_json`, filtros, `skip_reason`, códigos de saída e conteúdo de stdout/linhas de log. Commands ignorados não podem ter código de saída nem logs.

**Falha planejada:** `falha-intencional` retorna exit code 17 e Action `failed`; isso é considerado aprovação do teste se os outros Jobs e o registro da falha corresponderem exatamente ao JSON esperado.

## Limites

Os testes sem VM estão no GitHub Actions e validam parsing, conversão, regras e saídas esperadas. Os testes de InsulaVM/Libvirt devem ser rodados em host KVM real. Sem VM o verificador não pode afirmar que o transporte ou a persistência integral funcionaram.

[Contrato YAML](../contratos/CONTR-003-gitmaster-workflow.md) · [Grafo de Jobs](../contratos/CONTR-006-grafo-dependencias-jobs.md) · [Guia ZIP](GUIA-011-execucao-zip-vm.md)
