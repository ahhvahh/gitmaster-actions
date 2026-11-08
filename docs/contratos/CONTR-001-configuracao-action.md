<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-001 — Contrato proposto de configuração de Action

**Estado:** proposto; nenhum parser implementado neste repositório. O arquivo de Action não é o `config.yaml` global do InsulaVM. A biblioteca aceita tanto `RunYAML(ctx, []byte)` quanto `RunFile(ctx, path)`; o segundo delega ao mesmo parser. Caminhos relativos de importação são resolvidos a partir da pasta do arquivo somente em `RunFile`; para `RunYAML`, exigir caminhos absolutos.

## Objetivo

Receber YAML em memória ou caminho de arquivo local YAML e definir o trabalho que deve rodar em **uma VM**. A origem da VM deve ser um perfil InsulaVM **ou** uma ISO local, nunca ambas. Até o InsulaVM suportar instalação por ISO, esse segundo modo deve falhar com erro explícito antes de reservar uma VM.

## Exemplo de manifesto proposto (`action.yaml`)

```yaml
schema_version: 1
vm:
  profile: debian13-go
  # iso: /srv/isos/debian-13.iso  # alternativa futura, NÃO funcional hoje
  memory_mib: 2048
  vcpus: 2

action:
  name: testes
  repository: projeto-exemplo
  imports:
    - source: /srv/bundles/projeto.tar.gz
  pipelines:
    - name: validar
      jobs:
        - name: compilar
          commands:
            - "go version"
            - "go build ./..."
          allow_failure: false
        - name: testes
          commands:
            - "go test ./..."
          allow_failure: false

logging:
  capture_stdin: false
```

Este exemplo é **ilustrativo**: seu layout é um contrato do GitMaster Actions a desenvolver, não um manifesto `insula.yaml` aceito diretamente pelo parser InsulaVM. O adaptador deve traduzir a estrutura validada para operações públicas da biblioteca, sem duplicar seus mecanismos de VM ou SSH.

## Campos

| Campo | Tipo | Regra |
| --- | --- | --- |
| `schema_version` | inteiro | Obrigatório; nesta proposta, `1` |
| `vm.profile` | string | Nome de perfil QCOW2 registrado no InsulaVM; exclusivo de `vm.iso` |
| `vm.iso` | string | Caminho absoluto de ISO instaladora; exclusivo de `vm.profile`; modo ainda bloqueado |
| `vm.memory_mib` | inteiro | Opcional; `0`/omissão mantém padrão do perfil; não negativo |
| `vm.vcpus` | inteiro | Opcional; `0`/omissão mantém padrão do perfil; não negativo |
| `action.name` | string | Nome lógico obrigatório da Action |
| `action.repository` | string | Referência textual obrigatória do projeto executado |
| `action.imports[].source` | string | Caminho local de arquivo/diretório confiável; opcional; extração ocorre segundo a API InsulaVM vigente |
| `action.pipelines[].name` | string | Nome de pipeline; obrigatório, sem duplicatas no mesmo manifesto |
| `action.pipelines[].jobs[].name` | string | Nome de job; obrigatório, único na Action |
| `action.pipelines[].jobs[].commands[]` | lista de strings | Comandos shell não vazios; ao menos um comando por job |
| `action.pipelines[].jobs[].allow_failure` | booleano | Opcional, padrão `false`; tolera **exit code remoto não zero** daquele job, não falhas de infraestrutura |
| `logging.capture_stdin` | booleano | Opcional; padrão `false`, pois stdin pode conter dados sensíveis |

**Entradas administrativas separadas:** `InsulaConfigPath` e `DatabasePath` são definidos pelo projeto consumidor ao chamar `New(Options)`, nunca pelo manifesto.

## Validação proposta

1. YAML legível; apenas um documento; rejeitar chaves desconhecidas e versões não reconhecidas.
2. Exatamente uma origem dentre `vm.profile`, `vm.iso` (definida no YAML). Mesmo pela CLI de testes, não existem flags para sobrescrever a origem da VM; a fonte é o próprio YAML.
3. Validar nomes e caminhos, evitar traversal, verificar permissões locais e não aceitar interpolação automática de segredos/variáveis de ambiente.
4. Se `vm.iso` for selecionado antes de `CreateProfileFromISO` existir, retornar `ISO_NOT_SUPPORTED`, sem criar Action operacional ou iniciar VM. Pode registrar uma tentativa inválida somente se política futura decidir rastrear rejeições.
5. Ao menos uma pipeline e um job; comandos executados na ordem declarada. Não interpretar comandos do YAML como argumentos locais do shell hospedeiro.
6. Por padrão, cada comando abre sessão remota independente; alterações de `cd` e `export` não persistem entre comandos. O diretório inicial padrão é `guest.destination` do InsulaVM.

## Resultado lógico proposto

| Campo | Descrição |
| --- | --- |
| `action_id` | UUIDv7 da Action, gerado pelo GitMaster Actions |
| `vm_id` | UUIDv7 gerado pelo InsulaVM, se a VM tiver sido criada |
| `status` | `pending`, `starting`, `running`, `succeeded`, `failed` ou `cancelled` |
| `started_at`, `finished_at` | Instantes em UTC; término nulo enquanto não finalizado |
| `job_count`, `failed_job_count` | Totais derivados dos registros de jobs |

Um processo remoto terminando com exit code não zero é diferente de uma falha SSH/Libvirt. Consulte as definições de status em [PERS-001](../persistencia/PERS-001-registro-execucoes.md) e o processamento em [FLUX-001](../fluxos/FLUX-001-execucao-action.md).

## Acesso e erros

- Apenas um projeto consumidor autorizado pode fornecer YAML e caminhos de importação.
- O conteúdo dos comandos será executado no guest. Arquivos de origem, configuração, stdout e stderr podem conter segredos; proteger o banco SQLite e aplicar política de retenção antes de usar em produção.
- Erros previstos: arquivo ausente/inacessível, YAML inválido, origem ambígua, perfil inexistente, ISO não suportada, configuração InsulaVM inválida e falha de inicialização.

## Referências

- [INTF-GO-001](../interfaces/go/INTF-GO-001-biblioteca-publica.md)
- [ARQ-001](../arquitetura/ARQ-001-executor-actions.md)