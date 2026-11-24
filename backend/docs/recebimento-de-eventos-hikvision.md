# Recebimento de eventos de câmeras Hikvision

Referência para o desenho do coletor de eventos do Astra. As páginas abaixo correspondem à posição no PDF *ISAPI Network Cameras – Pro Series (EasyIP)* fornecido para o projeto. O comportamento em câmeras físicas ainda precisa de homologação por modelo e firmware.

A [ADR 0018](../../docs/adr/0018-coleta-supervisionada-de-eventos-de-camera.md) registra as escolhas do piloto para o canal, a continuidade da coleta e a verificação das regras.

## Como o evento chega ao Astra

| Modo do manual | Quem inicia | Como acompanhar |
| --- | --- | --- |
| Arming sem assinatura, §§ 4.5.1 e 5.4, pp. 28–29 e 43–46 | O Astra abre `GET /ISAPI/Event/notification/alertStream` e lê os eventos emitidos pela câmera. | O manual orienta encerrar e reabrir a conexão se passar o prazo de heartbeat sem receber mensagem. |
| Arming com assinatura, §§ 4.5.1.2 e 20.1.6, pp. 30–31 e 460–462 | O Astra verifica `isSupportSubscribeEvent` em `GET /ISAPI/System/capabilities`, consulta `GET /ISAPI/Event/notification/subscribeEventCap` e abre `POST /ISAPI/Event/notification/subscribeEvent` com os filtros suportados. | A resposta à assinatura, o heartbeat e os alertas chegam na conexão. O equipamento informa as opções de assinatura, inclusive o intervalo de heartbeat quando disponível. |
| Listening, §§ 4.5.2 e 6.3, pp. 32–34 e 88–90 | A câmera faz POST para o endereço de um servidor configurado em `httpHosts`. | O fluxo básico é descrito sem heartbeat. Quando o modelo permite, o Astra pode reler o destino e pedir um teste por `POST /ISAPI/Event/notification/httpHosts/<hostID>/test`. A chegada do teste ao Astra confirma o caminho naquele instante. |

Na conexão persistente, os exemplos das pp. 29–31 e 45–47 separam as partes de cada mensagem pelo delimitador multipart informado na resposta. O tipo de conteúdo de cada parte distingue XML, JSON e imagem. A resposta inicial à assinatura e o heartbeat são mensagens de controle do protocolo. O exemplo de assinatura da p. 47 usa `eventType=heartBeat`.

## O que uma interrupção permite afirmar

Uma conexão encerrada ou sem mensagens além do prazo esperado indica uma interrupção na supervisão. O último sinal válido e a primeira mensagem válida após a reconexão delimitam um período de cobertura incerta. A quantidade de alertas gerados nesse período depende de evidência da câmera. Um heartbeat recebido com sucesso também pode coexistir com uma regra analítica desabilitada ou fora de sua agenda.

Há recursos opcionais de reposição. A p. 462 associa o identificador de assinatura fornecido pelo cliente (`subscribeEventID`) à reposição automática de eventos. Nas pp. 741 e 746, o esquema de `httpHosts` inclui `httpBroken` e opções de assinatura com heartbeat. Para qualificar um modelo, é preciso provocar um alerta durante uma desconexão controlada e verificar se ele chega ao Astra após o restabelecimento, em cada modo e tipo de evento que se pretende oferecer.

## Como verificar uma regra esperada

Para intrusão, `GET /ISAPI/Smart/FieldDetection/<channelID>` informa a habilitação do analítico no canal e pode listar as regiões com seu ID e estado. Há também consulta por região (§ 20.13.1, pp. 1471–1472 e 1475–1478). Para cruzamento de linha, `GET /ISAPI/Smart/LineDetection` lista os canais e pode trazer o ID e a habilitação de cada linha. A consulta `GET /ISAPI/Smart/LineDetection/<channelID>/lineItem/<ruleID>` lê a linha individual (§ 20.13.2, pp. 1502–1503 e 1506–1507).

Para atribuir uma desabilitação a uma linha ou região específica, a leitura precisa identificar aquele item. A omissão de uma lista opcional ou uma consulta sem permissão deixa o estado do item indeterminado.

As agendas documentadas nas pp. 1455–1456 e 1486–1487 são por tipo de analítico e canal. Assim, a agenda do canal pode suspender vários perímetros juntos. Essa informação serve de referência para a etapa futura de supervisão por horário. O piloto verifica a habilitação das regras confirmadas e recebe os alertas emitidos pela câmera a qualquer hora.

## Encaixe no Astra

A [ADR 0017](../../docs/adr/0017-eventos-analiticos-individuais.md) define os dois primeiros tipos de evento. O adaptador Hikvision pode traduzir `fielddetection` para intrusão (§ 20.13.1.25, pp. 1480–1481) e `linedetection` para cruzamento de linha (§ 20.13.2.22, pp. 1509–1510). Os exemplos incluem estado do alerta, horário da câmera e contador opcional de envios. Cada novo recebimento da câmera tem registro próprio. Uma nova tentativa interna de gravar o mesmo recebimento deve conservar sua identidade e o instante em que o Astra o recebeu.

A [ADR 0010](../../docs/adr/0010-disponibilidade-de-camera-baseada-em-video.md) define a disponibilidade da câmera a partir do vídeo. A câmera pode continuar online durante uma falha de recepção de eventos. As [integrações por adaptador](../../docs/adr/0005-integracoes-de-camera-e-pipeline-de-midia.md) já oferecem o ponto de extensão para isolar as diferenças de protocolo entre fabricantes.

