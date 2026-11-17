# Configuração de gravação por câmera

Fixture dos componentes reais `CamerasWorkspace` e `StorageWorkspace`, com respostas de API simuladas em memória. Não acessa a instalação nem câmeras físicas.

Na pasta `frontend`, execute `npm run test:camera-recording`. O roteiro usa o Chrome instalado em modo headless. As capturas ficam em `output/playwright/camera-recording` na raiz do repositório.

O roteiro cobre criação com gravação desabilitada, cadastro sem perfis, agenda atravessando meia-noite, preservação entre abas, payload único de salvamento, resolução explícita de perfil removido, descarte, conflito, falha de leitura, confirmação de impacto da retenção, operação somente de gravação e consulta somente de reprodução. Também verifica a separação dos controles de armazenamento e o layout em 1440, 390 e 320 px nos temas claro e escuro.

Para inspeção manual, gere a fixture com `node tests/camera-recording/build.mjs` e inicie `node tests/camera-recording/serve.mjs`. Abra `http://127.0.0.1:13129`. Os parâmetros `?mode=recording-only`, `?mode=inventory-only`, `?mode=viewer` e `?mode=pending` selecionam os cenários.

Os testes HTTP com PostgreSQL ficam em `backend/internal/app/camera_recording_test.go`. Eles verificam atomicidade, revisões independentes, permissões, preservação de gravação habilitada, perfis pendentes e confirmação de retenção. Use um banco exclusivo de testes conforme `backend/docs/validation.md`.
