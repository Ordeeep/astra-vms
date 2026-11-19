# Deploy no Windows

Este roteiro parte da raiz do repositório e usa o [instalador existente](../../backend/deploy/windows/README.md). O alvo de referência é Windows 11 com disco local NTFS. Instalação, ACLs, atualização e reboot em máquina limpa ainda exigem homologação.

## 1. Preparar a máquina de build

São necessários Go compatível com [go.mod](../../backend/go.mod) (atualmente 1.27.0), Node.js 24 com npm, PostgreSQL 16 completo (`bin`, `lib` e `share`), MediaMTX, FFmpeg e ffprobe para Windows. Use binários compatíveis com a arquitetura do servidor e registre suas versões. Confira as obrigações de redistribuição em [THIRD-PARTY.md](../../backend/deploy/windows/THIRD-PARTY.md).

Go, Node e npm devem estar no PATH. Os caminhos abaixo para ferramentas de mídia são exemplos e precisam corresponder aos arquivos instalados. Não é necessário iniciar o PostgreSQL da máquina de build para empacotar; o instalador criará um cluster exclusivo no destino.

```powershell
git clone https://github.com/Ordeeep/astra-vms.git
Set-Location astra-vms
git rev-parse HEAD

Push-Location frontend
npm ci
npm run build
if ($LASTEXITCODE -ne 0) { throw 'Falha no build do frontend' }
npm run typecheck
if ($LASTEXITCODE -ne 0) { throw 'Falha no TypeScript' }
npm run test:recording-timeline
if ($LASTEXITCODE -ne 0) { throw 'Falha nos testes da linha do tempo' }
Pop-Location

Push-Location backend
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Falha nos testes Go' }
Pop-Location
```

Confira o sucesso de cada comando, inclusive `npm ci`, antes de continuar. O build pode exigir acesso à internet para dependências e fontes. Sem variáveis de integração, parte dos testes Go é pulada.

## 2. Construir e verificar o pacote

Na raiz do repositório, ajuste os caminhos dos runtimes:

```powershell
$astraPackageArgs = @{
    FrontendPath = (Resolve-Path '.\frontend').Path
    Go = (Get-Command go.exe).Source
    Node = (Get-Command node.exe).Source
    PostgresRoot = 'C:\Program Files\PostgreSQL\16'
    MediaMTX = 'C:\Tools\mediamtx\mediamtx.exe'
    FFmpeg = 'C:\Tools\ffmpeg\bin\ffmpeg.exe'
    FFprobe = 'C:\Tools\ffmpeg\bin\ffprobe.exe'
}
.\backend\deploy\windows\build-package.ps1 @astraPackageArgs
```

O script compila core, worker, supervisor Windows e frontend e informa o destino `backend\dist\vms-windows-<data-hora>`. Não use `-SkipBuild` na preparação de uma nova versão sem conferir os artefatos existentes.

Copie o caminho exato informado pelo empacotador:

```powershell
$astraBundle = Read-Host 'Caminho absoluto do pacote gerado'
& (Join-Path $astraBundle 'verify-package.ps1') -Bundle $astraBundle
```

Essa verificação confere o manifesto SHA-256 e o reinício do Node sob o supervisor em porta temporária. Não instala serviços nem comprova reinício do Windows. Transfira a pasta completa, inclusive `manifest.json`, sem modificar seu conteúdo. O destino não precisa de Go/npm: o pacote inclui os runtimes de operação.

## 3. Instalar no servidor

