# Deploy do Astra

Este diretório reúne o roteiro de instalação e operação. O repositório contém o frontend e o backend; publique e atualize os dois na mesma versão.

| Plataforma | Procedimento | Situação atual |
| --- | --- | --- |
| Windows 11 / NTFS | [Deploy Windows](windows.md) | Empacotador e instalador de serviços implementados; homologação em máquina limpa pendente |
| Ubuntu 24.04 x86_64 / ext4 local 4 KiB | [Deploy Linux](linux.md) | Adaptadores nativos, pacote e serviços systemd implementados; homologação operacional em andamento |

Para editar o projeto e executar processos em terminais, use [desenvolvimento local](../development.md). Esse fluxo é diferente da instalação de serviços.

## Componentes e rede

O navegador acessa o core por HTTPS. O core encaminha a interface para Node.js e controla o worker de mídia. O worker inicia seu próprio MediaMTX e usa FFmpeg/ffprobe. PostgreSQL guarda os metadados; o volume do acervo guarda os vídeos e seu índice privado.

| Porta padrão do pacote Windows | Uso | Exposição |
| --- | --- | --- |
| TCP 18444 | HTTPS do core, interface e API | Loopback por padrão; rede mediante configuração |
| UDP 18189 | Mídia WebRTC | Clientes autorizados na rede, quando habilitada |
| TCP 13001 | Frontend Node.js | Loopback |
| TCP 56433 | PostgreSQL exclusivo | Loopback |
| TCP 19091 | Controle do worker | Loopback |
| TCP 19997 | API do MediaMTX | Loopback |
| TCP 18889 | Sinalização WHEP | Loopback; encaminhada pelo core |
| TCP 18554 | RTSP interno | Loopback |

No desenvolvimento, HTTPS usa 18443, Node usa 13000 e PostgreSQL usa a porta definida pelo desenvolvedor. O servidor também precisa alcançar as portas RTSP e, conforme a integração, HTTP/HTTPS das câmeras. Acesso HTTPS funcionando não comprova conectividade WebRTC.

## Antes de disponibilizar

1. Registre o commit da versão, os runtimes usados e os caminhos de binários, dados e acervo.
2. Prepare DNS/IP, certificado, conectividade com câmeras e clientes e capacidade de disco.
3. Troque a senha inicial `admin/admin` antes de liberar acesso pela rede.
4. Valide vídeo ao vivo, gravação, reprodução, exportação e permissões com uma câmera de referência.
5. Confira parada, reinício, recuperação e restauração em homologação. Registre os resultados e os limites observados.

Build e testes automatizados não homologam capacidade de câmeras, hardware, instalação ou operação contínua. Os critérios adicionais estão em [validação do backend](../../backend/docs/validation.md).

## Atualização e backup

Planeje uma janela de manutenção: reiniciar a mídia interrompe as gravações. Preserve configurações, credenciais, chave `camera-key.bin`, PostgreSQL, vídeos e `ledger.db` de forma consistente. O backup de metadados do instalador Windows **não inclui o acervo**. Guarde também uma cópia protegida fora do disco da instalação e ensaie a restauração.

Migrações são executadas pelo core. Voltar a um binário anterior depende de compatibilidade de schema; não há downgrade automático. Consulte os procedimentos e limites específicos de cada plataforma antes de atualizar.
