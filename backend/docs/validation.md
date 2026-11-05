# Validação do backend

Os testes combinam verificações unitárias e integrações com PostgreSQL, NTFS, MediaMTX e FFmpeg. Simulações de falhas não substituem a homologação física.

## Testes sem infraestrutura

Na pasta `backend`, com Go compatível com `go.mod`:

```powershell
go test ./... -count=1 -timeout 120s
```

Os testes que dependem de infraestrutura usam `t.Skip` quando suas variáveis não estão definidas. Confira os motivos das omissões com `go test -v ./...`; uma suíte verde com testes pulados não comprova o caminho completo.

## Integrações

Use um PostgreSQL exclusivamente de laboratório. Os testes criam schemas temporários; o banco deve ser descartável e não conter dados de produção. FFmpeg e ffprobe precisam estar no PATH. Compile o worker antes do teste público de mídia.

Configure no terminal, sem salvar as credenciais em arquivos versionados:

```powershell
$astraTestConnection = Read-Host 'URI de PostgreSQL descartável para testes' -AsSecureString
$env:VMS_TEST_DB = [Net.NetworkCredential]::new('', $astraTestConnection).Password
$env:VMS_TEST_RTSP = Read-Host 'URL de fonte RTSP sintética do laboratório'
$env:VMS_TEST_MEDIAMTX = Read-Host 'Caminho absoluto para mediamtx.exe'
go build -trimpath -o bin/vms-worker.exe ./cmd/vms-worker
go test ./... -count=1 -timeout 120s
```

Não imprima a URI do banco ou URLs com credenciais. Deixe as portas usadas pelos testes disponíveis; os testes de mídia têm endereços de loopback fixos definidos em seus fixtures. Não execute os testes em paralelo contra uma instalação em uso.

A regressão opcional com oito fontes independentes exige os componentes de mídia e é ativada explicitamente:

```powershell
$env:VMS_TEST_MULTISOURCE = '1'
go test ./internal/app -run TestEightIndependent -count=1 -timeout 120s -v
```

Esse teste usa publishers sintéticos e verifica isolamento e conteúdo das gravações. Não mede a capacidade em câmeras físicas nem garante uma quantidade de câmeras de produção.

Ao terminar, remova as variáveis privadas do terminal:

```powershell
Remove-Item Env:VMS_TEST_DB -ErrorAction SilentlyContinue
Remove-Item Env:VMS_TEST_RTSP -ErrorAction SilentlyContinue
Remove-Item Env:VMS_TEST_MEDIAMTX -ErrorAction SilentlyContinue
Remove-Item Env:VMS_TEST_MULTISOURCE -ErrorAction SilentlyContinue
```

## Fronteiras exercitadas pelos testes

- Login, senha, sessões, contextos e concessões vinculadas a ação e escopo.
- Isolamento entre tenants e ausência de acesso implícito a vídeo para administração global.
- Sessões de mídia, revogação, limpeza e reutilização de conexões de controle.
- Gravação, índice, admissão de espaço, retenção e cotas.
- Reprodução, exportação, lacunas, orçamento e permissões atuais.
- SSE, diagnóstico e filtragem de URLs nos logs de mídia.
- Supervisão de processos; instalação SCM e reboot exigem validação separada.

Consulte [controle de mídia](media-control.md) e [índice de armazenamento](storage-index.md) para os contratos internos.

## Homologação ainda necessária

Ainda são necessários ensaios em servidor dimensionado, câmeras físicas e firmwares distintos, clientes separados, carga mista, operação prolongada, falta de conectividade, sobrescrita e volume fisicamente cheio. Instalação, atualização, contas de serviço, ACLs e reboot precisam ser conferidos em Windows limpo.

Não encha o disco do usuário para simular falta de espaço: use um volume isolado e descartável. Não confunda resultados de fontes sintéticas locais com comprovação de captura–tela, diversidade de codecs ou capacidade de produção.