Em **PowerShell como administrador**, entre na pasta do pacote. Reserve as portas da [tabela de rede](README.md#componentes-e-rede) e use diretórios distintos para programa e dados.

Para validar inicialmente somente no próprio servidor:

```powershell
.\install.ps1
```

| Item | Padrão |
| --- | --- |
| URL | `https://localhost:18444` |
| Binários e versões | `C:\Program Files\VMS` |
| Dados privados e logs | `C:\ProgramData\VMS` |
| Acervo inicial | `C:\ProgramData\VMS\archive` |
| PostgreSQL | Cluster exclusivo em loopback, TCP 56433 |

Para uma **nova instalação** acessível pela rede, substitua o DNS do exemplo:

```powershell
.\install.ps1 `
    -InstallRoot 'C:\Program Files\VMS' `
    -DataRoot 'C:\ProgramData\VMS' `
    -PublicHost 'astra.exemplo.interno' `
    -Listen '0.0.0.0:18444' `
    -AllowNetworkAccess
```

`-PublicHost` é apenas o DNS/IP, sem esquema ou porta, e precisa ser alcançável pelos clientes. O instalador configura esse nome no certificado local e no endereço anunciado pelo WebRTC. As regras de firewall abrangem TCP HTTPS e UDP 18189 nos perfis Domain/Private. NAT, proxies e sub-redes de clientes exigem validação adicional.

Em instalação existente, o instalador reutiliza host, listener e portas salvos em `installation.json`; fornecer argumentos novos não reconfigura esses valores. Planeje mudanças de endereço separadamente, incluindo configurações privadas, certificado, DNS e firewall.

## 4. Certificado e primeiro acesso

O core gera um certificado local no primeiro início. Para produção, use certificado confiável para o DNS escolhido ou distribua a confiança no certificado conforme a política da infraestrutura. Nenhuma raiz de confiança é instalada automaticamente.

Para certificado fornecido pela infraestrutura, configure `--cert` e `--key` nos argumentos de `C:\ProgramData\VMS\config\VMS-Core.json`. Preserve os demais argumentos e dê leitura dos arquivos à identidade `NT SERVICE\VMS-Core`. Reinicie o core após a alteração; mudar apenas `-PublicHost` não renova um certificado já existente.

Entre com `admin/admin` e altere a senha em Minha conta antes da abertura de acesso pela rede. Cadastre site, câmera, operadores e permissões. Em Gravação e agenda, escolha a pasta de gravação, perfil, agenda, retenção e cotas.

O instalador concede acesso ao acervo padrão. Para uma pasta em outro volume **NTFS local**, crie somente o diretório necessário e conceda Modify ao worker, em PowerShell elevado:

```powershell
New-Item -ItemType Directory -Path 'D:\Astra\archive' -Force
icacls.exe 'D:\Astra\archive' /grant 'NT SERVICE\VMS-Media:(OI)(CI)M'
```

Depois selecione essa pasta na interface. Não use a raiz do disco, diretórios comprimidos/criptografados ou compartilhamentos de rede como acervo de referência.

## 5. Conferir operação

```powershell
Get-Service VMS-Database,VMS-Web,VMS-Media,VMS-Core
curl.exe --fail https://astra.exemplo.interno:18444/api/v1/health
curl.exe --fail https://astra.exemplo.interno:18444/api/v1/version
Get-Content 'C:\ProgramData\VMS\logs\core.log' -Tail 50
Get-Content 'C:\ProgramData\VMS\logs\media.log' -Tail 50
```

Use a URL correspondente à instalação e um certificado confiável no cliente. Para diagnóstico inicial de certificado local, `curl.exe -k --fail https://localhost:18444/api/v1/health` ignora a validação TLS apenas nessa chamada.

O health verifica core e conexão ao banco. Confira também a página de login, vídeo em outro computador, disponibilidade da câmera, gravação de um intervalo, reprodução e exportação MP4/JSON. Teste permissões com um operador restrito. Homologue parada, recuperação e reboot antes de considerar o deploy concluído.

Os quatro serviços iniciam automaticamente. Logs ficam em `logs\core.log`, `media.log` e `web.log`, com rotação de duas gerações de aproximadamente 5 MiB. Auditoria funcional é consultada na interface.

## 6. Atualizar, fazer backup e recuperar

Gere e verifique um novo pacote. Em janela de manutenção, execute seu `install.ps1` como administrador com os **mesmos** caminhos da instalação:

```powershell
.\install.ps1 -InstallRoot 'C:\Program Files\VMS' -DataRoot 'C:\ProgramData\VMS'
```

O atualizador faz backup dos metadados, para os serviços da aplicação, instala uma nova pasta de versão e preserva dados e configurações. O PostgreSQL instalado não é substituído por esse fluxo. Alterações de versão maior do banco exigem procedimento próprio. Depois repita as verificações de operação.

Para backup manual, execute o script do pacote preservado:

```powershell
.\backup.ps1 -DataRoot 'C:\ProgramData\VMS'
```

O resultado fica em `C:\ProgramData\VMS\backups\<data-hora>`, com catálogo, configurações e chaves. Ele contém segredos e **não contém os vídeos**. Preserve também o acervo e seu `ledger.db` consistente, com os serviços de mídia parados, e copie o conjunto para armazenamento protegido fora do servidor. Nunca restaure o catálogo sem a chave `camera-key.bin` correspondente.

A restauração não é automatizada. Ensaie em cópia isolada: use `pg_restore` compatível no cluster exclusivo, recoloque configurações/chaves e um conjunto consistente de acervo/ledger, confira caminhos e ACLs e valide a operação antes de substituir a instalação em uso. Não combine índice antigo com vídeos sobrescritos.

Se uma atualização falhar, preserve a pasta de dados, consulte logs e estado dos serviços e identifique a fase da falha. Versões anteriores permanecem em `releases`, mas o schema pode ter migrado: não troque apenas os executáveis sem verificar a compatibilidade.

## Diagnóstico rápido

| Sintoma | Conferir |
| --- | --- |
| HTTPS não abre | `VMS-Core`, listener configurado, certificado, DNS e firewall |
| Interface indisponível / HTTP 503 | `VMS-Web`, `web.log`, URL privada do frontend no core |
| Health retorna 503 | `VMS-Database`, porta e URI privadas do PostgreSQL |
| Página abre e vídeo não aparece | `VMS-Media`, `media.log`, RTSP da câmera, host anunciado e UDP 18189 entre cliente e servidor |
| Gravação não inicia | Agenda, perfil, cotas, espaço livre, NTFS e ACL da identidade de mídia |
| Instalador recusa diretório ou serviço | Identidade da instalação, caminhos anteriores, conflitos de porta/serviço e manifesto íntegro |

Ao compartilhar logs, remova credenciais e dados privados. Para testes de integração e critérios de carga, consulte [validação](../../backend/docs/validation.md).
