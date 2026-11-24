# Coleta supervisionada de eventos de câmera

Decisão de desenho aprovada em 24/11/2026. Na primeira integração real, o Astra inicia e supervisiona um canal persistente com a câmera Hikvision para receber alertas de intrusão e cruzamento de linha. O adaptador usa assinatura seletiva quando a câmera suporta os tipos necessários e o fluxo geral de eventos quando precisa filtrar no Astra. Essa escolha permite acompanhar a recepção durante períodos sem disparos e reconectar quando a comunicação falha. O envio da câmera para um servidor configurado nela fica para outra etapa.

## Habilitação e supervisão das regras

O operador habilita a coleta por câmera e confirma as regras analíticas esperadas e seus horários de referência. O Astra registra alertas de intrusão e cruzamento de linha de todas as regiões e linhas da câmera, inclusive as que o operador ainda não selecionou para supervisão. Quando a câmera informa o encerramento de um alerta, o histórico identifica esse recebimento como encerramento. Os demais recebimentos conservam o significado informado pelo equipamento.

A cada cinco minutos, o Astra consulta a habilitação do analítico e das regras confirmadas nos modelos que expõem essa informação. Uma desabilitação observada gera um aviso para o operador investigar e corrigir. Uma falha de leitura deixa a verificação indisponível. O canal de eventos tem supervisão própria. Quando a câmera encerra a conexão, o worker tenta reconectar imediatamente. Quando a conexão permanece aberta e silenciosa, vale o prazo de heartbeat validado para aquele modelo.

Se uma leitura completa mostrar que a regra esperada desapareceu, o Astra indica **Regra esperada não encontrada**. Uma linha ou região criada com o mesmo nome exige confirmação do operador antes de substituir a regra esperada. Durante esse período, o Astra continua recebendo os alertas da câmera. Se a leitura for incompleta, o diagnóstico indica **Verificação indisponível**.

O piloto admite equipamentos que permitem verificar a habilitação do analítico apenas por canal. Neles, o Astra informa o estado observado do analítico no canal e identifica como indisponível a verificação da linha ou região individual. A recepção e o registro dos alertas continuam.

Os horários confirmados servem de referência operacional. A comparação automática com a agenda do equipamento e a aplicação de filtros de horário ficam para uma etapa posterior. O piloto registra os alertas recebidos inclusive fora do horário confirmado pelo operador. Uma regra desabilitada pode coexistir com um canal saudável, assim como o vídeo pode continuar disponível durante uma interrupção da recepção de eventos. A [ADR 0010](0010-disponibilidade-de-camera-baseada-em-video.md) define a disponibilidade de câmera pelo vídeo recebido.

A área **Eventos** reúne as falhas abertas de recepção e de supervisão das regras das câmeras acessíveis ao operador. O resumo informa desde quando a falha foi observada e identifica as regras afetadas quando conhecidas. Leituras sucessivas que confirmam a mesma falha mantêm um único registro aberto. Quando o Astra observa o restabelecimento, registra seu horário. A permissão **Visualizar eventos** permite consultar essas informações no escopo de tenant, site ou câmera concedido ao operador. **Gerenciar sites e câmeras** permite habilitar a coleta e mudar as regras esperadas no escopo autorizado.

Uma falha permanece visível enquanto estiver aberta, qualquer que seja sua duração. O histórico de falhas guarda o registro por 90 dias contados do encerramento. Para um evento analítico, o prazo de retenção começa no recebimento, conforme a [ADR 0017](0017-eventos-analiticos-individuais.md).

Uma perda de comunicação entre Core e coletor abre uma única falha, com as câmeras afetadas filtradas pelas permissões de quem consulta. O worker pode continuar recebendo enquanto está isolado. Após a reconexão, seus registros permitem distinguir entrega atrasada e interrupção do canal da câmera. Se o operador suspender a supervisão, o encerramento da falha registra esse motivo. A reativação exige nova leitura. A consulta de saúde da instalação informa o contato recente com o worker, o horário dessa informação e a pressão da fila.

## Continuidade da coleta e da entrega

Um worker de eventos em processo próprio mantém as sessões e grava os dados de cada alerta em uma fila local persistente. O piloto guarda os metadados do alerta. A inclusão de imagens fica para outra etapa. Se o worker reiniciar durante uma indisponibilidade do Core ou do PostgreSQL, ele retoma a coleta com a última configuração válida, protegida no servidor. Uma desativação que o worker ainda desconheça passa a valer nele assim que voltar a sincronizar.

A meta de dimensionamento da fila é sustentar 24 horas de indisponibilidade do Core ou do PostgreSQL com o volume de alertas medido no piloto. O limite físico preserva os itens já gravados em caso de saturação. O worker emite um aviso local mesmo com o Core indisponível e registra o intervalo em que a recepção ficou comprometida.

Cada tenant tem uma reserva de espaço na fila, dimensionada pelo volume de seus alertas. Mesmo durante um pico de recebimentos de uma empresa, as reservas das demais empresas e os itens já persistidos permanecem protegidos.

A entrega posterior preserva o horário do primeiro recebimento pelo Astra. Uma nova tentativa de entregar o mesmo item ao Core corresponde a um único registro no histórico. Um novo envio pela câmera constitui outro recebimento, conforme a [ADR 0017](0017-eventos-analiticos-individuais.md). Os detalhes do protocolo e os cenários de homologação estão na [referência Hikvision](../../backend/docs/recebimento-de-eventos-hikvision.md).

## Homologação por modelo e firmware

Uma combinação nova de modelo e firmware pode operar **em validação**. A homologação registra quais tipos de alerta foram recebidos em ensaio, até que nível foi possível verificar as regras e como o equipamento se comportou após uma desconexão.

Um modelo pode ser homologado para o piloto quando retoma o canal após uma interrupção, mesmo que o equipamento não reenvie alertas gerados durante a queda. O Astra mostra o período de cobertura incerta entre a última evidência válida de recepção e a primeira após o restabelecimento. Se houver reenvio parcial, a incerteza sobre os demais alertas do período permanece.

## Site e exclusão na entrega atrasada

O histórico usa o site em que a câmera estava cadastrada no instante em que o worker recebeu o alerta, conforme as mudanças de cadastro efetivadas no Core. Uma transferência às 10h atribui os recebimentos anteriores ao site de origem e os recebimentos a partir das 10h ao site de destino. Essa atribuição rege o acesso por site mesmo quando o worker estava isolado e entrega toda a fila depois da transferência.

O instante efetivo de exclusão no Core encerra o histórico operacional da câmera. Os alertas recebidos pelo worker antes desse instante continuam elegíveis para entrega e seguem a retenção da [ADR 0017](0017-eventos-analiticos-individuais.md). Se o worker continuar coletando com a configuração anterior, o Astra registra no diagnóstico o período de coleta após a exclusão. Os alertas recebidos a partir do instante de exclusão ficam fora do histórico operacional.
