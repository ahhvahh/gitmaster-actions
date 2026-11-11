<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-002 — Limite GitMaster / GitMaster Actions / InsulaVM

**Estado:** migração em andamento. Este documento distingue funções implementadas das funcionalidades ainda pertencentes ao GitMaster.

| Sistema | Responsabilidade |
| --- | --- |
| GitMaster | Selecionar conta/repositório/branch, criar clone, editar, commitar, ler YAML do SHA, organizar eventos commit/push e bloquear publicação enquanto validação estiver pendente |
| GitMaster Actions | Interpretar manifesto de Action, manter seu SQLite próprio, expor consultas Action/Job/Command/log, administrar perfis e iniciar/enviar/encerrar VMs por InsulaVM |
| InsulaVM | Controle do hipervisor, SSH, Cloud-Init, QCOW2, redes, guest e streaming |
 
## Contratos implementados neste repositório

- `New(Options)`, `RunYAML`, `RunFile`, `GetAction`, `ListActions`, `GetJob`, `GetCommand`, `CancelAction` e navegação por `Jobs`, `Commands`, `Logs`, `Outputs`.
- `NewRuntime` fornece a ponte de VM usada também pelo GitMaster legado: `Profile`, `Start`, `Deploy`, `Command`, `Stop` e `Workspace`.
- `ListVMImages`, `ListVMProfiles`, `RegisterVMProfile` e `ConfigurationFingerprint` concentram operações de administrador/identidade dos perfis do InsulaVM.
- CLI `gitmaster-actions` para executar YAML, listar actions/jobs/comandos/logs, consultar imagens e registrar perfis QCOW2 preparados.

## Limites e compatibilidade

O YAML `action.jobs[].commands[]` do GitMaster Actions **não** substitui silenciosamente o YAML `.gitmaster/workflow/` do GitMaster, que tem `triggers`, `needs`, filtro de branch e steps. Essas semânticas pertencem ao produtor GitMaster até existir adaptador sem perda.

O InsulaVM atual usa uma saída de console textual decorada, não fornece streams binários nativos separados, e requer script de deploy validado. O Engine gera o script de pós-importação em arquivo temporário confiável. `StartupScripts` somente pode ser configurado pelo administrador; não é aceito via YAML.

O runtime isolado não fornece rede por perfil, cópia não destrutiva de VM existente ou recuperação automática após morte do processo. Testes Go e execução KVM real ainda não foram realizados.

**Regra de segurança:** o GitMaster **não** deve chamar diretamente APIs de Libvirt/QEMU, SSH ou `insulavm.New`; toda chamada específica do InsulaVM deve ficar nesta biblioteca.
