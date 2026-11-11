<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-006 — Commit → workflow → InsulaVM → comandos e logs

> **Cópia histórica do GitMaster:** o código operacional e a persistência de Actions foram transferidos ao GitMaster Actions. Referências a classes e caminhos do GitMaster nesta cópia documentam o legado, não uma implementação duplicada.


**Estado:** fluxo orquestrado em Go na `develop`; validação com VM real ainda pendente. **Uma action corresponde a um workflow YAML e utiliza uma VM descartável.**

**Filtro por branch:** após validar o YAML do SHA, o orquestrador avalia `triggers[].branches` e cada `jobs.<nome>.branches.include/exclude`. Um job excluído e seus dependentes ficam `skipped` (sem executar os respectivos steps), com o motivo guardado em `action_jobs.skip_reason`. Dependências por falha de execução seguem marcando a action como falha. Se nenhum job for elegível, registra apenas o evento e não cria action/VM.

**Publicação:** `public` lê a configuração do HEAD, aguarda a conclusão das actions de validação `commit` e `push` para esse SHA e somente então transmite a branch. A tentativa de push fica como `validating` e só se torna `published` depois da transmissão; falha não publica. Veja [CONTR-010](../contratos/sockets/CONTR-010-publicar.md).
## Sequência após solicitação de commit

```mermaid
sequenceDiagram
    autonumber
    actor U as Usuário / LLM
    participant MCP as GitMaster MCP
    participant DB as SQLite
    participant Q as Worker GitMaster
    participant G as Git local
    participant I as Biblioteca InsulaVM
    participant VM as VM descartável
    U->>MCP: aplicar_alteracoes(clone_id, files/bundle)
    MCP->>DB: Registrar batch_id
    U->>MCP: criar_commit(clone_id, batch_id, expected_head_sha, title+description)
    MCP->>G: git commit; obter SHA
    MCP->>DB: action_events(event_type=commit)
    MCP->>G: git ls-tree / cat-file SHA .gitmaster/workflow
    MCP->>DB: action_runs + action_jobs + action_job_commands (queued)
    MCP-->>U: commit_sha + event_id + action_run_ids[] + event_status=queued
    Q->>DB: Listar e reivindicar action queued
    Q->>G: git archive --format=tar SHA + gzip
    Q->>I: StartVMWithOptions(profile, StartupScriptPath)
    I->>VM: Overlay QCOW2 + Cloud-Init (script de instalação como root)
    I->>VM: Esperar instalação terminar antes do deploy
    I-->>Q: insula_vm_id
    Q->>DB: Salvar insula_vm_id e eventos
    Q->>I: DeployToVM(material.tar.gz, deploy.sh, Extract=true)
    I->>VM: Importar e extrair TAR.GZ nativamente em guest.destination
    I->>VM: Executar deploy.sh de verificação após a extração
    VM-->>I: logs da extração e retorno
    I-->>Q: resultado e progresso
    Q->>DB: Registrar eventos de implantação
    loop Para cada job dependente elegível
        loop Para cada comando/step em ordem
            Q->>DB: Marcar command_id running
            Q->>I: ExecVM(VMID, Workspace, Command, Output)
            I->>VM: Comando via SSH
            VM-->>I: stdout, stderr e exit
            I-->>Q: Mensagens e erro/código de saída
            Q->>DB: action_command_responses (sequence)
            Q->>DB: command status, exit_code
        end
        Q->>DB: Atualizar status e exit_code do job
    end
    Q->>DB: Finalizar action (succeeded ou failed)
    Q->>I: StopVM(insula_vm_id)
    I->>VM: Encerrar e descartar overlay
    I-->>Q: Confirmação de limpeza
    Q->>DB: Registrar fim da VM e falhas de limpeza
    U->>MCP: listar_actions(clone_id,status?)
    MCP-->>U: action, VM, runtime_events, jobs, comandos
    U->>MCP: listar_respostas_comando(clone_id,command_id)
    MCP-->>U: stdout/stderr/erros em ordem e paginados
```

## Passo a passo operacional

