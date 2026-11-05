# Validação do formulário de câmeras

Fixture do componente real `CamerasWorkspace`, com o tema do Astra e respostas de API simuladas em memória. Não acessa a instalação nem câmeras físicas.

Na pasta `frontend`, gere a fixture e mantenha o servidor em execução:

```powershell
node tests/camera-form/build.mjs
node tests/camera-form/serve.mjs
```

Na raiz `projeto-vms`, execute o roteiro com Playwright CLI:

```powershell
New-Item -ItemType Directory -Force output/playwright/camera-form
npx --yes --package @playwright/cli playwright-cli -s=camera-form open http://127.0.0.1:13119
npx --yes --package @playwright/cli playwright-cli -s=camera-form run-code --filename frontend/tests/camera-form/check.cjs
npx --yes --package @playwright/cli playwright-cli -s=camera-form close
```

O roteiro cobre cadastro RTSP e Dahua, Hikvision quando habilitado, payloads de criação e edição, conexão principal em Informações, substreams em Streams, validação e foco de campos em outra seção, preservação de resultados entre seções, senhas salvas, consulta de perfis, erros, rejeição de consulta desatualizada, troca de tipo de conexão, Diagnóstico, restrições de site, teclado e restauração de foco. Também verifica temas claro/escuro e rolagem nas larguras de 390 e 320 px. As capturas ficam em `output/playwright/camera-form` na raiz.

Recarregar a página reinicia os dados da fixture. A API real e a recepção de vídeo pertencem aos testes do backend.
