<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-003 — Executar uma action manual por InsulaVM

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** comando implementado em `src/go/cmd/gitmaster/action_run.go`, ainda não validado com uma VM real. Não é um tool MCP. O GitMaster não acessa o hipervisor nem SSH diretamente. `StartVMWithOptions` recebe `StartupScriptPath` do mapa administrativo por perfil e aguarda a execução do script no Cloud-Init antes do envio do TAR.GZ.

## Sintaxe

```bash
gitmaster action run \
  --file /projetos/workflow.yml \
  --local /projetos/meu-repo \
  --config /etc/a-bioma/gitmaster.json \
  --insula-config /etc/insulavm/config.yaml \
  --log-file ./execucao.log
```

Alternativa remota:

```bash
gitmaster action run --file ./workflow.yml \
  --repo OWNER/REPO --branch develop \
  --account-id 1 --insula-config /etc/insulavm/config.yaml
```

O `--account-id` é opcional, para repositório privado com conta cadastrada. Não enviar segredos na linha de comando.

| Flag | Função |
| --- | --- |
| `--file` | Workflow YAML, obrigatório |
| `--local` | Diretório local; exclusivo com `--repo` |
| `--repo` e `--branch` | Repositório remoto e branch |
| `--account-id` | Conta GitMaster para fonte privada remota |
| `--config` | JSON GitMaster (padrão `/etc/a-bioma/gitmaster.json`) |
| `--insula-config` | Arquivo InsulaVM `config.yaml` (sobrescreve `insula_config_path`) |
| `--log-file` | Log host; se omitido, `gitmaster-<action-id>.log` |

**Fluxo:** validar YAML → copiar fonte → gerar TAR.GZ → registrar action → biblioteca InsulaVM inicializa VM → `DeployToVM(TAR.GZ, deploy.sh, Extract:true)` → `ExecVM` uma vez por step → persistir stdout/stderr, códigos, erros → `StopVM`.

**Não utilizar flags antigas de rede/SSH**: são controladas pelo InsulaVM. Sem perfil e configuração InsulaVM válidos a action não inicia.

**Referências:** [GUIA-005](../../guias/GUIA-005-executar-action-manual.md), [GUIA-006](../../guias/GUIA-006-configurar-insulavm.md), [ARQ-006](../../arquitetura/ARQ-006-adaptador-insulavm.md).
