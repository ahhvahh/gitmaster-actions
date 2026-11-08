<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — GitMaster Actions

**Estado:** biblioteca Go e CLI de testes propostas, sem implementação neste repositório. **Não é serviço nem daemon; a CLI é opcional.** Convenção A-Bioma Doc Standard, sem alegar certificação.

| Documento | Fonte canônica de |
| --- | --- |
| [ARQ-001 — Arquitetura](arquitetura/ARQ-001-executor-actions.md) | Componentes, dependências e isolamento |
| [INTF-GO-001 — API pública](interfaces/go/INTF-GO-001-biblioteca-publica.md) | `New`, `RunYAML`, `RunFile`, navegação por objetos e logs incrementais |
| [INTF-TERM-001 — CLI de testes](interfaces/terminal/INTF-TERM-001-cli-testes.md) | Sintaxe, comandos, parâmetros, STDIO e códigos de saída |
| [CONTR-001 — YAML](contratos/CONTR-001-configuracao-action.md) | Estrutura e validação do manifesto |
| [PERS-001 — SQLite](persistencia/PERS-001-registro-execucoes.md) | Tabelas, IDs, índices, relacionamentos |
| [FLUX-001 — Execução](fluxos/FLUX-001-execucao-action.md) | Ordem, falhas, cancelamento e cleanup |

**Ambiente:** uma Action usa uma VM clonada de perfil existente no InsulaVM local; os Jobs pertencem diretamente à Action. O banco SQLite não armazena dados da VM e não há recuperação após queda/reinício do servidor. **Dependência pendente:** STDIO separado com exit code (`ExecuteCommand`) no InsulaVM.