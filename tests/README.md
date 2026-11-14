# Testes locais — GitMaster Actions

Esta pasta centraliza compilação, execução e arquivos de trabalho. O modo padrão não inicia VMs.

## Estrutura

```text
tests/
├── build.sh                      # gera o binário com o nome do projeto
├── run.sh                        # runner de testes
├── fixtures/README.txt           # material de origem do ZIP mínimo
├── verifiers/
│   ├── branch/verify.go          # 2 Jobs: um executa, outro é ignorado
│   └── parallel/verify.go        # dependências, falha, skips e paralelismo
├── bin/gitmaster-actions         # criado pelo build (ignorado pelo Git)
└── work/<execucao>/              # arquivos gerados (ignorados pelo Git)
    ├── build.log
    ├── unit.log                   # quando modo unit/all
    ├── snapshot.zip               # quando modo vm/all
    ├── branch/<branch>/
    │   ├── actions.db
    │   ├── action.json
    │   ├── stderr.log
    │   └── verify.log
    └── parallel/<branch>/
        └── ...
```

## Compilar

```bash
bash tests/build.sh
./tests/bin/gitmaster-actions --help
```

Reaproveita o build oficial (`scripts/build.sh`) com `CGO_ENABLED=0`, gerando sempre `tests/bin/gitmaster-actions`.

## Testes automáticos (sem VM)

```bash
bash tests/run.sh
# ou
bash tests/run.sh --mode unit
```

Executa `go mod tidy -diff`, `go test -race -count=1 ./...` e `go vet ./...` além do build. Verifica a lógica da biblioteca com runtimes simulados.

## Testes integrais com VM descartável

Pré-requisitos: Linux com KVM/Libvirt, Go, zip e perfil `ci-smoke` configurado no catálogo de imagens QCOW2. A imagem precisa ter SSH/Cloud-Init, `unzip` e `/tmp/workspace` acessível.

```bash
# Filtro de branch: develop executa job_develop e ignora job_main; main faz o inverso
bash tests/run.sh --mode vm --case branch \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Dependências e paralelismo, incluindo falha intencional na branch teste-falha
bash tests/run.sh --mode vm --case parallel \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml

# Todos os testes: Go e CLI em VM
bash tests/run.sh --mode all \
  --insula-config /etc/insulavm/config.yaml \
  --profiles-config /etc/gitmaster-actions/profiles.yaml
```

Os YAMLs canônicos continuam em `examples/workflow/`. O runner usa esses mesmos arquivos, sem manter cópias divergentes. A CLI grava Action, Jobs e Commands no SQLite e retorna JSON com `--details`. Os verificadores exigem os status, códigos de saída e `skip_reason` previstos.

Na branch `teste-falha`, a Action deve terminar como `failed` (comando `exit 17`), e seu dependente como `skipped`. Esse resultado é **sucesso do teste** quando o verificador confirma a falha prevista.

## Diagnóstico

Cada execução mantém seus artefatos em `tests/work/<data>-<pid>/`, sem sobrescrever resultados anteriores. Abra `unit.log`, `build.log`, `action.json`, `verify.log` e `stderr.log` para investigar falhas. Nenhum deles é enviado ao Git graças a `tests/.gitignore`.

Para limpar apenas os artefatos gerados:

```bash
rm -rf tests/bin tests/work
```

Scripts antigos em `scripts/tests/` permanecem compatíveis. A execução com VM real não é realizada pelos runners de CI sem KVM.
