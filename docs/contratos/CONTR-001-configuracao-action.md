<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-001 — Contrato proposto de configuração de Action

**Estado:** parser YAML estrito implementado em `manifest.go`, ainda sem validação de build/KVM. O arquivo de Action não é o `config.yaml` global do InsulaVM. A biblioteca aceita tanto `RunYAML(ctx, []byte)` quanto `RunFile(ctx, path)`; o segundo delega ao mesmo parser. Caminhos relativos de importação são resolvidos a partir da pasta do arquivo somente em `RunFile`; para `RunYAML`, exigir caminhos absolutos.

## Objetivo

Uma **Action** é uma execução de **uma VM local clonada a partir de um perfil InsulaVM já preparado**, no mesmo host do processo da biblioteca/CLI. Só é permitido `vm.profile`; memória e vCPUs são parâmetros opcionais e, quando omitidos, são herdados do perfil existente. **O engine atual rejeita sobrescritas não zero** até integrar essa opção à inicialização da VM. Jobs são filhos diretos de `action`; não existe Pipeline.

## Exemplo proposto (`action.yaml`)

```yaml
schema_version: 1

vm:
  profile: debian13-go
  # memory_mib: 2048  # opcional: omissão mantém o valor do perfil
  # vcpus: 2          # opcional: omissão mantém o valor do perfil

action:
  name: build
  imports:
    - source: /srv/bundles/projeto.tar.gz
  jobs:
    - name: compilar
      commands:
        - "go version"
        - "go build ./..."
      allow_failure: false
    - name: testar
      commands:
        - "go test ./..."
      allow_failure: false

logging:
  capture_stdin: false
```

**Exemplo de entrada validada pelo parser; execução real depende de configuração InsulaVM disponível**: este contrato é específico do GitMaster Actions; o adaptador usa a biblioteca Go InsulaVM, não passa este YAML diretamente ao parser do InsulaVM.


## Campos

| Campo | Tipo | Regra |
| --- | --- | --- |
| `schema_version` | inteiro | Obrigatório; versão proposta `1` |
| `vm.profile` | string | Obrigatório; perfil QCOW2 previamente cadastrado no InsulaVM local |
| `vm.memory_mib` | inteiro | Opcional; omissão ou `0` usa RAM do perfil; valores positivos sobrescrevem apenas a instância |
| `vm.vcpus` | inteiro | Opcional; omissão ou `0` usa vCPUs do perfil; valores positivos sobrescrevem apenas a instância |
| `action.name` | string | Nome da Action, obrigatório |
| `action.imports[].source` | string | Origem de arquivo/diretório confiável, opcional |
| `action.jobs[].name` | string | Nome obrigatório e único dentro da Action |
| `action.jobs[].commands[]` | lista de strings | Um ou mais comandos shell não vazios, executados na VM |
| `action.jobs[].allow_failure` | booleano | Opcional, padrão `false`; tolera exit code remoto não zero, não falhas de infraestrutura |
| `logging.capture_stdin` | booleano | Opcional, padrão `false` para proteger dados sensíveis |

Configuração administrativa do banco (`DatabasePath`) e do InsulaVM (`InsulaConfigPath`) é informada ao `New(Options)` pelo consumidor, nunca por este YAML. O YAML não inclui repositório, pipeline, ISO ou identificadores internos.


## Validação proposta

1. Aceitar um único documento YAML e rejeitar campos desconhecidos e `schema_version` inválida.
2. Exigir perfil existente no InsulaVM **do mesmo host**; nenhuma outra origem de VM ou modo remoto é aceito.
3. Memória e vCPUs ausentes ou iguais a zero mantêm os recursos definidos no perfil; valores positivos valem só para a nova VM e não modificam o perfil.
4. Exigir `action.name`, pelo menos um `action.jobs[]`, nomes únicos e pelo menos um comando em cada Job.
5. Validar imports: `RunFile` resolve caminhos relativos a partir do diretório do arquivo; `RunYAML` exige caminhos absolutos.
6. Executar Jobs e comandos na ordem declarada, na **mesma VM**. Cada comando usa uma sessão SSH independente; `cd` e `export` não persistem entre comandos. Comandos nunca são executados no host.
7. Não interpolar segredos automaticamente nem aceitar caminhos de bootstrap administrativo fornecidos pelo manifesto.


## Resultado lógico proposto

`RunFile` e `RunYAML` retornam `*Action, error` conforme [INTF-GO-001](../interfaces/go/INTF-GO-001-biblioteca-publica.md). O `Action.ID` é UUIDv7; os campos `Status`, `StartedAt` e `FinishedAt` são persistidos. `VMID` é **transitório** e só existe no objeto retornado pela execução atual, quando disponível: não é gravado na tabela `actions`.

A execução não realiza recuperação ou retomada de VM após queda/reinício do processo ou servidor; o último status persistido pode estar desatualizado. Ver [FLUX-001](../fluxos/FLUX-001-execucao-action.md).


## Acesso e erros

- Apenas um projeto consumidor autorizado pode fornecer YAML e caminhos de importação.
- O conteúdo dos comandos será executado no guest. Arquivos de origem, configuração, stdout e stderr podem conter segredos; proteger o banco SQLite e aplicar política de retenção antes de usar em produção.
- Erros previstos: arquivo ausente/inacessível, YAML inválido, perfil inexistente, configuração InsulaVM inválida, falha de clonagem/inicialização ou falha de infraestrutura.

## Referências

- [INTF-GO-001](../interfaces/go/INTF-GO-001-biblioteca-publica.md)
- [ARQ-001](../arquitetura/ARQ-001-executor-actions.md)