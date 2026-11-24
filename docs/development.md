# Desenvolvimento local no Windows

Este procedimento usa ferramentas instaladas pelo desenvolvedor. Não depende de `.scratch/`, `.tools/` ou de configurações do computador do mantenedor. Os comandos partem de uma cópia contendo os dois diretórios de produto.

## Ferramentas e banco

Instale Go compatível com `go.mod`, Node.js 24, PostgreSQL 16, MediaMTX, FFmpeg e ffprobe. Deixe Go, Node, npm, FFmpeg e ffprobe no PATH. Guarde o caminho absoluto do executável MediaMTX.

Crie um banco exclusivo de desenvolvimento. Em `psql`, conectado como administrador do seu PostgreSQL:

```sql
CREATE ROLE astra_dev LOGIN;
\password astra_dev
CREATE DATABASE astra_dev OWNER astra_dev;
```

O comando `\password` solicita uma senha sua. Não reutilize senhas de produção. O core cria seu schema e executa as migrações ao iniciar.

Mantenha os dados fora da pasta de fontes, por exemplo em `%LOCALAPPDATA%\Astra-dev`. Esse diretório conterá certificados, chave das credenciais de câmera e configurações privadas. Não o envie junto de um ZIP de fontes.

## Compilar

Na raiz da cópia de fontes:

```powershell
Push-Location backend
go build -trimpath -o bin/vms.exe ./cmd/vms
go build -trimpath -o bin/vms-worker.exe ./cmd/vms-worker
go build -trimpath -o bin/vms-event-worker.exe ./cmd/vms-event-worker
Pop-Location
Push-Location frontend
npm ci
npm run build
Pop-Location
```

Confira o sucesso de cada comando antes de continuar. A instalação de dependências e o build da interface podem acessar a internet.

## Preparar comunicação privada

Gere uma vez um segredo exclusivo para o core e o worker:

```powershell
$astraData = Join-Path $env:LOCALAPPDATA 'Astra-dev'
New-Item -ItemType Directory -Force -Path $astraData | Out-Null
$astraSecretFile = Join-Path $astraData 'worker-secret.txt'
if (Test-Path -LiteralPath $astraSecretFile) { throw 'Segredo já existe; reutilize o arquivo existente.' }
$astraBytes = New-Object byte[] 48
$astraRng = [Security.Cryptography.RandomNumberGenerator]::Create()
$astraRng.GetBytes($astraBytes)
$astraRng.Dispose()
[Convert]::ToBase64String($astraBytes) | Set-Content -LiteralPath $astraSecretFile
```

Restrinja o acesso ao diretório privado à sua conta e aos processos que precisarem dele. O segredo não precisa ser exibido no terminal.

## Iniciar os processos

Use quatro terminais. As portas abaixo precisam estar disponíveis para esta instalação.

**Terminal 1**, na pasta `frontend`:

```powershell
npm start
```

Para editar a interface com atualização automática ao salvar, use `npm run dev` nesse terminal. Continue acessando pela entrada HTTPS do core na porta 18443. Alterações no backend exigem recompilar e reiniciar o processo Go.

**Terminal 2**, na raiz da cópia de fontes:

```powershell
$astraData = Join-Path $env:LOCALAPPDATA 'Astra-dev'
$env:VMS_WORKER_SECRET = (Get-Content (Join-Path $astraData 'worker-secret.txt') -Raw).Trim()
$astraMediaMTX = Read-Host 'Caminho absoluto para mediamtx.exe'
.\backend\bin\vms-worker.exe --mediamtx $astraMediaMTX --data (Join-Path $astraData 'worker') --udp '127.0.0.1:18189'
```

O worker inicia o MediaMTX; não execute outra instância nas mesmas portas. FFmpeg e ffprobe devem estar no PATH desse terminal.

**Terminal 3**, na raiz da cópia de fontes:

```powershell
$astraData = Join-Path $env:LOCALAPPDATA 'Astra-dev'
$env:VMS_WORKER_SECRET = (Get-Content (Join-Path $astraData 'worker-secret.txt') -Raw).Trim()
$astraConnection = Read-Host 'URI PostgreSQL do banco astra_dev (incluindo sua senha)' -AsSecureString
$env:VMS_DATABASE_URL = [Net.NetworkCredential]::new('', $astraConnection).Password
.\backend\bin\vms.exe --data (Join-Path $astraData 'app')
```

