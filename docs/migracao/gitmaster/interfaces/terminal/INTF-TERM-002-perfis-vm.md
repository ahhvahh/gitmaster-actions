<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-002 — Console de perfis de VM

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** comando e menu implementados no código Go; testes de instalação em Debian e UI real pendentes.

## Comandos

```sh
gitmaster vm profiles
# ou escolher "Cadastrar/configurar perfis de VM" no menu:
gitmaster ui
```

As opções globais, quando necessárias, precedem o subcomando: `gitmaster --admin-socket /run/a-bioma/gitmaster/admin.sock vm profiles`.

O programa lê as escolhas por `stdin` e apresenta resultados no `stdout`; falhas de conexão e validação são mostradas no terminal. É preciso ter TTY interativo e pertencer ao grupo do socket administrativo. Na tela, `0` retorna; para salvar, informe `s`. O comando `vm profiles` não cria VM nem instala imagens.

## Fluxo de uso

1. Administrador copia a ISO/QCOW2/IMG para o diretório de imagens configurado em `vm_images_dir`. A criação e as permissões do diretório pertencem à instalação Linux, não ao tool MCP.
2. Operador executa `gitmaster vm profiles` ou seleciona o item do menu principal.
3. Lista imagens e perfis; escolhe cadastrar/editar e seleciona a imagem numerada.
4. Preenche nome, CPUs, RAM MiB e disco GiB; confirma com `s`.
5. Consulta novamente a lista para ver o perfil persistido no SQLite.

O console envia ao `admin.sock` apenas nome do arquivo selecionado, nome do perfil e recursos solicitados. O uso em workflows futuros será pela chave `vm_profile`, que identifica o perfil e não um caminho arbitrário do host.

## Contratos

| Operação | Contrato | Campos exibidos |
| --- | --- | --- |
| Listar imagens | [CONTR-018](../../contratos/sockets/CONTR-018-perfis-vm.md) | `images[]`, `images_dir` |
| Listar perfis | [CONTR-018](../../contratos/sockets/CONTR-018-perfis-vm.md) | `profiles[]` |
| Salvar perfil | [CONTR-018](../../contratos/sockets/CONTR-018-perfis-vm.md) | `saved`, `profile`, `execution_id` |

**Tela:** [INTF-TELA-002](../telas/INTF-TELA-002-perfis-vm.md). **Execução futura:** [FLUX-005](../../fluxos/FLUX-005-executar-workflow-vm.md).
