<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-001 — CLI auxiliar para testes locais

**Estado:** interface proposta; nenhum binário ou comando foi implementado neste repositório.

## Objetivo e limite

Fornecer o executável opcional `gitmaster-actions` (diretório Go previsto: `cmd/gitmaster-actions`) para testar a biblioteca localmente, executar manifestos YAML e inspecionar o SQLite por **métodos públicos da biblioteca**. A CLI **não é serviço, daemon ou runner GitHub Actions**; não cria schema, não escreve SQL, não controla VM por fora da biblioteca e não reimplementa o parser.

As entradas de execução são mutuamente exclusivas: `--file CAMINHO` ou `--stdin` (YAML lido em bytes). Um dos dois é obrigatório em `run`. A CLI não oferece flags para escolher a VM: o manifesto [CONTR-001](../../contratos/CONTR-001-configuracao-action.md) exige `vm.profile` já preparado no InsulaVM local, e memória/CPUs são opcionais.

## Sintaxe proposta

```text
gitmaster-actions run (--file ARQUIVO | --stdin) --db CAMINHO --insula-config CAMINHO [--json]
gitmaster-actions actions list --db CAMINHO [--json]
gitmaster-actions actions get --id UUID --db CAMINHO [--json]
gitmaster-actions jobs list --action-id UUID --db CAMINHO [--json]
gitmaster-actions jobs get --id UUID --db CAMINHO [--json]
gitmaster-actions commands list --job-id UUID --db CAMINHO [--json]
gitmaster-actions commands get --id UUID --db CAMINHO [--json]
gitmaster-actions logs list --command-id UUID --db CAMINHO [--skip-lines N] [--format text|json]
gitmaster-actions outputs list --command-id UUID --db CAMINHO [--stream stdin|stdout|stderr] [--format text|json|raw]
gitmaster-actions profiles create --name NOME (--source-qcow2 CAMINHO | --source-vm UUID) --memory-mib MiB --vcpus N --internet yes|no --db CAMINHO --insula-config CAMINHO [--json]
gitmaster-actions --help
```

`profiles create` é interface **proposta**, exclusivamente para testar o novo método `CreateProfile`. Exige somente uma origem de disco e escolha explícita `--internet yes|no`. `--source-vm` dependerá de clonagem não destrutiva; `--internet` depende de rede por perfil no InsulaVM. Não ignorar flags sem suporte. Contrato: [CONTR-002](../../contratos/CONTR-002-criacao-perfil.md).

`--skip-lines N` aceita um inteiro não negativo (padrão 0) e solicita apenas linhas com número maior que N. `logs list` retorna no máximo 500 linhas e usa `LineNumber` para a próxima consulta; `outputs list` permanece distinto para chunks brutos.

`--db` é o caminho do SQLite administrado pela biblioteca. `--insula-config` informa a configuração administrativa InsulaVM **somente** para execução. O formato humano é padrão para listagens; `--json` oferece registros estruturados. `outputs list --format raw` escreve exatamente os bytes `payload`, sem cabeçalhos, conversão, decoração ou alteração de quebra de linha. Em `--format text`, os bytes são exibidos para inspeção, e dados binários podem não ser legíveis.

A CLI é invocada por processo **no mesmo host que executa InsulaVM**: cada comando inicializa um `Engine`, chama a API e libera recursos. Consultas ao banco não carregam VM/Libvirt; execução e cancelamento usam contexto do processo. Não há recuperação de VMs após queda/reinício; `running` persistido pode ser apenas o último estado conhecido.

## Mapeamento de métodos

| Comando | Operação única da biblioteca | Entrada relevante | Saída |
| --- | --- | --- | --- |
| `profiles create` | `CreateProfile` | Nome, fonte QCOW2/VM, recursos e internet | Perfil criado ou erro explícito |
| `run --file` | `RunFile` | Arquivo YAML, `Options` | ID, estado, ID da VM, erro ou resumo |
| `run --stdin` | `RunYAML` | Bytes do stdin, `Options` | Mesmo resultado da execução por arquivo |
| `actions list/get` | `ListActions` / `GetAction` | Filtros ou UUID | Ações e estados |
| `jobs list/get` | `GetAction(...).Jobs(ctx)` / `GetJob` | UUID Action ou Job | Jobs e estados |
| `commands list/get` | `GetJob(...).Commands(ctx)` / `GetCommand` | UUID Job ou Comando | Comandos, exit code e estados |
| `logs list` | `GetCommand(...).Logs(ctx, skipLines)` | UUID Comando e cursor | Até 500 linhas numeradas, sem repetição |
| `outputs list` | `GetCommand(...).Outputs(ctx, filter, page)` | UUID Comando, filtro de stream | Chunks ou bytes brutos |

