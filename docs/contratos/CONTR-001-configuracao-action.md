<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-001 — Manifesto local `action-v1`

**Estado:** parser estrito implementado em `manifest.go`. `RunFile`/`RunYAML` agora utilizam o mesmo executor durável de `SubmitYAML`. Testes reais KVM/Libvirt não executados.

## Exemplo

```yaml
schema_version: 1
vm:
  profile: ci-go
action:
  name: verificar
  imports:
    - source: /srv/snapshots/material.tar.gz
  jobs:
    - name: testes
      commands:
        - go test ./...
      allow_failure: false
logging:
  capture_stdin: false
```

## Regras e limites

| Campo | Regra implementada |
| --- | --- |
| `schema_version` | Inteiro obrigatório igual a `1`. |
| `vm.profile` | Nome não vazio de perfil InsulaVM existente. |
| `vm.memory_mib`, `vm.vcpus` | Apenas zero/omissão; overrides ainda não são expostos pelo adaptador Action. |
| `action.name` | String não vazia. |
| `action.jobs[]` | De 1 a 128 Jobs; nomes únicos; comandos em sequência. |
| `action.jobs[].commands[]` | De 1 a 256 scripts não vazios por Job, sem NUL. |
| `action.jobs[].allow_failure` | Tolera somente erro remoto identificado, nunca falha de infraestrutura. |
| `action.imports[].source` | No fluxo unificado: zero ou **um** arquivo TAR.GZ regular, até 100 MiB comprimidos; estrutura validada antes do enqueue. |
| `logging.capture_stdin` | Somente `false`/omissão; transporte atual não implementa stdin nativo. |

O documento tem um único manifesto; campos desconhecidos não são aceitos. O limite operacional do YAML é 1 MiB. `RunFile` aceita import relativo à pasta do manifesto e normaliza para caminho absoluto; `RunYAML` e `SubmitYAML(action-v1)` exigem import absoluto. Múltiplos imports retornam erro explícito, pois uma Action trabalha com um único snapshot verificável.

O formato `action-v1` não possui `needs` nem triggers. Não há conversão silenciosa do formato GitMaster: informe `FormatGitMasterV1` para os workflows de [CONTR-003](CONTR-003-gitmaster-workflow.md).

## Segurança e retorno

Scripts são enviados ao guest pelo InsulaVM, nunca ao host. A configuração da VM e os scripts administrativos de bootstrap vêm de `Options`, não do YAML. O conteúdo do TAR.GZ precisa ser autorizado pelo consumidor e passa pelos limites e filtros documentados em CONTR-003.

`RunYAML`/`RunFile` aguardam estado terminal e retornam `(*Action, error)`: quando a Action falha, retornam objeto persistido e erro. A API assíncrona retorna Action `pending` e não bloqueia a execução do consumidor.

## Referências

[Interface Go](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Fluxo](../fluxos/FLUX-001-execucao-action.md) · [Segurança TAR.GZ](CONTR-003-gitmaster-workflow.md)