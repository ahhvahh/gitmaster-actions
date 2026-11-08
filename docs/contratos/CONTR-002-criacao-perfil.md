<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-002 — Criar perfil reutilizável

**Estado:** contrato proposto, não implementado no GitMaster Actions. Fonte canônica desta operação administrativa. O **InsulaVM** continua responsável pelos perfis, imagens QCOW2, redes e VMs; GitMaster Actions apenas delega pelas APIs públicas, sem criar outro catálogo.

## Objetivo

Criar perfil nomeado com RAM/vCPUs padrão, permissão de internet e **uma única fonte**: imagem QCOW2 instalada ou VM InsulaVM existente. A biblioteca e o InsulaVM executam no mesmo host. As Actions continuam usando somente `vm.profile` no YAML.

Se o propósito for criar um ambiente com **MySQL**, ele deve ser instalado/configurado **dentro da imagem ou VM de origem** antes de gerar o perfil; MySQL por si só não é uma imagem de VM. ISO instaladora não é QCOW2 pronta e não é aceita por este contrato.

## Interface Go proposta

```go
type CreateProfileRequest struct {
    Name            string
    SourceQCOW2Path string // Caminho absoluto, ou...
    SourceVMID      string // ...identificador de VM; exclusivos.
    MemoryMiB       int
    VCPUs           int
    InternetAccess  *bool // Obrigatório; true = NAT, false = isolada.
}

type ProfileInfo struct {
    Name           string
    ImagePath      string
    MemoryMiB      int
    VCPUs          int
    InternetAccess bool
}

func (e *Engine) CreateProfile(
    ctx context.Context, req CreateProfileRequest,
) (*ProfileInfo, error)
```

Os tipos acima são **propostas**, não funções compiláveis. `*bool` distingue internet negada (`false`) de parâmetro ausente (`nil`).

## Entrada e validação

| Campo | Regra |
| --- | --- |
| `Name` | Obrigatório, válido, único no catálogo InsulaVM |
| `SourceQCOW2Path` | Alternativa A: caminho absoluto de arquivo QCOW2 com sistema instalado, Cloud-Init/SSH preparados |
| `SourceVMID` | Alternativa B: VM gerenciada pelo InsulaVM local, cuja origem deve ser preservada |
| `MemoryMiB` | RAM do novo perfil; 128 a 1048576 MiB, limites do validador atual do InsulaVM |
| `VCPUs` | 1 a 256, limites do validador atual do InsulaVM |
| `InternetAccess` | Obrigatório; `true` permite tráfego de saída, `false` impede encaminhamento externo |

Exigir **exatamente uma** fonte de disco. Preservar os dados e o perfil de origem. Derivar `DiskGB` da capacidade virtual real da imagem, sem confundir o tamanho do arquivo QCOW2 no host com tamanho do disco virtual. Hoje `RegisterProfile` exige `DiskGB` no objeto `Profile`; o adaptador deverá preencher esse campo mediante inspeção confiável.

## Integração e estado real do InsulaVM

| Fonte | Operação | Situação |
| --- | --- | --- |
| QCOW2 preparada | `InsulaVM.Engine.RegisterProfile(Profile, sourceQCOW2)` | **Disponível**, registra imagem e metadados, mas ainda não implementa política de internet por perfil |
| VM existente sem destruir origem | Nova operação de **clonagem não destrutiva** no InsulaVM | **Pendente**. A existente `FreezeVM` desliga e **remove a origem** após salvar o perfil; jamais invocá-la silenciosamente |
| Instância futura do perfil | `StartVMWithOptions` | **Disponível**, clona perfil e utiliza RAM/vCPUs originais quando overrides são zero |

Para copiar VM existente, o InsulaVM deverá produzir imagem consistente de disco (VM desligada ou mecanismo de quiesce/snapshot seguro), limpar identificadores **na cópia**, publicar QCOW2 autônomo e manter a VM de origem inalterada. Nenhuma mutação direta do filesystem interno do InsulaVM pelo GitMaster Actions.

## Internet: rede NAT ou isolada

**Pendência técnica confirmada:** o InsulaVM atual recebe rede de `config.yaml: virtualization.network`, global, e não disponibiliza configuração de rede no `profile.Profile` ou `StartVMWithOptions`. Portanto `InternetAccess` **não está implementado**.

Evolução necessária no InsulaVM:

1. Persistir `InternetAccess` em `profile.yaml` e aplicá-lo em toda VM iniciada a partir dele.
2. Escolher rede Libvirt validada: NAT para saída permitida ou rede isolada **sem `forward`** quando proibida. Preservar IP/DHCP e comunicação do host com guest para SSH.
3. Verificar a configuração real de encaminhamento, não apenas nome da rede. Rede isolada pode continuar comunicando-se com host/outras VMs; não é isolamento completo.
4. Até existir essa funcionalidade, **retornar erro explícito de suporte**, e nunca aceitar `false` executando silenciosamente na rede global padrão.

Uma rede NAT permite saída, mas não garante internet disponível caso o host esteja offline. Fonte: [Libvirt — Network XML](https://libvirt.org/formatnetwork.html).

## Persistência, erros e resultado

- Perfis e imagens permanecem em `profile.yaml`/QCOW2 administrados pelo InsulaVM; **não criar tabela de perfis** no SQLite do GitMaster Actions.
- Devolver `ProfileInfo` após publicação válida; em erro, não inventar sucesso nem apagar VM-fonte.
- Erros propostos: nome inválido/duplicado, duas origens ou nenhuma, QCOW2 inválido, VM inexistente, rede não suportada, clonagem não destrutiva ainda indisponível e falha de registro.
- O perfil deve poder ser usado muitas vezes por Actions independentes, cada uma recebendo sua VM clonada.

## Exemplo conceitual (ainda não compilável)

```go
internet := false
profile, err := engine.CreateProfile(ctx, gitmasteractions.CreateProfileRequest{
    Name: "debian13-mysql",
    SourceQCOW2Path: "/srv/images/debian13-mysql.qcow2",
    MemoryMiB: 4096,
    VCPUs: 2,
    InternetAccess: &internet,
})
// Uma Action futura usa: vm.profile: debian13-mysql
```

## Referências

- [INTF-GO-001 — API pública](../interfaces/go/INTF-GO-001-biblioteca-publica.md)
- [INTF-TERM-001 — CLI de testes](../interfaces/terminal/INTF-TERM-001-cli-testes.md)
- [CONTR-001 — YAML de Action](CONTR-001-configuracao-action.md)
- [ARQ-001 — Arquitetura](../arquitetura/ARQ-001-executor-actions.md)
- [InsulaVM — Biblioteca Go](https://github.com/ahhvahh/InsulaVM/blob/main/docs/arquitetura/ARQ-004-biblioteca-go.md)
- [InsulaVM — FreezeVM](https://github.com/ahhvahh/InsulaVM/blob/main/docs/fluxos/FLUX-004-congelar-vm.md)
