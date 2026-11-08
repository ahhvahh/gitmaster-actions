<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-001 — Arquitetura da biblioteca GitMaster Actions

**Estado:** proposto; nenhuma implementação neste repositório.

## Objetivo

Fornecer um **pacote Go importável** por outros projetos. Não executar como serviço nem daemon. A CLI opcional é um programa auxiliar de testes locais que chama a mesma biblioteca e não contém regras de orquestração ou SQL próprias. O projeto consumidor fornece YAML em memória ou caminho local; a biblioteca gerencia o SQLite, os UUIDs, o ciclo de Action e a VM por meio da API Go do InsulaVM.

## Componentes

| Componente | Responsabilidade |
| --- | --- |
| Projeto consumidor | Chamar a biblioteca, fornecer YAML ou caminho, controlar contexto |
| CLI auxiliar (`cmd/gitmaster-actions`) | Traduzir argumentos, arquivos/stdin e formatos de saída para chamadas da API pública, sem lógica de execução ou SQL |
| API Go GitMaster Actions | Métodos de execução e leitura, sem SQL exposto |
| Parser/validador | Ler e validar manifesto, origem da VM e caminhos |
| Orquestrador | Gerar UUIDs, registrar estados, executar jobs e comandos |
| Repositório SQLite interno | Criar esquema, migrar e persistir Action, Jobs, Comandos e Streams |
| Adaptador InsulaVM | Criar/encerrar VM, transferir arquivos e executar comandos usando API pública |
| InsulaVM | Gerenciar QEMU/KVM/Libvirt, Cloud-Init e SSH |

```mermaid
flowchart LR
    C[Projeto consumidor Go] --> A[API GitMaster Actions]
    T[CLI opcional de testes] --> A
    A --> P[Parser YAML]
    A --> O[Orquestrador]
    O --> D[(SQLite exclusivo)]
    O --> I[Biblioteca InsulaVM]
    I --> V[VM isolada]
    V -. Streams e resultados .-> O
```

## Invariantes

1. Cada Action, Job e Comando tem UUIDv7 próprio; `vm_id` é atribuído pelo InsulaVM.
2. Uma VM por Action; jobs e comandos executados sequencialmente no primeiro ciclo, com sessão remota nova por comando.
3. `actions.db` pertence ao GitMaster Actions; `insula.db` pertence ao InsulaVM. Não escrever diretamente no banco do InsulaVM.
4. A biblioteca é a única responsável por gravações e migrações de `actions.db`; consumidores, inclusive a CLI, usam métodos de leitura e execução. Consultas não dependem da inicialização de VM.
5. O YAML não pode escolher o caminho do banco nem o arquivo global InsulaVM; são opções administrativas de `New`.
6. Perfil QCOW2 é suportado pela biblioteca InsulaVM existente; ISO instaladora depende de `CreateProfileFromISO`, ainda pendente.
7. STDIN/STDOUT/STDERR separados e exit code estruturado dependem de `ExecuteCommand`, ainda pendente no InsulaVM. Não simular separação com saída combinada.
8. Comandos do YAML não executam no host; a biblioteca delega a execução ao InsulaVM. Scripts administrativos de bootstrap não são aceitos do YAML não confiável.
9. Cancelamento e erros não devem descartar logs parciais; encerrar a VM com contexto de cleanup independente.
10. A biblioteca não implementa protocolo GitHub Actions runner nem serviço de rede. A CLI só cria processos de curta duração para testes; não inicializa listener ou processo residente.

## Documentos canônicos

- [INTF-GO-001 — API pública](../interfaces/go/INTF-GO-001-biblioteca-publica.md)
- [INTF-TERM-001 — CLI auxiliar](../interfaces/terminal/INTF-TERM-001-cli-testes.md)
- [CONTR-001 — YAML](../contratos/CONTR-001-configuracao-action.md)
- [PERS-001 — Banco SQLite](../persistencia/PERS-001-registro-execucoes.md)
- [FLUX-001 — Processamento](../fluxos/FLUX-001-execucao-action.md)
- [API Go do InsulaVM](https://github.com/ahhvahh/InsulaVM/blob/main/docs/arquitetura/ARQ-004-biblioteca-go.md)