1. **Alteração e commit:** a LLM aplica o lote e pede `criar_commit`. O GitMaster confirma o SHA local. O formato aceita `message` legado ou o par `title`/`description`; ainda exige `batch_id`.
2. **Descoberta:** somente arquivos válidos `.gitmaster/workflow/*.yml/.yaml` presentes **na árvore do novo SHA** são considerados. Um `trigger` `event:commit` com branch correspondente produz uma action por YAML.
3. **Resposta imediata:** `criar_commit` retorna `event_id`, `action_detected`, `event_status` e `action_run_ids[]` sem iniciar/testar na mesma chamada. `queued` significa agendamento.
4. **Worker:** com `insula_config_path` configurado no servidor, o worker lê a action pendente e marca `running` **antes** de iniciar a VM. Sem essa configuração, a fila fica pendente.
5. **Pacote:** o GitMaster produz `material.tar.gz` via `git archive --format=tar SHA` + GZIP no host, sem `.git` nem alterações não commitadas; rejeita symlinks/submódulos e limita tamanho.
6. **Criação e instalação:** o GitMaster resolve o script administrativo em `insula_startup_scripts[vm_profile]` e o envia no argumento **`StartupScriptPath` de `StartVMWithOptions`**. O InsulaVM cria o overlay, insere o script no Cloud-Init, executa a instalação como root e espera confirmação **antes** de devolver a VM pronta.
7. **Entrega:** após a VM ficar pronta, o GitMaster envia `material.tar.gz` e `deploy.sh` por `DeployToVM(Extract:true)`. O próprio InsulaVM extrai o TAR.GZ **nativamente em `guest.destination`** e depois executa `deploy.sh` apenas para verificação. O script de instalação já foi executado no boot.
8. **Comandos:** cada job segue `needs` e cada step é executado **individualmente** pelo InsulaVM `ExecVM`, com seu próprio `command_id`. Não criar nova VM por step ou por job.
9. **Registros:** salvar `stdout`, `stderr`, erros e evento de término na sequência recebida. Um retorno não zero marca comando e job `failed` e bloqueia steps seguintes e jobs dependentes. Jobs independentes podem continuar. `stderr` sozinho não caracteriza falha.
10. **Encerramento:** o InsulaVM recebe `StopVM`; o GitMaster registra sucesso/erro do cleanup. Se não conseguir confirmar limpeza, preserva o ID para diagnóstico; a action não deve ser informada como íntegra.
11. **Diagnóstico:** a LLM chama `listar_actions(clone_id,status:"failed")`, encontra o `job_name` com erro, compara `step_order`, escolhe o `command_id` falho ou anterior e lê `listar_respostas_comando`.

## Persistência

| Evento / estado | Tabela |
| --- | --- |
| Commit/Push disparador | `action_events` |
| Workflow e UUID da VM | `action_runs` |
| Inicialização, importação, término, falhas pré-job | `action_runtime_messages` |
| Jobs e dependências | `action_jobs` |
| Step individual, status, código de saída | `action_job_commands` |
| Respostas sequenciais da VM | `action_command_responses` |
| Diagnóstico temporário da ferramenta MCP | `tool_executions`, `tool_execution_messages` |

**Limitações:** a imagem precisa de `bash`, `tar`, `gzip`, Cloud-Init e SSH. O script de instalação é confiável, mantido pelo administrador e passado no boot; o formato `profile.yaml` ainda não armazena diretamente esse script. Os SDKs podem vir da imagem-base ou ser instalados pelo Cloud-Init. O código precisa passar por build/CI e executar ao menos um teste real de VM. A leitura dos textos de console do InsulaVM ainda depende de prefixos de stream, pois a API pública não fornece callback estruturado de stdout/stderr.

**Referências:** [ARQ-006](../arquitetura/ARQ-006-adaptador-insulavm.md), [GUIA-006](../guias/GUIA-006-configurar-insulavm.md), [CONTR-021](../contratos/sockets/CONTR-021-eventos-commit-push.md), [PERS-002](../persistencia/PERS-002-execucao-actions.md).
