# GitMaster Actions

**Estado:** documentação de arquitetura e interfaces proposta; implementação Go pendente.

Biblioteca Go para executar Actions em VMs isoladas com [InsulaVM](https://github.com/ahhvahh/InsulaVM), registrar histórico local em SQLite e permitir consultas a Actions, Jobs, comandos e streams por métodos públicos.

A aplicação consumidora fornece o YAML em memória (`RunYAML`) ou o caminho do manifesto (`RunFile`). A biblioteca gera os UUIDs e administra integralmente seu banco de dados.

## CLI auxiliar de testes

O projeto **também terá** o executável opcional `gitmaster-actions` para execução e inspeção local. Essa CLI será apenas um adaptador sobre a mesma API Go: **não é serviço, daemon, runner GitHub Actions nem outra implementação do executor**.

Exemplos previstos, **ainda não executáveis**:

```bash
gitmaster-actions run --file ./action.yaml --db ./actions.db --insula-config ./insula-config.yaml
gitmaster-actions actions list --db ./actions.db
gitmaster-actions jobs list --action-id "$ACTION_ID" --db ./actions.db
gitmaster-actions commands list --job-id "$JOB_ID" --db ./actions.db
gitmaster-actions outputs list --command-id "$COMMAND_ID" --stream stderr --db ./actions.db
```

Consulte a [documentação](docs/README.md), a [API Go](docs/interfaces/go/INTF-GO-001-biblioteca-publica.md) e o [contrato da CLI de testes](docs/interfaces/terminal/INTF-TERM-001-cli-testes.md).

**Limitação externa conhecida:** o InsulaVM ainda não oferece instalação por ISO nem transporte STDIN/STDOUT/STDERR separado com código de saída estruturado, necessários para algumas funcionalidades propostas.