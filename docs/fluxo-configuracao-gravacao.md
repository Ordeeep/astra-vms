# Configuração de gravação no cadastro da câmera

Desenho consolidado e implementação autorizada em 16/11/2026. O fluxo foi implementado conforme as decisões confirmadas durante a entrevista.

## Decisões confirmadas

- Esta etapa reorganiza o fluxo existente, preservando as regras atuais de gravação. Novas modalidades ficam para outra etapa.
- A configuração individual de gravação passa para o diálogo de adicionar/editar câmera: habilitação, perfil de vídeo, agenda e retenção específica ou herdada.
- A tela “Gravação e agenda” será retirada. Os controles gerais passam para uma nova área “Armazenamento”, nas configurações do sistema.
- A área de Armazenamento reúne os controles existentes de cota e retenção padrão do tenant, orçamento e expiração de exportações, redução de cota e destino de armazenamento do servidor. A mudança de localização preserva os respectivos escopos e permissões.
- O diálogo terá uma aba “Gravação”, ao lado de Informações, Streams e Diagnóstico, contendo habilitação, perfil, agenda e retenção.
- Na criação, a gravação permanece desabilitada por padrão, com perfil principal, agenda 24h e retenção herdada do tenant.
- O estado da gravação passa para a lista de Câmeras. A última escrita fica nos detalhes da câmera. O estado da gravação permanece distinto da disponibilidade de vídeo.
- Um único botão “Salvar” aplica as alterações de todas as abas na criação e na edição. O salvamento é atômico: falhas de cadastro ou gravação não aplicam alterações parciais, e os valores preenchidos permanecem no diálogo para correção.
- Operadores com permissão somente de gravação abrem a mesma janela diretamente na aba “Gravação”, com identificação da câmera para consulta e sem acesso à edição da conexão e dos streams.
- Se uma alteração dos streams remover ou substituir o perfil selecionado de uma gravação habilitada, o operador autorizado deve escolher outro perfil válido ou desabilitar explicitamente a gravação antes de salvar. Não haverá troca automática para o perfil principal.
- Sem perfis de vídeo disponíveis para seleção, o cadastro pode ser salvo com a gravação desabilitada. Habilitar a gravação requer um perfil válido.
- A configuração de gravação pode ser salva como habilitada mesmo quando o destino ou a cota de armazenamento ainda estiverem pendentes. A aba e a lista devem sinalizar o impedimento da captura, com acesso à área de Armazenamento para operadores autorizados.
- Quem pode editar streams, mas não gerir gravação, fica impedido de salvar uma mudança que invalide o perfil de uma gravação habilitada. A interface explica a necessidade de um operador com ambas as permissões; as demais edições permanecem permitidas.
- Trocar de aba preserva o preenchimento. Cancelar ou fechar com alterações não salvas exige confirmação de descarte; nada é aplicado antes de “Salvar”.
- Alterações concorrentes no cadastro ou na configuração de gravação bloqueiam o salvamento e preservam o preenchimento no diálogo. A interface informa o conflito e orienta reabrir a versão atual, confirmando o descarte do rascunho antes de fazê-lo.
- A nova área “Sistema > Armazenamento” terá duas seções: “Política do ambiente atual”, com cota, retenção, exportações e redução de cota conforme as permissões no tenant; e “Armazenamento do servidor”, com destino e limites globais apenas para o administrador global.

## Restrições do fluxo atual

- Agenda individual por câmera, no fuso do site; gravação contínua 24h ou por períodos semanais.
- Habilitação, agenda e perfil controlam a captura futura e preservam o acervo existente. Reduzir a retenção pode afetar o acervo e requer a confirmação de impacto existente.
- Edição do cadastro e gestão da gravação possuem permissões independentes. O novo ponto de entrada precisa atender operadores autorizados somente a configurar gravação.
- Disponibilidade da câmera e estado da gravação são informações distintas.
- O funcionamento da captura depende também da configuração do armazenamento e da política do tenant.
- A reorganização preserva as configurações das câmeras existentes e seu acervo. Os valores iniciais de criação não são reaplicados aos cadastros existentes.

## Fluxo consolidado

1. O operador abre uma câmera existente ou inicia um cadastro. As ações disponíveis respeitam suas permissões; quem só gerencia gravação entra diretamente nessa aba.
2. Na aba “Gravação”, escolhe habilitação, perfil, agenda 24h ou períodos semanais e retenção própria ou herdada. Pendências de perfis e armazenamento aparecem conforme as regras confirmadas acima. Os detalhes também apresentam a última escrita.
3. Ao clicar em “Salvar”, o sistema valida o conjunto, incluindo permissões, perfil selecionado, revisões e eventual confirmação de impacto da retenção. A aplicação é integral; falhas mantêm o preenchimento para correção.
4. A lista de Câmeras apresenta o estado real da gravação separadamente da disponibilidade de vídeo. Salvar a configuração não significa que a captura já começou.
5. Cota, retenção padrão, exportações e destino global são consultados ou configurados em “Sistema > Armazenamento”, respeitando o escopo de cada seção.

## Fechamento

O usuário confirmou o entendimento consolidado e autorizou a implementação. A configuração individual passou para a câmera, a antiga tela foi retirada da navegação e os controles gerais passaram para Sistema > Armazenamento. A reorganização não exige migração dos dados de gravação existentes.

## Implementação

- `frontend/src/components/cameras.tsx`: diálogo integrado, indicadores na lista, permissões, descarte e conflitos.
- `frontend/src/components/camera-recording-editor.tsx`: habilitação, perfil, agenda, retenção, estado e última escrita.
- `frontend/src/components/storage.tsx`: política do tenant, cota, exportações e armazenamento global.
- `frontend/src/components/nav-main.tsx` e `workspace.tsx`: navegação e proteção do rascunho ao sair da câmera.
- `backend/internal/app/camera_configuration.go` e `inventory.go`: criação e edição com gravação na mesma transação.
- `backend/internal/app/camera_recording.go`: validação compartilhada, revisões, permissões e confirmação de impacto da retenção. As rotas antigas de edição de fonte também preservam o perfil de gravação habilitado.
- `backend/internal/app/recording.go`: configuração independente compatível com a API existente e estado de gravação paginado, com pendências de destino/cota e fuso do site para operadores com acesso apenas à câmera.

## Validação

- Suíte completa do backend com PostgreSQL em banco exclusivo de testes: aprovada. Testes que exigem fontes RTSP e infraestrutura de mídia opcional continuam condicionados às respectivas variáveis.
- Integração específica em `backend/internal/app/camera_recording_test.go`: criação e edição atômicas, rollback em revisão inválida, permissões independentes, perfis pendentes e confirmação de retenção.
- Checagem TypeScript e build de produção Next.js: aprovados.
- Regressão da interface: `npm run test:camera-recording` na pasta `frontend`. Usa os componentes reais com API simulada e Chrome headless; instruções em `frontend/tests/camera-recording/README.md`.
- Inspeção das capturas de tela nos temas claro e escuro, incluindo 320 e 390 px. Evidências locais em `output/playwright/camera-recording`.
- Backend de desenvolvimento recompilado e reiniciado, com verificação de saúde aprovada em `https://localhost:18443/api/v1/health`.

A validação de interface usa dados sintéticos, e os testes HTTP usam schemas temporários em banco de laboratório. Esta etapa não comprova captura de câmeras físicas.

Este documento registra o desenho do fluxo. Decisões arquiteturais com trade-offs duradouros serão registradas em ADR quando houver uma escolha que justifique esse registro.
