# Barra de gravações do Ao vivo

Na pasta `frontend`, execute `npm run test:live-recording`.

Requer Chrome e FFmpeg no PATH; `VMS_TEST_FFMPEG` permite informar o executável. O build usa Webpack/SWC do Next já instalado e gera um vídeo H.264 sintético de 12 segundos em HLS/fMP4. O Chrome decodifica os bytes reais com Hls.js; as APIs e as transmissões ao vivo usam dados sintéticos. Nenhum cadastro do ambiente é alterado e não é necessário iniciar um servidor de testes.

O cenário usa `LiveWorkspace`, seu provider, o menu de contexto, a barra e o player reais. Verifica isolamento do quadro mesmo com câmera repetida, abertura sem sessão, calendário com disponibilidade e falhas, zoom por slider/botões/roda e navegação, busca ao concluir o arraste, velocidade, pausa, passagem entre sessões e através da meia-noite, lacunas, revogação com toast e nova tentativa, respostas atrasadas, tela cheia e layouts nos temas claro/escuro. Também verifica a remoção do status fixo e do recolhimento. O navegador usa Asia/Tokyo e a câmera America/Sao_Paulo para detectar dependência indevida do fuso do navegador.

As capturas ficam em `output/playwright/live-recording` na raiz do projeto. São evidências locais geradas, sem dados pessoais.

`npm run test:recording-timeline` verifica separadamente calendário civil, horários inexistentes/ambíguos em mudanças de fuso, ancoragem e limites de zoom e mapeamento do tempo do vídeo.

A regressão de otimização também verifica faixa azul preservada durante pan conhecido sem novas consultas, crescimento por SSE enquanto o ponteiro continua arrastando, descarte de eventos de outra câmera, reconexão com indicação de desatualização e renovação da sessão a cada dois segundos, inclusive pausada. O cache tem testes de atualização parcial (apenas os segundos alterados), remoção de gravação, cancelamento, eventos durante HTTP e revogação.

O teste Go `TestRecordingAvailabilityUsesPlayablePartsAndHistoricalAuthorization` exige `VMS_TEST_DB` e usa schema exclusivo, removido ao terminar. Ele cobre agregação dos intervalos, lacunas internas, escopo histórico, limite de dois dias, datas mensais no fuso atual, extremos exclusivos, horário de verão, descarte e revogação. A suíte de interface complementa esses testes; sua API simulada não comprova captura de uma câmera física.

`TestScheduledCaptureToAuthorizedTemporalPlayback` exige também `VMS_TEST_RTSP` sintético: verifica disponibilidade e bytes decodificáveis antes de fechar o arquivo, crescimento e exportação do prefixo original enquanto a captura continua. `TestActiveCatalogGrowthIsMonotonicAndScoped` cobre revisões, eventos com câmera/período, remoção sem reativação tardia e fonte atrasada sem disponibilidade inventada. Os testes de armazenamento e HTTP cobrem leitura limitada ao checkpoint, Range, SeekEnd e recuperação de cauda/corrupção. Os testes opcionais de compatibilidade HEVC/H.264 usam `VMS_TEST_MEDIAMTX` e também consultam um arquivo ainda aberto.

`TestActiveCatalogUpgradesExistingInstallation` recria em schema descartável a estrutura da versão 8 e valida que a atualização aplica a coluna e os eventos de checkpoints. Essa cobertura é necessária porque a criação de um banco novo, isoladamente, não testa a atualização de uma instalação existente.
