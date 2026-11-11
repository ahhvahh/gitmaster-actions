<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-002 — Separação GitMaster / GitMaster Actions

**Estado:** fronteira da nova biblioteca implementada. O consumidor GitMaster ainda deve atualizar a integração para abandonar o pacote `legacy`; este documento não afirma que essa alteração foi aplicada.

## Responsabilidades

| Projeto | Responsável por | Não deve fazer |
| --- | --- | --- |
| GitMaster | GitHub, contas, clones, branches, commit/push, eventos, leitura do YAML em SHA e TAR.GZ imutável | Criar VM, executar SSH ou escrever em `actions.db` |
| GitMaster Actions | Validar YAML recebido, SQLite próprio, fila/worker, dependências, logs, VM e cleanup via InsulaVM | Acessar GitHub, clonar, commitar ou publicar |
| InsulaVM | Perfis, imagens, QEMU/Libvirt, Cloud-Init, SSH, transferência e execução remota | Interpretar workflows GitMaster |

O InsulaVM é importado apenas pela biblioteca GitMaster Actions. GitMaster permanece responsável por autorização e pelo gate de push sobre resultados do **mesmo SHA e configuração**.

## Migração do consumidor

O pacote `legacy` foi mantido temporariamente para compatibilidade; o GitMaster em sua integração antiga ainda utiliza `legacy.NewStore` com SQLite externo. Para adotar a nova fronteira, o GitMaster deve:

1. Instanciar `Engine` com arquivo `actions.db` próprio, separado do banco de clones e eventos.
2. Selecionar YAML da árvore Git do commit e invocar `SubmitYAML` com origem completa e TAR.GZ autorizado.
3. Usar `WaitAction`/`GetActionDetails`/logs, sem manipular tabelas da Action.
4. Executar `ImportLegacyHistory` em um backup do SQLite anterior, comparar resultados com `ListLegacyRecords` e `GetLegacyActionID`, e só então fazer dual-read/cutover.
5. Não remover tabelas ou referências anteriores sem validação de migração e autorização do mantenedor.

**Não há alteração automática do GitMaster nesta biblioteca.** O histórico bruto é mantido e o pacote legado continua disponível até a substituição explícita no consumidor.

## Referências

[ARQ-001](ARQ-001-executor-actions.md) · [GUIA-008](../guias/GUIA-008-integracao-gitmaster.md) · [INTF-GO-001](../interfaces/go/INTF-GO-001-biblioteca-publica.md)