Contratos dos tipos, erros de negócio e consultas: [INTF-GO-001](../go/INTF-GO-001-biblioteca-publica.md). Entidades e tipos de stream: [PERS-001](../../persistencia/PERS-001-registro-execucoes.md). Fluxo executado por `run`: [FLUX-001](../../fluxos/FLUX-001-execucao-action.md).

## STDIN, STDOUT, STDERR e sinais

- `run --stdin` consome integralmente o YAML do **stdin local**. Isso não implica persistir o stdin da execução remota.
- `stdout` local: saída humana ou JSON estruturado, exceto em `outputs list --format raw`, que entrega bytes brutos.
- `stderr` local: diagnósticos, erros de configuração, armazenamento, VM ou transporte; não misturar com `stdout` bruto.
- `Ctrl+C`/SIGINT: cancelar o `context.Context` da execução ativa e aguardar tentativa de cleanup; não criar ferramenta de cancelamento remoto entre processos nesta versão.
- Logs STDOUT/STDERR da **VM** podem ser consultados por `outputs list` somente após captura válida pela biblioteca. Não confundir canais do guest com canais da própria CLI.
- Ao imprimir ou exportar logs, lembrar que streams e comandos podem conter dados sensíveis. Evitar exibi-los por padrão nas listagens resumidas.

## Códigos de saída (propostos)

| Código | Significado |
| --- | --- |
| `0` | Operação de leitura concluída ou Action executada com sucesso |
| `1` | Action concluída com falha ou erro durante sua execução |
| `2` | Argumentos inválidos, YAML inválido ou origem de VM não suportada |
| `3` | Erro de infraestrutura, conexão InsulaVM ou SQLite |
| `4` | UUID válido, mas registro não localizado |
| `130` | Interrupção por SIGINT, após tentativa de cleanup |

Saídas de erro não devem fabricar `action_id`, `job_id`, `vm_id` ou resultados inexistentes. Se uma execução falhar **após** persistir a Action, mostrar o ID existente para consulta posterior. `--json` deve codificar erros em stderr com formato consistente, sem alterar a semântica dos códigos acima.

## Casos de uso propostos (não executáveis atualmente)

```bash
# Registrar perfil de imagem preparada (a política de rede ainda requer implementação)
gitmaster-actions profiles create --name debian13-mysql --source-qcow2 /srv/images/debian13-mysql.qcow2 --memory-mib 4096 --vcpus 2 --internet no --db ./actions.db --insula-config ./insula-config.yaml

# Executar manifesto usando um perfil InsulaVM definido no próprio YAML
gitmaster-actions run --file ./action.yaml --db ./actions.db --insula-config ./insula-config.yaml

# Enviar o mesmo YAML pelo stdin da CLI
cat ./action.yaml | gitmaster-actions run --stdin --db ./actions.db --insula-config ./insula-config.yaml --json

# Acompanhar histórico depois da execução
gitmaster-actions actions list --db ./actions.db
gitmaster-actions jobs list --action-id 018f0000-0000-7000-8000-000000000001 --db ./actions.db

# Consultar apenas logs após as primeiras 150 linhas
gitmaster-actions logs list --command-id 018f0000-0000-7000-8000-000000000002 --skip-lines 150 --format json --db ./actions.db

# Ver saída de erro de um comando sem alterar os bytes
gitmaster-actions outputs list --command-id 018f0000-0000-7000-8000-000000000002 --stream stderr --format raw --db ./actions.db
```

Os UUIDs acima são **exemplos ilustrativos**, não registros encontrados no SQLite. Os comandos são exemplos de interface a desenvolver; `go run` e testes locais só estarão disponíveis depois da implementação Go.

## Critérios de aceite futuros

1. Comando `run` chama exclusivamente `RunFile` ou `RunYAML`, sem alterar o manifesto.
2. Consultas usam somente API pública e não exigem QEMU/SSH ou configuração InsulaVM.
3. `--format raw` preserva bytes e não escreve texto adicional em stdout.
4. Uma execução com falha retorna código não zero e o ID real quando já houver registro.
5. Interrupção encerra contexto, preserva logs parciais e tenta limpeza via biblioteca.
6. O binário opcional não instala serviço nem requer processo residente.

## Referências

- [INTF-GO-001 — Biblioteca pública](../go/INTF-GO-001-biblioteca-publica.md)
- [CONTR-001 — Entrada YAML](../../contratos/CONTR-001-configuracao-action.md)
- [CONTR-002 — Criar perfil](../../contratos/CONTR-002-criacao-perfil.md)
- [PERS-001 — Persistência](../../persistencia/PERS-001-registro-execucoes.md)
- [FLUX-001 — Execução](../../fluxos/FLUX-001-execucao-action.md)