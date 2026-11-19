# Astra — pacote Ubuntu 24.04 x86_64

Requer PostgreSQL 16 local (cluster 16/main), FFmpeg/ffprobe e ext4 local com blocos de 4096 bytes. O pacote inclui Node e MediaMTX. Confira `BUILD-INFO`, `VERSION`, `licenses/` e `sha256sum --check SHA256SUMS` antes de instalar.

Execute `sudo bash install.sh IP_OU_DNS_DO_SERVIDOR`. A entrada inicial é `https://127.0.0.1:18443`; use túnel SSH para trocar `admin/admin` antes de autorizar LAN, conferir o firewall e definir o IP interno em `/etc/astra/core.env` (por exemplo `ASTRA_LISTEN=192.0.2.111:18443`). Reinicie `astra-core` após uma publicação autorizada.

Os serviços são `astra-core`, `astra-media` e `astra-web`. Logs: `journalctl -u astra-core -u astra-media -u astra-web`. Configurações privadas ficam em `/etc/astra`, dados em `/var/lib/astra`, acervo em `/srv/astra/archive` e versões em `/opt/astra/releases`.

O acervo inicia com limite global de 160 GiB e margem livre de 40 GiB. Configure cotas e retenção por ambiente na interface. Reservas Linux de arquivos abertos maiores que 16 KiB são conservadoras (até seis vezes os dados) para incluir metadados do ext4; ao finalizar, a cobrança passa a ser a alocação observada.

Para backup consistente: `sudo /opt/astra/current/backup.sh /DESTINO_NOVO --include-archive`. A operação interrompe core e mídia e os restaura depois. Copie o resultado protegido para fora da VM. Sem `--include-archive`, o backup não contém vídeos. O agendamento diário só deve ser habilitado depois de configurar e testar `/etc/astra/backup.env` com destino montado fora da VM.

Atualizações preservam configurações e fazem backup local de metadados antes das migrações. Não há downgrade automático; preserve banco, chaves, acervo e ledger correspondentes para recuperação. A instalação não comprova capacidade nem substitui testes com câmeras e clientes reais.
