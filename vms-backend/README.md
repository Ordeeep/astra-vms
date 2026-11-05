# vms-backend

Core e serviços de mídia do VMS local para Windows 11. O frontend Next.js/Node fica no repositório independente `vms-frontend`.

Há implementação de autenticação, tenants/grupos/permissões, sites, câmeras RTSP, mosaicos, vídeo WebRTC, agenda de gravação, acervo, reprodução, exportação MP4/JSON, retenção/cotas, auditoria e eventos SSE. A homologação completa continua pendente; não considerar a implementação uma comprovação de capacidade P1.

## Executar e instalar

Para compilar e executar a partir de uma cópia de fontes, consulte [desenvolvimento local](../docs/development.md). A licença do código próprio é MIT; veja [preparação para publicação](../docs/publication.md).

O pacote usa PostgreSQL local, Node.js, MediaMTX e FFmpeg. Consulte [instalação e atualização no Windows](deploy/windows/README.md). As portas privadas devem permanecer em loopback; o core é a entrada HTTPS do navegador.

A conta inicial é `admin/admin`. Altere a senha em Minha conta antes de permitir acesso pela rede. O login aceita usuário ou e-mail cadastrado, sem distinguir maiúsculas e minúsculas. Em Minha conta, cada operador pode atualizar seu nome e, confirmando a senha atual, seu e-mail. E-mails não podem coincidir com o usuário ou e-mail de outra conta. A atualização preserva contatos legados; um identificador ambíguo não autentica nenhuma conta. Nome e e-mail valem para todos os tenants autorizados. A troca de senha é opcional e encerra as outras sessões. Um administrador global só acessa vídeo quando também recebe a concessão correspondente no tenant.

Sequência inicial pela interface:

1. Entre no ambiente padrão, cadastre sites e câmeras em Câmeras e locais. Informe RTSP principal e, opcionalmente, substreams.
2. Configure a pasta NTFS local e os limites em Gravação e agenda. Defina o perfil e a agenda de cada câmera.
3. Monte e salve mosaicos em Ao vivo. Qualidade ao vivo e qualidade gravada são independentes.
4. Consulte o acervo por câmera e período. Exportações apresentam a cobertura antes de gerar MP4 e relatório JSON.
5. Configure operadores e grupos por ambiente. Confira captura, falhas e ações em Auditoria e diagnóstico.

## Responsabilidades

- `cmd/vms`: HTTPS, autenticação, autorização, regras de negócio, PostgreSQL, proxy da interface e entrega autorizada do acervo.
- `cmd/vms-worker`: ingestão/muxação, supervisão do MediaMTX, controle dos arquivos e geração de exportações. Recebe autoridade temporária do core.
- `cmd/vms-service`: hospedagem e recuperação de processos como serviços nativos do Windows.
- `internal/storage`: admissão de espaço, quota física, proteção de leitura, recuperação e índice privado do gravador. Consulte [o formato do ledger](docs/storage-index.md).

O contrato público é `/api/v1`, versão maior 1, informada por `/api/v1/version` e pelo cabeçalho `X-VMS-Contract`. O cliente recusa uma versão incompatível. Migrações do PostgreSQL são transacionais; futuras migrações devem ser acrescentadas à sequência existente.

A [exclusão definitiva de câmera](docs/camera-deletion.md) exige permissão própria, encerra seus vídeos em uso e remove suas gravações em segundo plano. A tarefa retoma após falhas e reinícios; exportações prontas seguem sua expiração, e o resultado fica na auditoria.

O [acompanhamento de disponibilidade](docs/camera-availability.md) confirma vídeo recebido pelo servidor, sem ping, e informa Online, Offline, Sem informação ou Monitoramento suspenso na listagem. Core e worker devem ser atualizados juntos.

SSE é vinculado à sessão e ao contexto da aba. Clientes devem atualizar os snapshots ao conectar/reconectar, receber `ResyncRequired` ou mudar a autorização. O cursor não congela permissões. Há limite de conexões, lote máximo de 100 eventos e prazo de escrita; uma conexão lenta deve reconectar.

## Validação

`go test ./...` executa os ensaios do backend. As integrações exigem `VMS_TEST_DB` apontando para um PostgreSQL de laboratório, `VMS_TEST_RTSP` e `VMS_TEST_MEDIAMTX`. Os testes criam namespaces isolados; não use um banco de produção. FFmpeg e ffprobe precisam estar no PATH.

Consulte [procedimentos de validação](docs/validation.md) para os comandos, o ensaio com oito fontes independentes e os critérios ainda pendentes.

Ainda são necessários os ensaios em servidor dimensionado, fontes físicas homologadas, carga P1, operação contínua e instalação/reboot em Windows limpo. Não há dependência de nuvem no fluxo operacional.
