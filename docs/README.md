<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — GitMaster Actions

Fontes canônicas, vinculadas ao código Go e aos testes. Os documentos em `migracao/gitmaster/` são **referências históricas**; não descrevem automaticamente o estado do executor atual.

| Documento | Fonte de verdade |
| --- | --- |
| [ARQ-001 — Executor](arquitetura/ARQ-001-executor-actions.md) | Componentes, isolamento e limites |
| [ARQ-002 — Projetos](arquitetura/ARQ-002-separacao-gitmaster.md) | Fronteira GitMaster/Actions/InsulaVM |
| [CONTR-001 — Action local](contratos/CONTR-001-configuracao-action.md) | YAML `action-v1`, CLI e wrappers síncronos |
| [CONTR-002 — Profiles](contratos/CONTR-002-criacao-perfil.md) | Cadastro de QCOW2 e imagens |
| [CONTR-003 — Workflow GitMaster](contratos/CONTR-003-gitmaster-workflow.md) | YAML GitMaster, eventos, dependências e SHA |
| [INTF-GO-001 — API Go](interfaces/go/INTF-GO-001-biblioteca-publica.md) | Assinaturas e retornos Go públicos |
| [INTF-TERM-001 — CLI](interfaces/terminal/INTF-TERM-001-cli-testes.md) | Sintaxe CLI realmente disponível |
| [PERS-001 — SQLite](persistencia/PERS-001-registro-execucoes.md) | Esquema, migração e recuperação |
| [FLUX-001 — Execução](fluxos/FLUX-001-execucao-action.md) | Sequência submit/worker/VM/cleanup |
| [GUIA-008 — Integração](guias/GUIA-008-integracao-gitmaster.md) | Integração do consumidor e histórico legado |
| [GUIA-009 — Validação Go 1.27](guias/GUIA-009-validacao-go127.md) | Ferramentas, comandos e limitações do CI |
| [ARQ-006 histórico](migracao/gitmaster/arquitetura/ARQ-006-adaptador-insulavm.md) | Material antigo mantido para auditoria |

**Estado geral:** o pacote principal tem API assíncrona, SQLite independente e runtime InsulaVM via Go; o consumidor `GitMaster` antigo ainda necessita migração explícita. Testes KVM/Libvirt reais pendentes.
- [CONTR-004 — Catálogo de perfis QCOW2 congelados](contratos/CONTR-004-perfis-congelados.md)
- [GUIA-011 — Execução de workflow com ZIP na VM](guias/GUIA-011-execucao-zip-vm.md)

- [CONTR-005 — Conversor YAML para lista Job/Command](contratos/CONTR-005-conversor-yaml.md)

- [CONTR-006 — Grafo de dependências e avaliação de prontidão dos Jobs](contratos/CONTR-006-grafo-dependencias-jobs.md)

- [GUIA-012 — Suite integral de YAMLs e validação SQLite na InsulaVM real](guias/GUIA-012-testes-reais-insulavm.md)
