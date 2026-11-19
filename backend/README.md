# backend

Core e serviÃ§os de mÃ­dia do VMS local para Windows 11 e Ubuntu 24.04 x86_64. O frontend Next.js/Node fica no diretÃ³rio `frontend`.

HÃ¡ implementaÃ§Ã£o de autenticaÃ§Ã£o, tenants/grupos/permissÃµes, sites, cÃ¢meras RTSP, mosaicos, vÃ­deo WebRTC, agenda de gravaÃ§Ã£o, acervo, reproduÃ§Ã£o, exportaÃ§Ã£o MP4/JSON, retenÃ§Ã£o/cotas, auditoria e eventos SSE. A homologaÃ§Ã£o completa continua pendente; nÃ£o considerar a implementaÃ§Ã£o uma comprovaÃ§Ã£o de capacidade P1.

## Executar e instalar

Para compilar e executar a partir de uma cÃ³pia de fontes, consulte [desenvolvimento local](../docs/development.md). A licenÃ§a do cÃ³digo prÃ³prio Ã© MIT; veja [preparaÃ§Ã£o para publicaÃ§Ã£o](../docs/publication.md).

O pacote usa PostgreSQL local, Node.js, MediaMTX e FFmpeg. Consulte [instalaÃ§Ã£o e atualizaÃ§Ã£o no Windows](deploy/windows/README.md). As portas privadas devem permanecer em loopback; o core Ã© a entrada HTTPS do navegador.

No Ubuntu, consulte [pacote Linux e systemd](deploy/linux/README.md). O primeiro filesystem Linux atendido Ã© ext4 local com blocos de 4096 bytes. Reservas abertas incluem uma margem conservadora para metadados de extents; o valor fÃ­sico observado Ã© aplicado ao finalizar. Windows mantÃ©m o cÃ¡lculo e os serviÃ§os existentes.

A conta inicial Ã© `admin/admin`. Altere a senha em Minha conta antes de permitir acesso pela rede. O login aceita usuÃ¡rio ou e-mail cadastrado, sem distinguir maiÃºsculas e minÃºsculas. Em Minha conta, cada operador pode atualizar seu nome e, confirmando a senha atual, seu e-mail. E-mails nÃ£o podem coincidir com o usuÃ¡rio ou e-mail de outra conta. A atualizaÃ§Ã£o preserva contatos legados; um identificador ambÃ­guo nÃ£o autentica nenhuma conta. Nome e e-mail valem para todos os tenants autorizados. A troca de senha Ã© opcional e encerra as outras sessÃµes. Um administrador global sÃ³ acessa vÃ­deo quando tambÃ©m recebe a concessÃ£o correspondente no tenant.

SequÃªncia inicial pela interface:

1. Entre no ambiente padrÃ£o, cadastre sites e cÃ¢meras em CÃ¢meras e locais. Informe RTSP principal e, opcionalmente, substreams.
2. Configure a pasta local e os limites em GravaÃ§Ã£o e agenda (NTFS no Windows, ext4 no Ubuntu). Defina o perfil e a agenda de cada cÃ¢mera.
3. Monte e salve mosaicos em Ao vivo. Qualidade ao vivo e qualidade gravada sÃ£o independentes.
4. Consulte o acervo por cÃ¢mera e perÃ­odo. ExportaÃ§Ãµes apresentam a cobertura antes de gerar MP4 e relatÃ³rio JSON.
5. Configure operadores e grupos por ambiente. Confira captura, falhas e aÃ§Ãµes em Auditoria e diagnÃ³stico.

## Responsabilidades

- `cmd/vms`: HTTPS, autenticaÃ§Ã£o, autorizaÃ§Ã£o, regras de negÃ³cio, PostgreSQL, proxy da interface e entrega autorizada do acervo.
- `cmd/vms-worker`: ingestÃ£o/muxaÃ§Ã£o, supervisÃ£o do MediaMTX, controle dos arquivos e geraÃ§Ã£o de exportaÃ§Ãµes. Recebe autoridade temporÃ¡ria do core.
- `cmd/vms-service`: hospedagem e recuperaÃ§Ã£o de processos como serviÃ§os nativos do Windows.
- `internal/storage`: admissÃ£o de espaÃ§o, quota fÃ­sica, proteÃ§Ã£o de leitura, recuperaÃ§Ã£o e Ã­ndice privado do gravador. Consulte [o formato do ledger](docs/storage-index.md).

O contrato pÃºblico Ã© `/api/v1`, versÃ£o maior 1, informada por `/api/v1/version` e pelo cabeÃ§alho `X-VMS-Contract`. O cliente recusa uma versÃ£o incompatÃ­vel. MigraÃ§Ãµes do PostgreSQL sÃ£o transacionais; futuras migraÃ§Ãµes devem ser acrescentadas Ã  sequÃªncia existente.

A [exclusÃ£o definitiva de cÃ¢mera](docs/camera-deletion.md) exige permissÃ£o prÃ³pria, encerra seus vÃ­deos em uso e remove suas gravaÃ§Ãµes em segundo plano. A tarefa retoma apÃ³s falhas e reinÃ­cios; exportaÃ§Ãµes prontas seguem sua expiraÃ§Ã£o, e o resultado fica na auditoria.

O [acompanhamento de disponibilidade](docs/camera-availability.md) confirma vÃ­deo recebido pelo servidor, sem ping, e informa Online, Offline, Sem informaÃ§Ã£o ou Monitoramento suspenso na listagem. Core e worker devem ser atualizados juntos.

SSE Ã© vinculado Ã  sessÃ£o e ao contexto da aba. Clientes devem atualizar os snapshots ao conectar/reconectar, receber `ResyncRequired` ou mudar a autorizaÃ§Ã£o. O cursor nÃ£o congela permissÃµes. HÃ¡ limite de conexÃµes, lote mÃ¡ximo de 100 eventos e prazo de escrita; uma conexÃ£o lenta deve reconectar.

## ValidaÃ§Ã£o

`go test ./...` executa os ensaios do backend. As integraÃ§Ãµes exigem `VMS_TEST_DB` apontando para um PostgreSQL de laboratÃ³rio, `VMS_TEST_RTSP` e `VMS_TEST_MEDIAMTX`. Os testes criam namespaces isolados; nÃ£o use um banco de produÃ§Ã£o. FFmpeg e ffprobe precisam estar no PATH.

Consulte [procedimentos de validaÃ§Ã£o](docs/validation.md) para os comandos, o ensaio com oito fontes independentes e os critÃ©rios ainda pendentes.

Ainda sÃ£o necessÃ¡rios os ensaios em servidor dimensionado, fontes fÃ­sicas homologadas, carga P1, operaÃ§Ã£o contÃ­nua e instalaÃ§Ã£o/reboot em Windows limpo. NÃ£o hÃ¡ dependÃªncia de nuvem no fluxo operacional.
