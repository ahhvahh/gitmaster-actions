<!-- A-BIOMA-DOC-STANDARD -->

# GUIA-008 — Integrar GitMaster com GitMaster Actions

## Pré-requisitos

- Go compatível com `go.mod` do GitMaster Actions (módulo declara Go 1.27).
- InsulaVM com configuração local, profile QCOW2 e guest.destination absoluto, não raiz.
- Caminho gravável para `actions.db` **fora do SQLite do GitMaster**.
- TAR.GZ produzido a partir do SHA do commit que originou o evento, previamente autorizado.

## Passos

1. Fixe a dependência pelo commit da branch `main` que passou pela validação de sua instalação, não por uma branch mutável:

   ```sh
   go get github.com/ahhvahh/gitmaster-actions@COMMIT_SHA_VALIDADO
   ```

2. Crie o `Engine` uma vez no processo de serviço. Informe `Options.DatabasePath`, `Options.InsulaConfigPath` e, opcionalmente, `Workers`, `StartupScripts` e `ImagesDir`.
3. Para cada commit/push, leia o YAML de `.gitmaster/workflow/*.yml|*.yaml` da árvore do SHA, **não** da working tree. Envie `SubmitRequest.Format=FormatGitMasterV1`, `YAML`, `ActionOrigin` e `ArchivePath` apontando para o snapshot TAR.GZ.
4. Persista o `Action.ID` retornado pelo enqueue e consulte `WaitAction`, `GetActionDetails`, `ListCommandLogs`, `ListCommandOutputs` e `ListRuntimeEvents`. Enqueue não equivale a sucesso.
5. Na inicialização do serviço, chame `ReconcileInterrupted` quando as permissões do runtime estiverem disponíveis. Nunca encerre VMs não vinculadas a Actions anteriores.
6. Para importar histórico, execute `ImportLegacyHistory` apontando ao SQLite antigo, mantenha o arquivo original como backup e verifique cada tabela com `ListLegacyRecords` antes de planejar a substituição do pacote `legacy`.

## Restrições

Não importar InsulaVM diretamente no GitMaster. Não executar comandos de YAML no host. A CLI legada `RunFile` continua síncrona e usa `action-v1`; o serviço deve usar `SubmitYAML` e `gitmaster-workflow-v1`. Testes reais de VMs precisam de autorização e host com KVM/Libvirt; o funcionamento do mock não equivale a essa validação.

## Referências

[Contrato](../contratos/CONTR-003-gitmaster-workflow.md) · [API](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [Persistência](../persistencia/PERS-001-registro-execucoes.md)