<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-005 — Execução de jobs em uma VM InsulaVM

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** novo executor no código `develop`, ainda sem validação em VM real. O GitMaster não provisiona ou acessa VMs com recursos próprios.

## Entrada

A action previamente enfileirada contém `action_run_id`, `clone_id`, `commit_sha`, `vm_profile`, jobs e steps. O worker `StartInsulaWorker` só processa actions de eventos Git (`event_id` não nulo); a CLI manual chama o mesmo adaptador para sua action própria.

## Fluxo executável

1. Reivindicar a action `queued` e registrar `running`.
2. Validar perfil por `InsulaVM.GetProfile`.
3. Gerar TAR.GZ da árvore Git no SHA do evento ou TAR.GZ do snapshot da execução manual.
4. `InsulaVM.StartVMWithOptions`: uma VM por action, com instalação administrativa pelo Cloud-Init, com perfil QCOW2 reutilizável; registrar `insula_vm_id`.
5. Gerar `deploy.sh` do GitMaster (verificação pós-extração TAR.GZ nativa em `guest.destination`).
6. `InsulaVM.DeployToVM(Extract:true)` recebe TAR.GZ e script, transfere e executa via seu próprio SSH.
7. Executar jobs elegíveis por `needs`. Para **cada step**, `InsulaVM.ExecVM` recebe um comando e diretório de trabalho relativo à raiz. Esperar resposta anterior antes do próximo comando; sem VM nova por job.
8. Persistir stdout, stderr, mensagens de sistema, código de saída e erro. Uma falha marca comando/job e pula os passos seguintes e jobs dependentes. Jobs independentes podem continuar.
9. Finalizar action e solicitar `InsulaVM.StopVM`. Erro de cleanup gera evento durável, conserva ID da VM e marca a action como falha.
10. Em reinício do daemon, ações `running` viram `interrupted` e o InsulaVM é solicitado a limpar seus IDs registrados; não retomar job interrompido silenciosamente.

## Limites

`DeployToVM` usa `Extract:true` para TAR.GZ e extrai nativamente no InsulaVM, sem `unzip`. O script de instalação é enviado **na criação da VM** via `StartupScriptPath` (Cloud-Init), não no pacote de código. A imagem do InsulaVM deve conter `bash`, `tar`, `gzip`, Cloud-Init e SSH; as dependências dos testes podem ser instaladas no primeiro boot pelo script administrativo; `guest.destination` é configurado no InsulaVM e não pode ser escolhido pelo YAML de repositório.

Os dados duráveis de VM e comandos estão no SQLite do GitMaster; a consulta é `listar_actions(clone_id)` e `listar_respostas_comando(clone_id,command_id)`. O `execution_id` de ferramentas MCP continua sendo histórico temporário distinto.

**Detalhes de ponta a ponta:** [FLUX-006](FLUX-006-commit-action-insulavm.md). **Biblioteca:** [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md). **Guia:** [GUIA-006](../guias/GUIA-006-configurar-insulavm.md).
