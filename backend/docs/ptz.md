# Controle PTZ

O Ao vivo opera um contrato comum de PTZ. A integração de câmera pode implementar a interface opcional `camera.PTZAdapter`, que abre um `PTZDriver` com consulta de recursos, movimento, parada e retorno à posição inicial. A implementação inicial é Hikvision; RTSP manual e Dahua ainda não oferecem PTZ. Incluir outra implementação não exige condições por fabricante na interface ou no serviço `internal/ptz`.

## Autorização e recursos

`ptz.control` é uma concessão independente, por tenant ou site. Também é necessário ter `live` para a mesma câmera: concessões em sites diferentes não se combinam para autorizar recursos distintos. Administradores globais precisam dessas concessões. Grupos existentes e a conta inicial não recebem automaticamente a nova ação; conceder explicitamente em Grupos.

O campo `Camera.capabilities` continua representando permissões efetivas. Os recursos físicos oferecidos pelo adaptador são consultados separadamente: `panTilt`, `zoom` e `home`. A interface identifica a câmera do quadro ativo, prioriza a maximizada e desabilita controles sem recurso ou autorização.

## API do Astra

Todas as rotas usam o contexto de visualização da aba e a autenticação compartilhada. Escritas exigem CSRF. O prefixo é `/api/v1/tenants/{tenant}/cameras/{camera}/ptz`.

| Método e sufixo | Operação |
| --- | --- |
| `GET` | Consulta recursos e a revisão opaca da fonte/conexão/site. |
| `POST /commands` | Inicia movimento ou solicita retorno à posição inicial; retorna `id` e `expiresInMs`. |
| `PUT /commands/{id}` | Renova apenas o comando ainda atual da mesma sessão e aba. |
| `DELETE /commands/{id}` | Encerra apenas o comando pertencente à mesma sessão e aba; uma parada antiga é inócua. |

O POST recebe `revision` obtida no GET e velocidades normalizadas de -1 a 1 em `pan`, `tilt` e `zoom`. Por exemplo, `{"revision":"<revisão>","pan":-0.6,"tilt":0.6}` solicita uma diagonal para cima e esquerda. O retorno à posição inicial usa `{"revision":"<revisão>","home":true}` sem velocidades. Vetor nulo, valores fora da faixa e combinação de Home com movimento são rejeitados.

A revisão evita aplicar um comando preparado para a conexão anterior a uma fonte substituída ou movida de site. A autorização e as linhas de inventário ficam protegidas durante a operação; comandos usam uma trava compartilhada de leitura de autorização, enquanto alterações de permissões e inventário usam a trava exclusiva existente. Assim, dispositivos diferentes podem receber comandos simultaneamente sem perder a consistência das concessões.

## Movimento e concorrência

Setas e zoom operam enquanto pressionados, por ponteiro ou teclado. Soltar, cancelar o ponteiro, perder foco, ocultar/sair da página, trocar de câmera/contexto ou perder permissão encerra o comando. O botão de casa solicita retorno à posição inicial existente e mantém a supervisão enquanto esse comando for o atual, até outro comando ou encerramento do contexto; não cria nem sobrescreve posições. Um clique de tecnologia assistiva em seta/zoom, sem eventos de pressionar/soltar, produz um pulso curto supervisionado.

O navegador renova o comando a cada 400 ms. Sem renovação, o comando expira em 2 segundos; o supervisor verifica vencimentos a cada 150 ms e solicita parada. Esse prazo é de expiração do comando, não uma promessa do instante da parada física: a requisição à câmera e eventuais falhas de comunicação acrescentam tempo. Paradas que falham permanecem pendentes para repetição; renovações não reativam comandos encerrados. Encerramento normal do backend também solicita parada dos comandos ativos.

Prevalece o comando mais recente aceito por câmera, inclusive entre operadores, sessões e abas. O serviço serializa requisições ao mesmo equipamento, identifica comandos por tokens e não deixa uma parada, renovação ou expiração antiga afetar seu sucessor. Uma aba que perde o controle não tenta retomá-lo automaticamente: é preciso soltar e pressionar novamente. O frontend também distingue gestos sobrepostos de teclado e ponteiro.

Cada comando conserva o driver da conexão original para a parada, incluindo as credenciais protegidas. Uma troca da fonte não redireciona a parada ao equipamento novo. Respostas atrasadas ao início são usadas apenas para encerrar aquele comando, com o contexto original da aba. A auditoria registra o início aceito com vetor ou Home, sem credenciais; renovações não geram eventos repetidos.

## Validação e limites

Testes do adaptador usam servidor HTTP simulado; o serviço tem testes de comandos sobrepostos, origem preservada, expiração, erro ambíguo de movimento e repetição de parada. Testes da API usam PostgreSQL de laboratório em namespaces isolados e dois operadores, cobrindo concessões, contexto, tenant, revogação e o supervisor real. A fixture Playwright usa os componentes reais, com API simulada, para gestos, respostas atrasadas, câmera maximizada, capacidades parciais e troca de tenant.

O registro completo de execução fica em `.scratch/ptz-integration/spec.md` na raiz do projeto. A homologação física permanece pendente por modelo e firmware. O supervisor do servidor não garante parada durante queda abrupta do processo ou perda da rede até o equipamento; não foi presumido suporte a expiração embarcada em todos os modelos. Presets, patrulhas, definição de posição inicial, foco, íris, NVR/DVR e implementação Dahua não integram esta entrega.
