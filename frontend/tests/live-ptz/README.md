# Integração PTZ no navegador

Fixture com `LiveViewProvider` e `LivePTZControls` reais e API simulada, sem conexão a equipamentos. Gera CSS com o tema e os estilos reais do controle. A primeira câmera oferece todos os recursos; a segunda oferece zoom e Home.

Na pasta `frontend`:

```powershell
node tests/live-ptz/build.mjs
node tests/live-ptz/serve.mjs
```

Em outro terminal, na mesma pasta:

```powershell
New-Item -ItemType Directory -Force output/playwright/live-ptz | Out-Null
npx --yes --package @playwright/cli playwright-cli -s=ptz-integration open http://127.0.0.1:13125
npx --yes --package @playwright/cli playwright-cli -s=ptz-integration run-code --filename tests/live-ptz/check.cjs
npx --yes --package @playwright/cli playwright-cli -s=ptz-integration run-code --filename tests/live-ptz/multi-input.cjs
npx --yes --package @playwright/cli playwright-cli -s=ptz-integration run-code --filename tests/live-ptz/collapse.cjs
npx --yes --package @playwright/cli playwright-cli -s=ptz-integration close
```

O primeiro ensaio verifica pressionar/soltar por teclado e ponteiro, velocidade, diagonal, renovação, retorno à posição inicial, perda de foco, capacidades parciais, maximização, resposta atrasada ao trocar de alvo, comando substituído, revogação e isolamento na troca de tenant. Salva capturas desktop/mobile em `output/playwright/live-ptz`.

O segundo verifica que soltar uma tecla antiga não interrompe um gesto mais recente do mouse e que uma confirmação atrasada após troca de tenant recebe parada imediata com o contexto original. A fixture não comprova comportamento mecânico nem compatibilidade de firmware.

O ensaio `collapse.cjs` abre a variante `?panel`, com a lista de câmeras e o painel recolhível reais, e mede a altura da página a cada frame durante quatro ciclos de recolhimento em desktop e mobile. Rejeita crescimento da página ou saltos na altura do conteúdo durante a animação. A regressão reproduzia crescimento de 720 para 799 px porque a região acessível de status, posicionada absolutamente, tinha a barra lateral como referência e escapava do recorte do painel. O contêiner PTZ estabelece agora essa referência de posicionamento, preservando a altura natural dos controles.
