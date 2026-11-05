# Validação da disponibilidade de câmeras

Fixture do componente real CamerasWorkspace com quatro estados e respostas de inventário simuladas em memória. Não acessa a instalação nem câmeras físicas.

Na pasta frontend:

```powershell
node tests/camera-availability/build.mjs
node tests/camera-availability/serve.mjs
```

O servidor usa http://127.0.0.1:13118. A partir da raiz projeto-vms, com o servidor ativo:

```powershell
New-Item -ItemType Directory -Force output/playwright/camera-availability
npx --yes --package @playwright/cli playwright-cli -s=camera-availability open http://127.0.0.1:13118
npx --yes --package @playwright/cli playwright-cli -s=camera-availability run-code --filename frontend/tests/camera-availability/check.cjs
npx --yes --package @playwright/cli playwright-cli -s=camera-availability close
```

O roteiro verifica os quatro estados, contadores, detalhes por perfil, manutenção do popover aberto durante polling, filtros, recuperação automática, consultas GET sem testes de câmera, validade local, falha de atualização, troca de tenant com resposta atrasada, temas claro/escuro e viewport de 390 px. As capturas ficam em output/playwright/camera-availability na raiz.

window.availabilityQA permite alterar resultados, simular falha de rede e atrasar respostas. Os prazos dos dados simulados expiram deliberadamente; recarregar a página reinicia a fixture. A API real e a recepção de vídeo são cobertas pelos testes do backend.
