# Continuidade da visualização ao vivo

O zoom digital pode ser verificado com `run-code --filename tests/live-continuity/digital-zoom.cjs` na mesma sessão. O ensaio usa WebRTC local e API simulada; verifica a lupa junto ao fechamento, ampliação de 1× a 8×, roda do mouse, arraste da imagem sem mover o card, teclado, restauração, isolamento entre câmeras, maximização e troca de câmera. Confirma também que ampliar e mover a imagem preservam os elementos de vídeo e as transmissões, sem abrir sessões adicionais. A captura fica em `output/playwright/live-digital-zoom.png`.

Ensaio com os componentes reais `LiveWorkspace` e `LiveViewProvider`, reconciliação do React e decodificação nativa de vídeo gerado por canvas no Chromium. A API é simulada; não exige login nem conecta a câmeras. No modo padrão, a sinalização WebRTC também é simulada. Abrir a URL com `#native` usa dois peers WebRTC reais no navegador, incluindo SDP, transporte RTP e estatísticas reais de recepção. Não mede codecs de equipamentos nem capacidade do servidor.

Na pasta `vms-frontend`, usando PowerShell:

```powershell
npx --yes esbuild@0.28.2 tests/live-continuity/fixture.tsx --bundle --outfile=tests/live-continuity/.generated/fixture.js --tsconfig=tsconfig.json --define:process.env.NODE_ENV='"production"'
node tests/live-continuity/serve.mjs
```

Com o servidor acima ativo, em outro terminal na mesma pasta:

```powershell
New-Item -ItemType Directory -Force output/playwright/live-continuity
npx --yes --package @playwright/cli playwright-cli -s=live-continuity open http://127.0.0.1:13110
npx --yes --package @playwright/cli playwright-cli -s=live-continuity run-code --filename tests/live-continuity/check.cjs
npx --yes --package @playwright/cli playwright-cli -s=live-continuity close
```

O comando `run-code` retorna erro se uma asserção falhar. O caso original falhava com quatro sessões excluídas e quatro abertas ao maximizar; deve preservar as quatro originais e abrir somente uma adicional. Também verifica transições entre faixas, limite de uma preparação adicional, falha e repetição de qualidade, perfil explícito, cancelamento e resposta tardia, mudança de câmera/fonte, atualização de catálogo, watchdog com vídeos ocultos, troca de tenant e revogação de acesso. As capturas ficam em `output/playwright/live-continuity/`.

Para repetir com transporte WebRTC nativo, execute `goto http://127.0.0.1:13110/#native` na mesma sessão antes de `run-code`. O ensaio preserva esse modo ao recarregar a página.

O ensaio de visualizações salvas usa WebRTC nativo e a API simulada para verificar salvamento sem reconexão, restauração da ordem e do mosaico maximizado preservando players e transmissões, e isolamento ao trocar de tenant:

```powershell
npx --yes --package @playwright/cli playwright-cli -s=live-continuity run-code --filename tests/live-continuity/saved-views.cjs
```

Os testes de serialização das posições, layouts vazios e parciais, perfis e identidades repetidas podem ser executados com `npm run test:live-mosaic`.

Os ajustes do painel são verificados com `run-code --filename tests/live-continuity/panel.cjs`: destaque de todas as câmeras do mosaico, remoção e duplicatas, mudança de layout, recolhimento pelo cabeçalho sem sobrepor o fechamento das câmeras, busca de visualizações e linhas contendo apenas o nome. As capturas ficam em `output/playwright/live-panel-views.png` e `output/playwright/live-panel-cameras.png`.

A regressão de fallback por falha real de sinalização usa `run-code --filename tests/live-continuity/fallback.cjs`. Verifica cinco câmeras com WHEP 503 no perfil automático, recuperação no próximo perfil da política, encerramento das sessões falhas, esgotamento sem repetição infinita, respeito ao perfil manual e bloqueio após perda de permissão. Esse ensaio usa API e sinalização simuladas, com decodificação de vídeo real no navegador.

Para verificar o arraste entre cards, execute `run-code --filename tests/live-continuity/drag.cjs` na mesma sessão. Esse ensaio usa WebRTC nativo e API simulada; verifica movimento para posição vazia, troca entre ocupadas, continuidade dos vídeos, perfil explícito, câmeras duplicadas, cancelamento, menu, maximização e fechamento após a troca. As capturas são `output/playwright/live-drag-target.png` e `output/playwright/live-drag-result.png`.