A URI tem a forma `postgres://astra_dev:SENHA_CODIFICADA@127.0.0.1:5432/astra_dev?sslmode=disable`. Use a porta do seu PostgreSQL e codifique os caracteres reservados da senha como exigido por uma URI. `sslmode=disable` é apresentado apenas para banco local em loopback. Não cole a URI em issues ou commits.

Abra `https://localhost:18443`, aceite/confie no certificado local para esse laboratório e siga o primeiro acesso do README. O frontend isolado na porta 13000 não substitui a entrada HTTPS do core, que serve a API na mesma origem.

Para parar, use Ctrl+C em cada terminal. Remova as variáveis privadas quando terminar:

```powershell
Remove-Item Env:VMS_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:VMS_WORKER_SECRET -ErrorAction SilentlyContinue
```

Esses comandos iniciam processos no terminal, sem registrar serviços Windows. O roteiro foi conferido com os argumentos do código, mas ainda exige homologação de instalação em máquina limpa.

## Simular eventos analíticos

O **terminal 4**, na raiz da cópia de fontes, executa o coletor de eventos independentemente do banco e do Core.

```powershell
$astraData = Join-Path $env:LOCALAPPDATA 'Astra-dev'
$env:VMS_WORKER_SECRET = (Get-Content (Join-Path $astraData 'worker-secret.txt') -Raw).Trim()
.\backend\bin\vms-event-worker.exe --data (Join-Path $astraData 'events')
```

Para exercitar o protocolo e a fila, abra outro terminal em `backend` e execute `go run -tags astra_dev ./cmd/vms-simulate-camera`. No Astra de desenvolvimento, cadastre uma câmera Hikvision com o nome **Câmera simulada**, protocolo HTTP, host `127.0.0.1` e porta `19093`. Preencha os campos de usuário e senha com valores fictícios. O simulador é local e fornece somente a interface de eventos e a descoberta do perfil.

Em **Câmeras**, abra a configuração de recepção, habilite a coleta e consulte as regras. Selecione a linha 7 e salve. Em **Eventos** aparecem as intrusões da região 1 e os cruzamentos das linhas 7 e 8, com suas aberturas e encerramentos. Digite `off` no terminal da câmera para desabilitar a linha 7. No próximo ciclo de supervisão, o Astra abre a falha. Digite `on` para restabelecer a regra. O ciclo dura cinco minutos. Encerrar e reiniciar o simulador permite observar o período de cobertura incerta. Esses recebimentos percorrem o adaptador ISAPI e aparecem com o nome da câmera cadastrada.

### Gerar um lote diretamente no histórico

Na pasta `backend`, com o core de desenvolvimento já iniciado e o `VMS_DATABASE_URL` apontando para o banco de desenvolvimento, gere um lote de eventos para uma câmera cadastrada. Use o ID do ambiente e o número da câmera nesse ambiente.

```powershell
go run -tags astra_dev ./cmd/vms-simulate-events -tenant ID_DO_AMBIENTE -camera NUMERO_DA_CAMERA
```

Abra **Operação → Eventos** e clique em **Atualizar**. O lote inclui duas intrusões com o mesmo horário de câmera e um cruzamento de linha. Os três aparecem como **Simulado** e usam o histórico consultado pela interface.

## Testes e pacote Windows

Execute os comandos do README. Integrações Go dependem de PostgreSQL descartável em `VMS_TEST_DB`; testes de mídia podem exigir `VMS_TEST_RTSP`, `VMS_TEST_MEDIAMTX`, FFmpeg, ffprobe e um worker compilado. Consulte [validação do backend](../backend/docs/validation.md).

O empacotador aceita `-Go` com o caminho do seu Go instalado e `-FrontendPath` com o diretório do frontend; informe-os para substituir os padrões particulares do workspace original. Consulte os outros argumentos em [build-package.ps1](../backend/deploy/windows/build-package.ps1).

Não publique `dist/` como se fosse uma distribuição homologada. Ele contém runtimes de terceiros cujas licenças e materiais precisam ser conferidos separadamente.
