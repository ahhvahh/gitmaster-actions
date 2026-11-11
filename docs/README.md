<!-- A-BIOMA-DOC-STANDARD -->

# Documentação — GitMaster Actions

Uma fonte por contrato e uma distinção explícita entre código implementado e dependências ainda não validadas em VM real.

| Documento | Assunto |
| --- | --- |
| [CONTR-001 — Action local](contratos/CONTR-001-configuracao-action.md) | Manifesto `action-v1` usado pela CLI legada |
| [CONTR-003 — Workflow GitMaster](contratos/CONTR-003-gitmaster-workflow.md) | Formato `gitmaster-workflow-v1`, eventos e dependências |
| [INTF-GO-001 — API](interfaces/go/INTF-GO-001-biblioteca-publica.md) | Submissão, consultas, perfis, cancelamento e importação |
| [PERS-001 — SQLite](persistencia/PERS-001-registro-execucoes.md) | Esquema, migrações e histórico |
| [GUIA-008 — Integração](guias/GUIA-008-integracao-gitmaster.md) | Configuração e ligação sem modificar GitMaster |
| [GUIA-009 — Validação Go 1.27](guias/GUIA-009-validacao-go127.md) | Testes locais, CI complementar e limites KVM |
| [ARQ-001 — Executor](arquitetura/ARQ-001-executor-actions.md) | Arquitetura e isolamento, histórico da implementação inicial |
| [FLUX-001 — Execução](fluxos/FLUX-001-execucao-action.md) | Fluxo inicial, sujeito às atualizações do contrato assíncrono |
| [INTF-TERM-001 — CLI](interfaces/terminal/INTF-TERM-001-cli-testes.md) | Modo local existente |
| [Documentação migrada](migracao/gitmaster/arquitetura/ARQ-006-adaptador-insulavm.md) | Referências históricas preservadas |

**Estado:** o módulo especifica Go 1.27. Testes KVM/Libvirt não executados nesta entrega. A biblioteca principal usa seu SQLite próprio; o pacote `legacy` permanece para compatibilidade e requer migração explícita dos dados antigos.