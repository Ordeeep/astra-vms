# Homologação do Astra no Ubuntu 24.04

Registro técnico de 2026-11-19. Suporte nativo implementado e instalado em homologação, mantendo Windows. HTTPS, serviços, captura e gravações foram verificados; a liberação definitiva de produção depende dos ensaios operacionais pendentes. Este registro omite endereços, identidades e credenciais do ambiente. O inventário detalhado e os relatórios operacionais permanecem privados, fora do Git.

## Plataforma e implementação

- Ubuntu 24.04 x86_64, systemd, PostgreSQL 16 e acervo em ext4 local com blocos de 4096 bytes. A VM de ensaio possui quatro vCPUs, aproximadamente 8 GiB de RAM e volume de aproximadamente 247 GiB compartilhado com o sistema operacional; não houve reparticionamento ou formatação.
- Adaptadores Linux de exclusividade do acervo (`flock`), espaço disponível (`statfs`), pré-alocação (`fallocate`) e medição física (`st_blocks`). Adaptadores Windows/NTFS preservados.
- Arquivos abertos maiores que 16 KiB reservam temporariamente até seis vezes os dados para cobrir conservadoramente metadados de extents do ext4. Na finalização, a cobrança passa à alocação física medida. O dimensionamento de gravações e exportações precisa considerar essa reserva; o cálculo Windows permanece igual.
- Escrita atômica de configuração e sincronização dos diretórios Linux na persistência de arquivos e índices.
- Processo guardião Linux encerra comandos e seus descendentes comuns quando o dono para ou morre; grupos de processos e cgroups do systemd complementam o encerramento. Não cobre processos que escapem deliberadamente do grupo. Core e worker tratam SIGTERM.
- Pacote nativo, instalador, unidades systemd e backup consistente. Instalação começa em loopback; atualizações preservam ambientes privados, chaves e drop-ins. O pacote inclui Node e MediaMTX; PostgreSQL e FFmpeg vêm dos pacotes do sistema.

Decisão: [ADR 0012](adr/0012-suporte-nativo-ubuntu-com-systemd.md). Procedimentos: [deploy Ubuntu](deploy/linux.md), [empacotamento](../backend/deploy/linux/README.md) e [validação](../backend/docs/validation.md).

## Verificações executadas

| Verificação | Resultado e alcance |
| --- | --- |
| Backend Ubuntu | `go test ./... -p 1 -count=1 -timeout 600s` passou com banco exclusivo, RTSP sintético, MediaMTX e oito fontes independentes habilitados |
| Concorrência | `go test -race` passou nos pacotes storage, servicehost e media; `GORACE=atexit_sleep_ms=0` evitou atraso artificial na saída dos processos guardiões de teste |
| Regressão Windows | Todos os pacotes passaram na regressão final, sem repetir o teste de heap de mil arquivos já aprovado no ensaio anterior. A fixture de redução administrativa foi corrigida para percorrer as etapas limitadas por orçamento de IO e confirmada separadamente e na regressão final |
| Frontend | TypeScript e sete testes da linha do tempo passaram no Windows; instalação de dependências e build standalone de produção passaram nativamente no Linux |
| Runtimes do ensaio | Go 1.27.4, Node 24.22.3, MediaMTX 1.21.3, PostgreSQL 16.15 e FFmpeg 6.1.1; downloads de Go, Node e MediaMTX tiveram SHA-256 conferido |
| Serviços | Core, mídia e web ativos e habilitados para boot; encerramento forçado do worker provocou recuperação pelo systemd |
| HTTPS por IP | API de saúde e interface responderam de outra estação em TCP 443; core sem root usa apenas `CAP_NET_BIND_SERVICE`. Certificado local com SAN correspondente ao IP; teste com `curl -k` não comprova confiança do navegador |
| Backup | Dump, configurações e chaves copiados com core/mídia parados e serviços restaurados depois. Restauração de metadados em banco isolado permitiu saúde e login; vídeos não participaram desse ensaio |
| Atualização | Troca entre dois pacotes com os mesmos binários e ajustes do instalador preservou hashes de chaves/ambientes, login e política; não comprova migração de schema entre versões diferentes |

O ensaio de revogação de vídeo passou a usar operador comum: o administrador da instalação conserva autoridade independentemente de grupos do tenant. As verificações de corte de RTP, renovação e negativa de acesso foram mantidas para H.264 e H.265.

## Câmeras e retenção configuradas

A seleção de nove câmeras da instalação local foi copiada pela API para a homologação após backup de metadados. Os nove novos cadastros foram criados antes da exclusão dos cinco antigos; os cinco trabalhos de limpeza concluíram sem erro. A origem local foi preservada. Credenciais e payloads de transferência não integram os fontes; as cópias temporárias com segredos foram removidas após a operação.

- As nove câmeras possuem gravação contínua no perfil principal e retenção configurada de sete dias; a política do tenant também estabelece sete dias.
- Cota de gravações: 150 GiB; exportações: 10 GiB; orçamento global: 160 GiB; margem livre: 40 GiB.
- Cinco fontes apresentaram captura, escritas recentes e intervalos no acervo. Um segmento real de cada fonte foi decodificado com sucesso por FFmpeg.
- Quatro fontes permaneceram indisponíveis em HTTP/RTSP. Seus cadastros usam RTSP genérico com perfis conhecidos para deixar a gravação preparada; a integração do fabricante poderá ser retomada quando sua API estiver acessível. Não há gravações dessas fontes nesse ensaio.
- Uma amostra curta das cinco fontes acessíveis projetou aproximadamente 924 GiB para sete dias. O responsável aceitou manter a cota atual com sobrescrita antecipada. **Retenção configurada de sete dias não garante sete dias completos de histórico sob essa cota.** O bitrate varia e a amostra não dimensiona as quatro fontes indisponíveis.

## Pendências para produção

1. Restabelecer as quatro fontes indisponíveis e validar vídeo WebRTC, reprodução e exportação em navegadores remotos.
2. Definir destino externo, cobertura e retenção dos backups; testar recuperação do conjunto definitivo de banco, chaves, vídeos e ledger. O timer diário permanece desabilitado até configurar e testar o destino.
3. Configurar DNS interno e certificado confiável para os clientes de produção; conferir a política de acesso da rede.
4. Ensaiar reboot em janela planejada do servidor compartilhado, operação prolongada, carga mista e interrupções das câmeras.
5. Executar falta de espaço em volume isolado e descartável, registrar retenção efetiva/capacidade e obter aceite operacional.

A saúde HTTP e os testes sintéticos não comprovam disponibilidade futura, desempenho sustentado ou retenção física. A restauração de metadados não substitui um ensaio de recuperação com vídeos. Os testes de integração usam banco e arquivos exclusivos; o acervo definitivo não deve receber ensaios destrutivos.
