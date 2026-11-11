# GitMaster Actions

**Estado:** biblioteca Go e CLI inicial implementadas em `main`; compilação e testes em VM real ainda não verificados. APIs futuras de perfis/rede e streams nativos continuam propostas.

Biblioteca Go para executar Actions em **VMs locais clonadas de perfis QCOW2 já preparados** com [InsulaVM](https://github.com/ahhvahh/InsulaVM), registrar histórico local em SQLite e permitir consultas a Actions, Jobs, comandos e streams por métodos públicos. Cada Action contém Jobs diretamente (sem pipelines). Memória e CPUs vêm do perfil, com sobrescrita opcional na execução.

A aplicação consumidora fornece o YAML em memória (`RunYAML`) ou o caminho do manifesto (`RunFile`). A biblioteca gera os UUIDs e administra integralmente seu banco de dados. As consultas navegam por `Action.Jobs(ctx)`, `Job.Commands(ctx)` e `Command.Logs(ctx, skipLines...)`, permitindo buscar somente linhas ainda não recebidas.

## Criação de perfis

Foi definido o método administrativo **proposto** `Engine.CreateProfile(ctx, req)`, que delegará ao InsulaVM a criação de um perfil nomeado a partir de QCOW2 preparado ou VM existente, com RAM, CPUs e permissão de internet. `RegisterProfile` já existe para imagens QCOW2; a escolha de rede por perfil e clonagem de VM sem remover a origem **ainda precisam ser implementadas no InsulaVM**. O método não altera o modelo SQLite de Actions. Ver [CONTR-002](docs/contratos/CONTR-002-criacao-perfil.md).

## CLI auxiliar de testes

O projeto **também inclui** o executável opcional `gitmaster-actions` para execução e inspeção local. Essa CLI será apenas um adaptador sobre a mesma API Go: **não é serviço, daemon, runner GitHub Actions nem outra implementação do executor**.

Exemplos da interface proposta (algumas flags abaixo permanecem pendentes; consulte os comandos reais ao final):

```bash
gitmaster-actions profiles create --name debian13-mysql --source-qcow2 /srv/images/debian13-mysql.qcow2 --memory-mib 4096 --vcpus 2 --internet no --db ./actions.db --insula-config ./insula-config.yaml
gitmaster-actions run --file ./action.yaml --db ./actions.db --insula-config ./insula-config.yaml
gitmaster-actions actions list --db ./actions.db
gitmaster-actions jobs list --action-id "$ACTION_ID" --db ./actions.db
gitmaster-actions commands list --job-id "$JOB_ID" --db ./actions.db
gitmaster-actions logs list --command-id "$COMMAND_ID" --skip-lines 150 --db ./actions.db
gitmaster-actions outputs list --command-id "$COMMAND_ID" --stream stderr --db ./actions.db
```

Consulte a [documentação](docs/README.md), a [API Go](docs/interfaces/go/INTF-GO-001-biblioteca-publica.md) e o [contrato da CLI de testes](docs/interfaces/terminal/INTF-TERM-001-cli-testes.md).

**Limites:** o GitMaster Actions e o InsulaVM executam no mesmo host; não há recuperação automática das VMs após queda/reinício. O SQLite não guarda configuração nem identificador da VM. O contrato usa somente perfis existentes; STDIO separado e exit code estruturado ainda dependem de evolução no InsulaVM.