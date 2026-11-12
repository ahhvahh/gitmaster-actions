<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-008 — Integração e migração do GitMaster

**Estado:** roteiro para o consumidor, sem modificação automática do repositório GitMaster.

## Pré-requisitos

- Go 1.27, InsulaVM local configurado e perfil QCOW2 existente.
- Diretório gravável e exclusivo para `actions.db`, distinto do SQLite do GitMaster. Apenas um Engine ativo por banco (lock `<DatabasePath>.lock`).
- YAML da árvore imutável do SHA e snapshot TAR.GZ autorizado pelo consumidor.

## Nova execução

1. Fixar a dependência em um commit de `main` **que esteja comprovadamente validado**, nunca assumir que um commit apenas publicado passou nos testes.
2. Inicializar um único `Engine` por arquivo SQLite e compartilhar a instância entre as requisições do serviço.
3. Ler `.gitmaster/workflow/*.yaml|*.yml` na árvore do SHA; enviar `SubmitRequest{Format: FormatGitMasterV1, YAML, Origin, ArchivePath}`.
4. Guardar `Action.ID`; acompanhar por `WaitAction`, `GetActionDetails`, `ListCommandLogs`, `ListCommandOutputs` e `ListRuntimeEvents`.
5. Tratar `pending` como enfileirado, nunca aprovado; exigir estado final `succeeded` e validação ligada ao mesmo commit/contexto antes do push.
6. No reinício, inspecionar interrupções e chamar `ReconcileInterrupted` quando o InsulaVM estiver disponível. Não retomar comandos antigos automaticamente.

## Migração do SQLite anterior

1. Fazer backup consistente do SQLite legado (incluindo WAL, se houver) e conservar o original somente leitura.
2. Executar `ImportLegacyHistory(ctx, caminhoAbsoluto)` no novo Engine. Uma transação preserva os dados brutos em `legacy_archive`, normaliza Actions/Jobs/Commands/logs/eventos e registra IDs em `legacy_links`.
3. Comparar a contagem de cada tabela com `ListLegacyRecords`, consultar `GetLegacyActionID` e verificar o mesmo histórico por `GetActionDetails`, `ListCommandOutputs` e `ListRuntimeEvents`.
4. Migrar o código consumidor do pacote `legacy` para a API do novo Engine. Manter leitura de transição até conferência de dados reais e dos direitos de acesso de cada clone.
5. Não apagar dados antigos nem alterar GitMaster sem autorização própria. O caminho `legacy.NewStore` permanece por compatibilidade.

## Limitações

A biblioteca não gerencia branches GitMaster, commits ou pushes. O InsulaVM `ExecVM` fornece console textual; não há garantia de captura binária original. Testes KVM/Libvirt reais exigem infraestrutura e autorização adicionais.

## Referências

[API](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Persistência](../persistencia/PERS-001-registro-execucoes.md) · [Separação arquitetural](../arquitetura/ARQ-002-separacao-gitmaster.md)
Para compilar e testar a nova disposição do código sem alterar o import path, consulte [GUIA-010](GUIA-010-build-testes.md).
