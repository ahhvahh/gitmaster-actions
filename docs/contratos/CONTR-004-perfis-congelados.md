<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-004 — Catálogo administrativo de perfis QCOW2 congelados

## Propósito

Separar as **imagens confiáveis do servidor** dos comandos submetidos por repositórios Git. O catálogo YAML fica no host e é informado em `Options.ProfilesConfigPath` ou `--profiles-config`. Não é importado do ZIP nem de um YAML de workflow.

## Exemplo de configuração

```yaml
profiles:
  - name: ci-go
    image_path: /var/lib/insulavm/frozen/debian13-ci.qcow2
    memory_mib: 2048
    vcpus: 2
    workspace: /tmp/workspace
  - name: ci-light
    image_path: /var/lib/insulavm/frozen/debian13-light.qcow2
    memory_mib: 1024
    vcpus: 1
    workspace: /tmp/workspace
```

Referência executável de parsing: [exemplo](../../examples/insula/frozen-profiles.yaml).

## Campos e validação

| Campo | Regra |
| --- | --- |
| `profiles` | Lista de 1 a 64 perfis; nomes únicos. |
| `name` | Identificador do perfil selecionável por `vm_profile` no workflow. |
| `image_path` | Caminho absoluto para um arquivo regular `.qcow2`, sem link simbólico; valida cabeçalho QCOW2. |
| `memory_mib` | RAM entre 128 e 1.048.576 MiB. |
| `vcpus` | CPU de 1 a 256. |
| `workspace` | Caminho absoluto normalizado dentro da VM, distinto de `/`; recomenda-se `/tmp/workspace`. |

O parser é estrito: não admite chaves desconhecidas, múltiplos documentos ou YAML maior que 1 MiB. O QCOW2 precisa ser previamente congelado pelo InsulaVM e conter o sistema operacional preparado, SSH gerenciado e `unzip`.

## Ciclo de vida

1. O executor lê o catálogo confiável no servidor e valida o perfil escolhido pelo workflow.
2. Caso não esteja registrado, chama `InsulaVM.RegisterProfile`, que **copia** a imagem QCOW2 para um perfil gerenciado. O arquivo de origem não é modificado.
3. Perfis já existentes são conferidos quanto à CPU, memória e workspace; divergências exigem novo nome ou gerenciamento explícito do perfil no InsulaVM. A atualização do conteúdo de um QCOW2 de origem **não altera automaticamente** o perfil já registrado.
4. Cada Action inicia uma instância descartável com overlay independente; a cópia base congelada não é modificada.
5. A VM recebe o ZIP autorizado no workspace, extrai os arquivos e executa os comandos em sequência; ao terminar, o executor tenta desligá-la mesmo em caso de falha.

## Dependência

Contratos verificados contra `InsulaVM/main` no commit `1ced9ce8b5b8cdd4b52c903e7949dfd8c1fb3abf`: `RegisterProfile`, `GetProfile`, `ResolveVMWorkspace`, `StartVMWithOptions`, `ImportToVM`, `ExecVM`, `StopVM`.

**Compatibilidade de boot:** o executor atual importa arquivos e executa comandos por SSH gerenciado. Perfis `boot_mode: direct` provenientes de instalações ISO que só possuem QGA não são compatíveis com esse fluxo e são rejeitados na seleção. Para utilizá-los, é preciso implementar transporte e execução por QGA em outra evolução ou preparar uma imagem com Cloud-Init/SSH.