## Ensaios do piloto

### Operação da implementação

O serviço `astra-events` no Linux e `VMS-Events` no Windows guarda a fila e a última configuração válida no diretório privado `events` da instalação. O Core se conecta a `127.0.0.1:19092` usando o segredo de serviço da instalação. Guarde o segredo junto ao backup protegido da fila. A sincronização ocorre a cada cinco segundos. O Core confirma os itens depois de concluir a transação no PostgreSQL.

Monitore `GET /api/v1/health/event-collector` no Core. HTTP 200 indica contato nos últimos 20 segundos e ausência de pressão da fila. HTTP 503 indica contato vencido, pressão ou indisponibilidade do banco. A resposta de saúde informa `connected`, `lastContact` e `queuePressure` quando o banco está acessível. Em **Eventos**, os operadores podem consultar as falhas abertas e encerradas no próprio escopo.

O orçamento lógico padrão da fila é 1 GiB. O argumento `--queue-bytes` ajusta o total e `--reservations` recebe o caminho de um arquivo JSON que associa o ID de cada tenant à sua reserva em bytes, por exemplo `{"tenant-a":268435456}`. O restante é dividido entre os tenants sem reserva fixa. A pressão é sinalizada a partir de 80% da cota de um tenant. Para dimensionar as 24 horas, meça o crescimento durante uma interrupção da entrega sob carga representativa, incluindo picos. Provisione também espaço em disco para páginas e índices da fila e para os avisos de saturação. Os itens confirmados liberam capacidade para reutilização.

Em **Câmeras**, a consulta de regras exibe o modelo e o firmware informados. Uma combinação nova aparece **Em validação**. Após ensaio físico, o responsável pode registrar a evidência no arquivo `event-qualifications.json` do diretório de dados do Core. Ele contém uma lista de objetos com `model` e `firmware` exatos, `types` com os tipos ensaiados (`intrusion` ou `line_crossing`), `supervision` (`channel` ou `rule`) e `reconnect` com o resultado observado. Os dados do simulador servem ao piloto de software. [Execução da câmera simulada](../../docs/development.md#simular-eventos-analíticos)

### Cenários de verificação

| Situação a reproduzir | Evidência a conferir |
| --- | --- |
| Manter a cena sem disparos e o canal aberto. | O sinal de vida mantém a recepção verificável durante o silêncio dos analíticos. |
| Gerar cruzamentos em duas linhas, com apenas uma delas confirmada para supervisão. | O histórico registra os alertas de ambas. Quando o modelo emitir encerramento, o registro correspondente identifica esse estado. |
| Desabilitar a linha confirmada mantendo o canal e o vídeo ativos. | A próxima consulta bem-sucedida do ciclo de cinco minutos identifica a linha desabilitada. Uma falha de leitura deve produzir diagnóstico de verificação indisponível. |
| Interromper apenas o canal de eventos, mantendo o vídeo. Provocar um disparo durante a queda e outro após a reconexão. | O worker tenta restabelecer o canal. O Astra apresenta os limites do período de cobertura incerta e registra o alerta posterior. Conferir separadamente o eventual reenvio do disparo durante a queda. |
| Indisponibilizar o Core ou o banco, manter o envio de alertas e reiniciar o worker durante a indisponibilidade. | Conferir a retomada com a última configuração válida e a entrega dos itens da fila após a recuperação, com o horário de recebimento e um registro por item. Medir o volume necessário para a meta de 24 horas. |
| Isolar o worker do Core, transferir a câmera do Site A para o B e produzir alertas antes e depois da transferência. | Ao esvaziar a fila, conferir com operadores restritos a cada site que cada um só vê os recebimentos do período em que a câmera estava no seu site. |
| Excluir a câmera no Core enquanto o worker isolado ainda recebe alertas e depois restabelecer a comunicação. | Os recebimentos anteriores à exclusão chegam ao histórico. O diagnóstico registra a coleta que continuou após o corte. |
| Usar uma câmera que informe a habilitação do analítico apenas por canal e gerar um cruzamento de linha. | O histórico recebe o alerta. O diagnóstico informa o estado do canal e a impossibilidade de verificar a linha individual. |
| Manter uma regra confirmada desabilitada por dois ciclos de verificação e depois reabilitá-la. | O resumo da área Eventos mostra uma única falha aberta para os operadores autorizados. Após a leitura que confirma a reabilitação, o Astra registra o restabelecimento. |
| Remover uma linha esperada e criar outra de mesmo nome com identidade distinta. Repetir com uma resposta de inventário incompleta. | Com o inventário completo, o Astra sinaliza a ausência da original e aguarda confirmação da substituta. Com o inventário incompleto, informa que a verificação está indisponível. Os alertas emitidos continuam entrando no histórico. |
| Isolar o worker do Core e levar um tenant ao limite disponível para ele enquanto outro tenant também envia alertas. | O segundo tenant consegue usar a própria reserva. Conferir o aviso de saturação e verificar os itens dos dois tenants depois que a entrega for retomada. |
| Verificar a retenção com uma falha aberta há mais de 90 dias e com falhas encerradas há menos e há mais de 90 dias. | A falha aberta permanece visível. A expiração das encerradas considera o horário do encerramento. |
| Ativar uma combinação nova de modelo e firmware e provocar os eventos e as interrupções previstos no ensaio. | A coleta funciona em validação. O resultado da homologação identifica a combinação ensaiada, os tipos de alerta recebidos, a granularidade da supervisão e o comportamento na reconexão. |
