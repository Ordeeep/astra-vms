# Deploy nativo no Ubuntu 24.04

Este procedimento atende Ubuntu 24.04 x86_64 com systemd e acervo em ext4 local de blocos de 4096 bytes. Consulte [empacotamento](../../backend/deploy/linux/README.md) para construir a versão no Linux. A instalação inicial é limpa e os ensaios de integração devem usar banco e arquivos separados dos dados da instalação.

## Preparar dependências

No servidor Ubuntu, instale os pacotes do sistema:

```bash
sudo apt-get update
sudo apt-get install --no-install-recommends postgresql-16 postgresql-client-16 ffmpeg curl ca-certificates openssl python3
```

Confira PostgreSQL 16 em loopback TCP 5432, cluster padrão `16/main`, codecs `libx264`/`libx265` no FFmpeg e espaço disponível. O pacote inclui Node e MediaMTX; não requer Go ou npm no destino. Reserve as portas da aplicação e verifique a rota para as câmeras e os navegadores. Não altere serviços preexistentes sem avaliar seu uso.

## Instalar em loopback

Transfira a pasta completa do pacote por um canal confiável. Dentro dela, substitua o endereço de exemplo pelo IP ou DNS alcançável pelos navegadores:

```bash
sha256sum --check SHA256SUMS
sudo bash install.sh 192.0.2.111
systemctl status astra-web astra-media astra-core --no-pager
curl -k --fail https://127.0.0.1:18443/api/v1/health
```

O instalador verifica o manifesto, cria contas de serviço, papel/banco exclusivos, configura segredos aleatórios e inicia os serviços com HTTPS restrito a loopback. Conflitos com nomes e caminhos de instalação nova são recusados. Não há particionamento, formatação, alteração de firewall ou reaproveitamento de bancos existentes.

O acervo inicial fica em `/srv/astra/archive`, com orçamento global de 160 GiB e margem livre de 40 GiB. Esses limites são um ponto de partida da homologação no disco atual, não uma promessa de retenção ou reserva de desempenho. As cotas e retenção dos tenants continuam sendo definidas separadamente pela interface.

## Primeiro acesso e rede interna

Para acessar o loopback a partir da estação, abra um túnel SSH:

```bash
ssh -L 18443:127.0.0.1:18443 usuario@IP_DO_SERVIDOR
```

No navegador, acesse `https://localhost:18443`, confira o certificado local e altere a senha pública inicial `admin/admin` em Minha conta. Só depois de autorizar a publicação e conferir a política de firewall, edite `/etc/astra/core.env` com `sudoedit`, mudando `ASTRA_LISTEN=127.0.0.1:18443` para o IP interno escolhido, por exemplo `ASTRA_LISTEN=192.0.2.111:18443`, e reinicie o core:

```bash
sudo systemctl restart astra-core
```

O endereço de homologação será `https://IP_DO_SERVIDOR:18443`. Depois de mudar o listener para esse IP, use também esse endereço nos checks de saúde. Um certificado local requer confiança explícita do cliente; para produção configure DNS interno e certificado confiável. Os argumentos `--cert` e `--key` do core permitem certificado próprio, com leitura autorizada à conta `astra-core`; crie um drop-in systemd para substituir `ExecStart`, preservando os demais argumentos.

Permita somente os clientes necessários em TCP 18443 e UDP 18189 nos firewalls do ambiente. PostgreSQL, Next, worker, API MediaMTX, WHEP e RTSP interno permanecem em loopback. `ASTRA_PUBLIC_HOST` deve corresponder ao IP/DNS alcançável pelos navegadores. HTTPS funcionando não comprova a entrega de vídeo WebRTC.

Os serviços são habilitados para o boot. Teste parada/reinício da aplicação e ensaie reboot do servidor em uma janela compatível com os demais serviços. Não reinicie um servidor compartilhado apenas para testar o Astra sem planejar o impacto.

### HTTPS padrão sem porta na URL

Para publicar HTTPS na porta padrão, confira se TCP 443 está livre, defina `ASTRA_LISTEN=IP_DO_SERVIDOR:443` e ajuste a política de acesso do ambiente. Mantendo o usuário `astra-core`, crie `/etc/systemd/system/astra-core.service.d/https-port.conf`:

```ini
[Service]
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
```

Após alterar ambiente/drop-in, execute `sudo systemctl daemon-reload` e `sudo systemctl restart astra-core`. Esse drop-in é preservado nas atualizações; o instalador consulta o endereço do ambiente existente para verificar saúde. Use `https://192.0.2.111/api/v1/health` e `https://192.0.2.111/` como exemplos dos checks, substituindo o endereço pelo IP real. O endereço `192.0.2.111` é apenas ilustrativo e não deve ser configurado literalmente. A configuração inicial de novos servidores continua em loopback TCP 18443.

## Logs e diagnóstico

```bash
journalctl -u astra-core -u astra-media -u astra-web --since today --no-pager
systemctl is-enabled astra-core astra-media astra-web
curl -k --fail https://127.0.0.1:18443/api/v1/version
```

A retenção do journal é uma configuração do sistema, não uma cota de gravação. Remova credenciais e informações privadas antes de compartilhar diagnósticos. A saúde HTTP não comprova câmera, captura, reprodução ou exportação.

## Atualizar

Construa uma nova versão e execute seu `install.sh` como root. O instalador reconhece a identidade da instalação, preserva ambientes/chaves, faz um backup local de metadados, interrompe os serviços, troca o link da versão e reinicia. O host informado na atualização não substitui a configuração privada existente. As versões anteriores ficam preservadas.

Uma atualização pode executar migrações do banco. Se falhar, preserve dados, logs e backup; não aponte o link para uma versão antiga sem verificar compatibilidade do schema. Não há downgrade automático. O backup pré-atualização não contém os vídeos e precisa ser copiado para fora da VM.

## Backup e restauração

Para preparar backup consistente, use um destino novo fora das pastas do programa/acervo:

```bash
sudo /opt/astra/current/backup.sh /DESTINO_PROTEGIDO/astra-20261119 --include-archive
```

O script para core e mídia, copia PostgreSQL e estado privado e, com `--include-archive`, inclui vídeos e ledger correspondente; depois restaura os serviços que estavam ativos. Isso interrompe gravações durante a cópia. O resultado contém segredos e deve ficar protegido fora da VM. Sem a opção, o backup não recupera vídeos.

Para agendamento diário, monte um destino de backup separado e configure `/etc/astra/backup.env`, pertencente a root com modo 0600:

```text
ASTRA_BACKUP_ROOT=/mnt/backup/astra
ASTRA_BACKUP_ARCHIVE=1
```

Só depois habilite `sudo systemctl enable --now astra-backup.timer`. A rotina recusa destino no filesystem raiz. Um disco na mesma VM não equivale a uma cópia externa. O timer executa às 03h no fuso do servidor; configure retenção e monitore o sucesso para que o destino não cresça sem limite. Não há remoção automática dos backups.

Ensaie a restauração em uma instalação isolada, usando a versão e dependências identificadas no backup: pare core e mídia, restaure o dump em um banco limpo, recoloque configurações e `camera-key.bin` correspondentes e restaure acervo e ledger como um conjunto. Confira a senha do papel PostgreSQL contra a URI privada restaurada, os caminhos e os proprietários (`astra-core`/`astra-media`). Nunca misture ledger antigo com vídeos sobrescritos, nem teste restauração por cima do banco em uso. Só libere o acesso depois de validar login, permissões, captura, reprodução e exportação.

O acompanhamento da implementação e os limites observados estão em [homologação Ubuntu](../homologacao-ubuntu-24.04.md).
