<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# PERS-003 — Perfis de VM: InsulaVM como fonte de verdade

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** a tabela `vm_profiles` permanece no banco GitMaster por compatibilidade histórica, mas o **worker e a CLI manual não a utilizam**. O catálogo de perfis executáveis pertence ao InsulaVM, que mantém `profile.yaml` + `base.qcow2` em `storage.profiles`.

## Cadastro ativo

- `listar_perfis_vm` (socket administrativo) chama `InsulaVM.Engine.ListProfiles` e `GetProfile`.
- `salvar_perfil_vm` chama `InsulaVM.Engine.RegisterProfile` para registrar um **novo** perfil a partir de QCOW2 instalado, previamente aprovado em `vm_images_dir`. Não sobrescreve um perfil já existente.
- `listar_imagens_vm` continua listando arquivos locais de **origem**, não perfis ativos, e não inicia VM. Aceita ISO/QCOW2/IMG na listagem, mas o registro para InsulaVM exige `.qcow2`.
- Para uso no workflow, `vm_profile: ci-go` referencia exatamente o nome do perfil InsulaVM. CPU/RAM/disco e sistema operacional são resolvidos lá.

## Legado

A tabela `vm_profiles` mantém `name` (PK), `image`, `vcpus`, `memory_mib`, `disk_gib`, `enabled`, `created_at`, `updated_at`. Seus métodos internos `registry.SaveVMProfile/VMProfile/ListVMProfiles` existem por compatibilidade de dados, mas **não são chamados pelos handlers de perfis novos nem pelo runner InsulaVM**. Não criar perfis somente nessa tabela; eles não seriam reconhecidos.

Migração operacional: registrar a imagem QCOW2 antiga como perfil no InsulaVM (ou preparar VM e `FreezeVM`), conferir `listar_perfis_vm`, e então usar o nome no workflow. Essa mudança não apaga os registros antigos sem autorização.

## Vínculo com action

`action_runs.vm_profile` persiste o nome do perfil; `action_runs.insula_vm_id` recebe o UUID da instância retornado pelo InsulaVM. O `InsulaVM.StopVM` cuida do ciclo de vida. O estado de cada command/job permanece no SQLite GitMaster.

**Scripts de instalação:** `profile.yaml` não armazena um campo `bootstrap_script`, mas `insula_startup_scripts` no JSON administrativo GitMaster associa um arquivo de script confiável a cada perfil. O caminho é entregue ao `InsulaVM.StartVMWithOptions(StartupScriptPath)` **na criação da VM**; o InsulaVM executa o script via Cloud-Init antes do upload do TAR.GZ. O `DeployToVM(Extract:true)` recebe o material e extrai nativamente. Não há cópia imutável da configuração do perfil no histórico da action; para auditoria reprodutível futura, registrar versão/digest da base QCOW2.

**Referências:** [CONTR-018](../contratos/sockets/CONTR-018-perfis-vm.md), [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md), [GUIA-006](../guias/GUIA-006-configurar-insulavm.md).
