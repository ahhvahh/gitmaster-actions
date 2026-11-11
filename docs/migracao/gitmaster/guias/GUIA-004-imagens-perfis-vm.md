<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-004 — Registrar perfis reutilizáveis no InsulaVM

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** a administração do GitMaster consulta/registra perfis pela biblioteca InsulaVM. Perfil precisa de imagem QCOW2 com sistema instalado; ISO não é válida como perfil pronto.

## Passos

1. Preparar uma VM Linux com Cloud-Init, SSH, `tar`, `gzip`, `bash` e ferramentas dos testes. O InsulaVM permite criar perfil a partir de QCOW2 existente (`profile register`) ou congelar VM pronta (`vm freeze`).
2. Configurar `insula_config_path` no JSON do GitMaster para apontar ao `config.yaml` absoluto do InsulaVM. O perfil real fica em `storage.profiles` do InsulaVM.
3. Cadastrar pelo InsulaVM, ou usar `gitmaster --admin call salvar_perfil_vm` para **registrar um perfil novo** a partir de QCOW2 já colocado no diretório administrativo `vm_images_dir` do GitMaster.
4. Confirmar com `gitmaster --admin call listar_perfis_vm '{}'`, que agora consulta diretamente `Engine.ListProfiles/GetProfile`.
5. Referenciar o nome no YAML `.gitmaster/workflow/*.yml`, como `vm_profile: ci-go`. O GitMaster não administra domínios ou overlays.

## Exemplo administrativo

```bash
gitmaster --admin call listar_imagens_vm '{}'
gitmaster --admin call salvar_perfil_vm '{"name":"ci-go","os":"debian","image":"debian13-go.qcow2","vcpus":2,"memory_mib":4096,"disk_gib":20}'
gitmaster --admin call listar_perfis_vm '{}'
```

O InsulaVM copia a imagem para seu catálogo e impede sobrescrever perfil existente. A operação não inicia VM. O antigo cadastro SQLite `vm_profiles` do GitMaster não é usado pelo executor; antigos perfis devem ser cadastrados no InsulaVM.

**Script de inicialização:** `profile.yaml` ainda não guarda `bootstrap_script`, mas o GitMaster associa caminhos de scripts administrativos por perfil em `insula_startup_scripts` e fornece o arquivo ao `StartVMWithOptions(StartupScriptPath)`. O InsulaVM executa o script como root pelo Cloud-Init durante o primeiro boot, antes de aceitar o `DeployToVM(Extract:true)` com TAR.GZ. Os steps de testes continuam no YAML.

**Guias relacionados:** [GUIA-006](GUIA-006-configurar-insulavm.md), [CONTR-018](../contratos/sockets/CONTR-018-perfis-vm.md), [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md).
