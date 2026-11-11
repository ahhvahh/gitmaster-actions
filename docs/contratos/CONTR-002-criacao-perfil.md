<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-002 — Cadastro de profiles e consulta de imagens

**Estado:** `RegisterVMProfile`, `GetVMProfile`, `ListVMProfiles` e `ListVMImages` implementados. Rede por perfil e clonagem não destrutiva de VM-fonte ainda **não** são suportadas por este contrato.

## Interface existente

```go
type ProfileRequest struct {
    OS        string
    Name      string
    Image     string // nome de arquivo QCOW2 na pasta administrativa
    VCPUs     int
    MemoryMiB int
    DiskGiB   int
}
profile, err := engine.RegisterVMProfile(ctx, gitmasteractions.ProfileRequest{
    Name: "ci-go", OS: "debian", Image: "ci-go.qcow2",
    VCPUs: 2, MemoryMiB: 2048, DiskGiB: 20,
})
```

`ImagesDir` e `InsulaConfigPath` são fornecidos em `New(Options)`. A imagem deve ser um **arquivo regular QCOW2 existente**, identificado apenas pelo basename autorizado; symlinks e caminhos relativos/arbitrários no YAML não são aceitos. O método delega o cadastro ao `InsulaVM.Engine.RegisterProfile`, sem criar catálogo duplicado no SQLite das Actions.

`ListVMImages` enumera `.qcow2`, `.iso` e `.img` regulares na pasta administrativa, com nome, formato e tamanho do arquivo. **Listar ISO não significa que seja aceita como imagem QCOW2 de um profile.**

## Limites atuais

- `RegisterVMProfile` aceita somente QCOW2 previamente preparada. Não recebe ID de uma VM já existente como origem.
- A configuração de rede do InsulaVM é global na API consultada; não existe `InternetAccess` isolado por profile nesta implementação. **Não oferecer um booleano de isolamento de rede que não seja aplicado.**
- A Action herda RAM/vCPUs do perfil; o parser `action-v1` rejeita overrides. A API de VM do InsulaVM suporta overrides por instância, mas o adaptador Action ainda não os publica.
- O InsulaVM controla arquivos de profile, QCOW2, Libvirt e SSH. Nenhum comando externo de VM é executado pelo GitMaster Actions.

## Evoluções que exigem novo contrato

Se necessário cadastrar profiles a partir de VM existente sem destruí-la ou escolher rede NAT/isolada por profile, implementar primeiro a API correspondente no InsulaVM e criar novo contrato/testes antes de expor esses campos. Não executar `FreezeVM` silenciosamente quando essa operação destruiria a VM de origem.

## Referências

[Interface Go](../interfaces/go/INTF-GO-001-biblioteca-publica.md) · [CLI](../interfaces/terminal/INTF-TERM-001-cli-testes.md) · [InsulaVM](https://github.com/ahhvahh/InsulaVM)