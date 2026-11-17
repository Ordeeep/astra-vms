# Barra de gravações do Ao vivo

Na pasta `frontend`, execute `npm run test:live-recording`.

Requer Chrome e FFmpeg no PATH; `VMS_TEST_FFMPEG` permite informar o executável. O build usa Webpack/SWC do Next já instalado e gera um vídeo H.264 sintético de 12 segundos em HLS/fMP4. O Chrome decodifica os bytes reais com Hls.js; as APIs e as transmissões ao vivo usam dados sintéticos. Nenhum cadastro do ambiente é alterado e não é necessário iniciar um servidor de testes.

O cenário usa `LiveWorkspace`, seu provider, o menu de contexto, a barra e o player reais. Verifica isolamento do quadro mesmo com câmera repetida, abertura sem sessão, calendário com disponibilidade e falhas, zoom e navegação, busca ao concluir o arraste, velocidade, pausa, passagem entre sessões e através da meia-noite, lacunas, revogação, respostas atrasadas, tela cheia, recolhimento e layouts nos temas claro/escuro. O navegador usa Asia/Tokyo e a câmera America/Sao_Paulo para detectar dependência indevida do fuso do navegador.

As capturas ficam em `output/playwright/live-recording` na raiz do projeto. São evidências locais geradas, sem dados pessoais.

`npm run test:recording-timeline` verifica separadamente calendário civil, horários inexistentes/ambíguos em mudanças de fuso, ancoragem e limites de zoom e mapeamento do tempo do vídeo.

O teste Go `TestRecordingAvailabilityUsesPlayablePartsAndHistoricalAuthorization` exige `VMS_TEST_DB` e usa schema exclusivo, removido ao terminar. Ele cobre agregação dos intervalos, lacunas internas, escopo histórico, limite de dois dias, datas mensais no fuso atual, extremos exclusivos, horário de verão, descarte e revogação. A suíte de interface complementa esses testes; sua API simulada não comprova captura de uma câmera física.
