# GitMaster Actions

**Estado:** documentação de arquitetura e interfaces proposta; implementação Go pendente.

Biblioteca Go para executar Actions em **VMs locais clonadas de perfis QCOW2 já preparados** com [InsulaVM](https://github.com/ahhvahh/InsulaVM), registrar histórico local em SQLite e permitir consultas a Actions, Jobs, comandos e streams por métodos públicos. Cada Action contém Jobs diretamente (sem pipelines). Memória e CPUs vêm do perfil, com sobrescrita opcional na execução.

A aplicação consumidora fornece o YAML em memória (`RunYAML`) ou o caminho do manifesto (`RunFile`). A biblioteca gera os UUIDs e administra integralmente seu banco de dados. As consultas navegam por `Action.Jobs(ctx)`, `Job.Commands(ctx)` e `Command.Logs(ctx, skipLines...)`, permitindo buscar somente linhas ainda não recebidas.

## CLI auxiliar de testes

O projeto **também terá** o executável opcional `gitmaster-actions` para execução e inspeção local. Essa CLI será apenas um adaptador sobre a mesma API Go: **não é serviço, daemon, runner GitHub Actions nem outra implementação do executor**.

Exemplos previstos, **ainda não executáveis**:

```bash
gitmaster-actions run --file ./action.yaml --db ./actions.db --insula-config ./insula-config.yaml
gitmaster-actions actions list --db ./actions.db
gitmaster-actions jobs list --action-id "$ACTION_ID" --db ./actions.db
gitmaster-actions commands list --job-id "$JOB_ID" --db ./actions.db
gitmaster-actions logs list --command-id "$COMMAND_ID" --skip-lines 150 --db ./actions.db
gitmaster-actions outputs list --command-id "$COMMAND_ID" --stream stderr --db ./actions.db
```

Consulte a [documentação](docs/README.md), a [API Go](docs/interfaces/go/INTF-GO-001-biblioteca-publica.md) e o [contrato da CLI de testes](docs/interfaces/terminal/INTF-TERM-001-cli-testes.md).

**Limites:** o GitMaster Actions e o InsulaVM executam no mesmo host; não há recuperação automática das VMs após queda/reinício. O SQLite não guarda configuração nem identificador da VM. O contrato usa somente perfis existentes; STDIO separado e exit code estruturado ainda dependem de evolução no InsulaVM.