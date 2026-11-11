<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-018 — Perfis InsulaVM via MCP administrativo

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** ferramentas do GitMaster usam a biblioteca **InsulaVM** para consultar e registrar perfis de VM; validação em VM real pendente. **Socket:** `admin.sock`. InsulaVM é a única fonte de verdade para `profile.yaml`, imagens QCOW2 e recursos.

## `listar_imagens_vm`

**Entrada:** `{}`. Lista arquivos **de origem** do diretório administrativo `vm_images_dir` do host GitMaster (`.qcow2`, `.iso`, `.img`). Não os registra no InsulaVM, não verifica boot e não inicia VMs. Ignora symlinks/subpastas.

Exemplo resumido:

```json
{"images":[{"name":"debian13-go.qcow2","format":"qcow2","size_bytes":1024}],"images_dir":"/var/lib/a-bioma/gitmaster/vm-images"}
```

Para `salvar_perfil_vm`, **apenas QCOW2 previamente instalado** é aceito.

## `listar_perfis_vm`

**Entrada:** `{}`. Chama `InsulaVM.Engine.ListProfiles/GetProfile` usando `insula_config_path` do servidor.

```json
{"managed_by":"InsulaVM","profiles":[{"name":"ci-go","os":"debian","version":"v001","image":"versions/v001/base.qcow2","vcpus":2,"memory_mib":4096,"disk_gib":20,"managed_by":"InsulaVM"}]}
```

Nomes são obtidos do catálogo InsulaVM; não do SQLite legado `vm_profiles`.

## `salvar_perfil_vm`

**Entrada:**

```json
{"name":"ci-go","os":"debian","image":"debian13-go.qcow2","vcpus":2,"memory_mib":4096,"disk_gib":20}
```

O campo opcional `os` identifica o sistema operacional no metadado do perfil, sem instalar um SO. O GitMaster valida o nome simples e a extensão `.qcow2`, confere o arquivo no diretório `vm_images_dir` e chama **`InsulaVM.Engine.RegisterProfile`**, que copia e registra a imagem na própria estrutura InsulaVM.

**Saída:** `{"profile":{...,"version":"v001","managed_by":"InsulaVM"},"saved":true,"vm_started":false,"execution_id":"<uuidv7>"}`.

**Mudança importante:** cadastro **cria um perfil novo**, **não** atualiza/sobrescreve perfil existente. Para nova configuração, escolher nome novo ou preparar VM no InsulaVM e usar `FreezeVM`. A operação não inicia VM.

O `profile.yaml` do InsulaVM não armazena `bootstrap_script`, mas o GitMaster pode indicar em `insula_startup_scripts` um script administrativo por perfil. O script é fornecido ao `StartVMWithOptions(StartupScriptPath)` **na criação da VM**, executado como root pelo Cloud-Init e confirmado antes do `DeployToVM(Extract:true)` com TAR.GZ. `vm_profile` no workflow seleciona o nome do perfil, nunca o script.

## Configuração e autorização

Sem o campo JSON `insula_config_path` (caminho absoluto de `config.yaml`), as chamadas de listagem/registro de perfis retornam erro `INSULA_NOT_CONFIGURED`. Operações administrativas exigem acesso ao `admin.sock`; o operador deve ter acesso autorizado aos arquivos de InsulaVM e do Libvirt. Não expor caminhos arbitrários de imagens ao socket operacional.

O GitMaster **não** gerencia mais recursos de CPU/RAM/disco durante a execução: esses valores pertencem ao perfil InsulaVM. A API e a estrutura de perfis estão em [PERS-003](../../persistencia/PERS-003-perfis-vm.md) e [ARQ-006](../../arquitetura/ARQ-006-adaptador-insulavm.md).

**Guias:** [GUIA-004](../../guias/GUIA-004-imagens-perfis-vm.md), [GUIA-006](../../guias/GUIA-006-configurar-insulavm.md).
