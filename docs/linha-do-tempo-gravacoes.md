# Linha do tempo de gravações no Ao vivo

Desenho consolidado e aprovado em 17/11/2026 com a habilidade grill-with-docs. O usuário confirmou o entendimento e autorizou a implementação, concluída conforme o fluxo abaixo. As decisões registram o pedido e as respostas de três rodadas de entrevista.

## Revisão de otimização — 19/11/2026

Desenho aprovado com grill-with-docs em 19/11/2026. O usuário confirmou Q1 a Q6 e autorizou a implementação, preservando as regras anteriores de navegação e reprodução.

### Decisões confirmadas

- A faixa azul deve crescer conforme novos trechos se tornarem efetivamente disponíveis para reprodução. A meta aceita é de até cinco segundos entre captura e disponibilidade reproduzível/visual em operação normal.
- As faixas já carregadas permanecem durante arraste e zoom. Consultas devem buscar os períodos ainda desconhecidos, reaproveitando os conhecidos.
- A chegada de novas gravações continua atualizando as faixas durante o gesto de navegação; o arraste não pode bloquear ou descartar essas atualizações.
- O canal SSE existente será reaproveitado, com identificação da câmera e do período afetado, agrupamento de notificações, reconciliação após reconexão e conferência periódica de baixa frequência como recuperação.
- Os arquivos continuam com aproximadamente 30 segundos, disponibilizando as partes completas já persistidas antes de seu fechamento.
- Fontes que impeçam a meta de atraso conservam sua disponibilidade real, com indicação de atraso e sem reconfiguração automática da câmera nem transcodificação adicional para cumprir a meta.

### Evidências anteriores à implementação

- Hover apenas atualiza o estado local da régua. Arraste e zoom alteram a janela temporal visível; a consulta aguarda 140 ms sem nova alteração, e há atualização periódica a cada 30 segundos. Portanto, movimento de ponteiro e requisição HTTP não têm relação de um para um.
- O painel associa os intervalos à janela temporal exata e à revisão. Quando essa chave muda, fornece uma lista vazia à régua até receber a nova resposta. Esse caminho explica o apagamento das faixas durante a navegação por inspeção estática; não houve reprodução no navegador nesta revisão.
- As respostas de consultas antigas são ignoradas, mas essas requisições não são canceladas. Não há cache de blocos temporais reutilizáveis nesse painel.
- Já existe um EventSource compartilhado por contexto de visualização. O painel reage aos eventos, mas invalida também consultas após alterações em outras câmeras. O evento ArchiveChanged expõe o identificador do segmento, sem a identidade da câmera no JSON; o filtro por câmera exige complementar esse contrato.
- O serviço de mídia fecha normalmente os segmentos a cada 30 segundos; apenas segmentos finalizados são publicados no catálogo. O backend importa esse catálogo a cada dois segundos. Trocar o transporte de eventos não elimina essa espera até a disponibilização do vídeo.
- A sessão de reprodução tem renovação HTTP a cada 600 ms, separada das consultas da linha do tempo, e expiração de dez segundos. A autorização de cada entrega de mídia é independente, inclusive com rechecagem a cada 400 ms em transferências em andamento. A inspeção estática permite propor renovação a cada dois segundos, mantendo a expiração e as verificações de autorização; o comportamento precisa ser validado com pausa, revogação, lentidão e expiração. O custo real ainda não foi medido nesta revisão.

Fontes: `frontend/src/components/live-recording-panel.tsx`, `recording-ruler.tsx`, `recording-video.tsx` e `workspace.tsx`; `backend/internal/app/recording.go`; `backend/internal/media/archive_service.go`; `backend/internal/storage/index.go`.

### Implementação da otimização

- O cache pertence ao painel da câmera no contexto autorizado e conserva até dezesseis dias usados recentemente, além das páginas visíveis durante sua troca. Pan e zoom reaproveitam dias conhecidos. Regiões novas são consultadas após 140 ms; consultas de páginas abandonadas são canceladas. Uma única consulta por página fica em andamento, com reconciliação posterior quando chegam outros eventos.
- Notificações de acervo identificam câmera, revisão e intervalo. Eventos da outra câmera não refazem consultas. Mudanças de gravação atualizam somente a região afetada dentro de um dia em cache, preservando o restante; mudanças de retenção podem reduzir as faixas. A atualização por SSE é agrupada em 120 ms e continua durante arraste, sem deslocar a janela manualmente escolhida.
- A conferência completa ocorre a cada sessenta segundos e após reconexão. Erros transitórios e desconexão preservam as faixas conhecidas e indicam falha/desatualização. Revogação ou resincronização de autorização elimina disponibilidade e reprodução; respostas abortadas não restauram esses dados.
- O calendário só consulta enquanto aberto, preserva o cancelamento e não refaz consultas quando um evento apenas estende um dia já marcado. Mudanças relevantes são agrupadas, com recuperação periódica de sessenta segundos.
- Cada checkpoint durável publica uma revisão do mesmo objeto. A leitura abre atomicamente metadados e prefixo confirmado, com tamanho fixo inclusive para Range e SeekEnd. O Core importa páginas a cada 500 ms e drena lotes, aplicando revisões monotônicas sem reativar registros descartados. Checkpoints inválidos após reinício geram remoção no catálogo sem liberar indevidamente a cota.
- A exportação valida o hash do prefixo da prévia, mesmo quando partes posteriores são gravadas. A conversão para o navegador mantém a chave de cache por parte e libera a proteção de leitura também nos acertos de cache.
- A sessão de reprodução é renovada a cada dois segundos: frequência nominal de cem para trinta requisições por minuto por sessão, sem aumentar a expiração de dez segundos ou relaxar as verificações de autorização da mídia. Só a renovação recebe timeout de seis segundos para não aguardar além da validade; demais operações conservam seu timeout anterior.
- A disponibilidade recente informa atraso observado quando a captura continua recebendo quadros e sua cauda ou seus fragmentos excedem a meta. A câmera conserva seus parâmetros. A escolha estrutural está registrada no ADR `0013-disponibilidade-das-partes-em-gravacao.md`.

### Validação da otimização — 19/11/2026

- Treze testes de calendário, navegação, relógio e cache aprovados; TypeScript e build de produção com Webpack aprovados.
- Dezessete cenários no Chrome com componentes reais e HLS sintético decodificado aprovados. Incluem faixa preservada sem consulta no pan conhecido, atualização SSE durante o arraste, filtro de outra câmera, reconexão sem apagar faixas, pausa com renovação a cada dois segundos, revogação e layouts de 1440, 390 e 320 px nos dois temas.
- A integração com PostgreSQL e RTSP sintético de GOP de um segundo tornou o primeiro trecho reproduzível em **1,74 segundo**, antes de fechar o arquivo. A disponibilidade cresceu, os bytes foram decodificados e a exportação preservou a prévia original enquanto a captura continuava. Essa rodada manteve a sincronização de disco do PostgreSQL. O resultado valida esse laboratório e não representa uma garantia para câmeras físicas ou todas as cargas.
- O pacote completo de aplicação passou isoladamente com PostgreSQL descartável; a fonte sintética voltou a ficar reproduzível em **2,24 segundos**. Essa rodada usou sincronização de disco desativada somente na instância descartável do banco, sem alterar a persistência do armazenamento de vídeo. O ensaio opcional de oito fontes não foi habilitado.
- O pacote de mídia passou na execução agregada. Os testes de HEVC e H.264 com quadros reordenados foram ampliados para consultar arquivo ainda aberto e repetidos com aprovação. HTTP Range, leitura limitada, prefixo estável de exportação, autorização, remoção e recuperação foram exercitados.
- O pacote completo de armazenamento passou na rodada inicial (471 segundos). A rodada agregada concorrente excedeu o limite de 600 segundos durante o teste de escala e causou timeouts na limpeza de schemas; os testes de snapshot, crescimento, corrupção e recuperação passaram nessa rodada. A aplicação foi então repetida isoladamente, com o resultado registrado acima.

Essa rodada inicial não reiniciou os processos do VMS. A ativação posterior está registrada abaixo; os resultados sintéticos não constituem homologação de todas as câmeras ou cargas.

### Ativação no ambiente local — 19/11/2026

O usuário autorizou o reinício do Core e do Worker. Na preparação, foi identificada e corrigida a necessidade de uma migração incremental para instalações existentes: a versão 9 adiciona `catalog_sequence` e atualiza o contrato de eventos, sem depender da recriação do schema inicial. O teste de atualização de uma instalação na versão 8 reproduziu a ausência da coluna antes da correção e passou depois, junto com os testes de catálogo, credenciais, reinício e migrações anteriores.

Core e Worker foram reiniciados às **16:57 de 19/11/2026**, no fuso America/Sao_Paulo, mantendo os diretórios, credenciais e configurações existentes. A API retornou `status: ok`, a interface respondeu HTTP 200 e a única câmera com gravação habilitada retomou a captura. O banco confirmou a migração 9, revisões de checkpoints e eventos com identificação da câmera e do intervalo. O catálogo do Worker expôs um objeto com `Finalized: false` enquanto o Core já indexava suas partes completas.

Essa verificação observou captura real e publicação de disponibilidade no ambiente local. Não incluiu inspeção visual de reprodução da câmera no navegador nem homologação de outras fontes. Nenhum parâmetro da câmera ou agenda de gravação foi alterado.

## Fluxo implementado

1. No Ao vivo, o operador abre Gravação pelo menu de um quadro. Somente esse quadro entra em modo de gravação, mesmo que a mesma câmera esteja repetida no mosaico. O quadro aguarda a escolha de horário e não inicia vídeo automaticamente.
2. A régua abre com uma visão de 24 horas do dia atual no fuso do site atual da câmera. O calendário distingue dias com gravações autorizadas, dias vazios, seleção, carregamento e falha. A disponibilidade é baseada em imagens do acervo, não na agenda de captura.
3. O operador navega arrastando a régua ou escolhendo uma data. Isso não muda as imagens que já estão tocando. A janela pode atravessar datas livremente; o zoom vai de um minuto até dois dias, com botões +/−, slider entre os botões e roda ancorada no horário sob o ponteiro. O slider usa escala logarítmica e acompanha o zoom aplicado pelos demais controles. Marcas e rótulos se adaptam à escala, sem sobreposição.
4. O calendário representa a data de navegação, distinta da data/hora da reprodução. A data de navegação acompanha o dia no centro da régua; escolher uma data abre diretamente a janela de 24 horas a partir de 00:00 do dia escolhido, sem botão de confirmação. O calendário é aberto pelo próprio card quadrado do horário (hora em destaque e data abaixo), com acesso por clique, Enter ou Espaço; o botão de data separado foi removido. O calendário tem 224 px de largura e células compactas, sem a legenda dos dias em azul; os indicadores de disponibilidade, carregamento e erro permanecem. O relógio nos controles continua indicando o instante das imagens. A régua identifica as viradas de data e o tooltip informa data e HH:mm:ss.
5. Clicar em um horário gravado na régua inicia a primeira reprodução. Depois disso, os deslocamentos da posição preservam o estado de pausa ou reprodução. Arrastar o marcador busca imagens, enquanto arrastar a régua apenas navega. A consulta do vídeo é efetivada ao concluir o gesto do marcador, sem criar uma sessão a cada movimento do mouse.
6. O vídeo percorre automaticamente as partes consecutivas de um trecho contínuo, inclusive através da meia-noite. Ao atingir uma lacuna, para e oferece o próximo trecho. Ao alcançar o fim das imagens disponíveis, informa isso; fechar o painel retorna ao vivo. Um clique ou salto que caia em uma lacuna informa a ausência naquele instante, sem deslocar silenciosamente a escolha para outro horário.
7. Reproduzir/pausar, −10s/+10s, trecho contínuo anterior/próximo e velocidades de −8× a 8× controlam o quadro selecionado. Os botões de zoom ficam à direita, sem indicador numérico de escala. Navegar manualmente suspende o acompanhamento automático; saltar para o trecho anterior/próximo o retoma.
8. O divisor amplia em conjunto o card de data/hora e a coluna do nome da câmera, mantendo os dois alinhados, e se estende até o topo da data da régua. O ajuste é conservado nas próximas aberturas neste navegador. O painel funciona no modo normal, com quadro maximizado e em Video Wall. O recolhimento foi removido; Fechar fica ao lado do zoom, encerra a reprodução e restaura o vídeo ao vivo. O status fixo acima dos controles foi removido; falhas de consulta ou reprodução aparecem em toast com opção de tentar novamente, quando aplicável.
9. Trocar a câmera controlada encerra a reprodução anterior. Remover ou ocultar o quadro por mudança de layout, sair do Ao vivo ou mudar de contexto encerra a sessão. Trocar apenas a aba do navegador não é tratado como remoção do quadro.
10. O fuso é sempre o do site atual da câmera, também para o acervo de sites anteriores. Os instantes das gravações e as autorizações pelo site de origem são preservados.

## Simplificação visual de 17/11/2026

A régua foi reduzida de 82 para 50 px: 14 px para datas, 20 px para horários e 16 px para a faixa de gravações. Foram removidos o título Gravação, o fuso visível, a entrada de hora e sua lógica de envio, os botões Ir, Visão de 24h, Acompanhar reprodução e Voltar ao vivo, o indicador de escala e a identificação sobreposta na imagem. A data filtra diretamente a disponibilidade; a escolha do horário fica na régua. Os registros históricos abaixo descrevem as decisões e validações anteriores a essa simplificação.

Validação desta alteração: TypeScript, sete testes de navegação/linha do tempo e a suíte de reprodução HLS no navegador passaram, incluindo retorno ao vivo ao fechar e layouts em 1440, 390 e 320 px nos dois temas.

## Velocidade por setas e retrocesso

O seletor foi substituído por duas setas junto aos controles centrais. Os passos são −8×, −4×, −2×, −1×, −0,5×, 0,5×, 1×, 2×, 4× e 8×; o indicador textual mostra o valor atual. Zero é tratado pelo botão de pausa, sem passo de velocidade zero. Alterar a velocidade conserva a pausa.

O avanço usa a velocidade nativa. O retrocesso usa buscas sucessivas em quadros decodificados, pois a taxa negativa não tem suporte confiável no navegador. Ao ultrapassar o começo da sessão, o player consulta disponibilidade autorizada e carrega uma janela anterior de até oito segundos, sem saltar lacunas. A liberação de sessões e a revalidação de permissões continuam ativas. A fluidez e a taxa efetiva do retrocesso dependem da decodificação e do carregamento dos trechos.

TypeScript, sete testes de navegação e a suíte HLS passaram. A suíte cobre retrocesso entre sessões, pausa, retorno ao avanço, limites de velocidade, parada na lacuna anterior e liberação ao fechar. Na câmera HOWBE PTZ, o relógio foi observado retrocedendo de 16:29:12 para 16:28:30, com imagens reais, sem mudar a configuração da câmera.

## Requisitos expressos no pedido

- Tornar funcional o painel aberto pelo menu de contexto da câmera no Ao vivo.
- Navegar livremente pelo tempo e ampliar ou reduzir a escala da linha do tempo.
- Atravessar a meia-noite sem limitar a navegação a um único dia; permitir, por exemplo, partes de dois dias na mesma janela visível.
- Exibir marcas menores de minutos e marcas maiores de horas, adaptadas à escala.
- Mostrar a hora, os minutos e os segundos do instante sob o mouse.
- Usar calendário em popover inspirado no Date Picker do shadcn, com indicação azul nos dias que possuem gravações e aparência normal nos demais.
- Permitir redimensionar o divisor entre a identificação da câmera e a escala, para visualizar nomes maiores.
- Usar as funcionalidades dos exemplos de ISS como referência e manter o estilo visual do Astra.

## Decisões confirmadas — rodada 1

1. A reprodução acontece no próprio quadro da câmera selecionada no mosaico. As demais câmeras permanecem ao vivo. A barra controla uma câmera por vez; o quadro indica o modo Gravação e oferece retorno ao vivo.
2. Navegação e reprodução são independentes. Arrastar a régua desloca somente a janela temporal visível. Clicar em um instante ou arrastar o marcador de reprodução solicita as imagens daquele instante. A roda do mouse e os botões +/− ajustam o zoom.
3. Ao alcançar uma lacuna, a reprodução para, informa a ausência de gravação e oferece uma ação explícita para o próximo trecho. Não salta automaticamente o intervalo ausente.
4. Os controles incluem reproduzir/pausar, saltos de −10s/+10s, gravação anterior/próxima, velocidades 0,5×/1×/2×/4×/8× e voltar ao vivo. Reprodução reversa e quadro a quadro não integram o conjunto confirmado.

## Decisões confirmadas — rodada 2

5. O painel abre pausado, aguardando a escolha de um horário. A proposta de iniciar automaticamente nos últimos cinco minutos foi rejeitada.
6. Escolher um dia no calendário apenas posiciona a régua e preserva o vídeo já em reprodução. Dias sem gravação continuam selecionáveis; o azul identifica imagens reais da câmera disponíveis para a conta do operador.
7. A visão máxima será de dois dias. O usuário corrigiu explicitamente a proposta de sete dias por considerar a visualização complexa. A escala inicial de 24 horas, aproximação até um minuto, zoom da roda ancorado no ponteiro e acompanhamento do marcador suspenso após navegação manual foram confirmados na consolidação final.
8. A barra usa o fuso do site da câmera, identificado ao operador, em calendário, régua, relógio e entrada de horário.
9. O painel também funciona em tela cheia/Video Wall e pode ser recolhido para liberar espaço de vídeo.
10. Fechar encerra a reprodução e devolve o quadro ao vivo. Abrir gravações de outra câmera encerra a anterior. A largura escolhida para a coluna do nome é lembrada nas próximas aberturas neste navegador.

## Decisões confirmadas — rodada 3

11. O quadro abre com Escolha um horário na linha do tempo, sem vídeo iniciado. A primeira escolha de horário gravado inicia a reprodução; as escolhas posteriores preservam o estado de pausa/reprodução.
12. Todo o histórico é apresentado no fuso do site atual da câmera, inclusive gravações de quando ela estava em outro site. A conversão de apresentação não altera os instantes preservados no acervo.
13. A reprodução fica vinculada somente ao quadro que abriu o menu. Outras ocorrências da mesma câmera continuam ao vivo. Remover o quadro, deixá-lo fora do layout ou sair do Ao vivo encerra a reprodução.
14. Partes consecutivas são percorridas automaticamente, inclusive ao atravessar a meia-noite. A reprodução para em lacunas reais ou ao chegar ao vídeo mais recente disponível. Anterior/próxima gravação navega por trechos contínuos, e não por arquivos internos. O fim é informado, com retorno ao vivo explícito.
15. Haverá entrada HH:mm:ss com botão Ir, além dos atalhos Visão de 24h e Acompanhar reprodução.

## Autorização de implementação

O usuário confirmou explicitamente: “Confirmo, pode implementar a barra de gravações conforme o fluxo consolidado.” A autorização inclui visão inicial de 24 horas do dia atual, zoom mínimo de um minuto, acompanhamento, referência central do calendário, busca ao concluir o arraste e recolhimento compacto distinto de fechamento.

## Árvore de decisões

- Reprodução no mosaico, uma câmera por vez — confirmada.
  - Abertura pausada e primeira escolha iniciando reprodução — confirmadas, Q5 e Q11.
  - Fechamento, troca de câmera e largura memorizada — confirmados, Q10.
  - Ocorrências duplicadas da câmera e retirada do quadro do mosaico — confirmadas, Q13.
- Navegação independente da reprodução — confirmada.
  - Calendário somente como navegação — confirmado, Q6.
  - Máximo de dois dias — confirmado, Q7; demais detalhes confirmados no fechamento do fluxo.
  - Entrada direta HH:mm:ss e atalhos — confirmados, Q15.
- Interrupção em lacunas e conjunto de controles — confirmados.
  - Continuidade entre partes consecutivas e fim do acervo — confirmados, Q14; salto para lacuna segue a ausência explícita, sem avanço automático.
- Interpretação dos horários.
  - Fuso do site da câmera — confirmado, Q8.
  - Site atual como referência uniforme do histórico — confirmado, Q12.
- Barra acessível durante a operação.
  - Maximização e Video Wall — confirmados, Q9.

## Pesquisa de referências

Consulta de documentação pública em 17/11/2026. Os comportamentos abaixo são documentados pelos fornecedores; não houve acesso operacional às instalações desses produtos. Versões, clientes desktop e aplicativos móveis podem diferir.

| Referência | Comportamento documentado | Aplicação ao desenho do Astra |
| --- | --- | --- |
| [HikCentral — Play Video File](https://enpinfo.hikvision.com/hkwsen/unzip/20230830144955_12918_doc/GUID-38D51BFC-4320-4075-A458-564CFBBCC70D.html) | Navegação arrastando a linha do tempo, ampliação/redução com botões ou roda e cores por tipo de vídeo. Miniaturas dependem do equipamento. | Confirma a linha do tempo como controle de localização e escala. Não implica adicionar miniaturas ao escopo. |
| [DahuaWiki — Playback/Basic, seção DSS Express](https://dahuawiki.com/Playback/Basic) | Busca por câmera, origem, data e hora; resultado no eixo temporal; duplo clique em horário gravado para reproduzir e roda para ampliar o eixo. | Referência de funcionamento do DSS Express; não atribuir automaticamente todos os detalhes ao DSS Professional. |
| [DSS Professional V7 — ficha de recursos](https://dahuawiki.com/images/Files/Manuals/DSS_Windows/DSS_Professional_V7.00.000.pdf) | Lista ampliação e redução da linha do tempo. | Confirma o recurso no Professional. Os manuais completos tentados em dahuasecurity.com e DahuaWiki falharam por timeout; a pesquisa detalhada usa a documentação acessível do Express. |
| [vCloud.ai — manual VSaaS 1.1, Playing archive](https://docs.vcloud.ai/index.php/VCloud.ai_VSaaS_user_manual_1.1) | Alternância entre ao vivo e acervo, arraste e zoom da linha do tempo, entrada manual de data/hora e escolha de velocidade. | Referência para integração da reprodução ao quadro e acesso direto a um instante. O produto identificado nesta pesquisa foi vCloud.ai. |
| [Intelbras Defense IA 4.0 — guia de operação](https://manuais.intelbras.com.br/manual-defense-ia-4.0/pt-BR/guiadeopera%C3%A7%C3%A3o.html) | Controles desktop para reprodução, velocidade e deslocamento temporal. Na seção do aplicativo, dias com vídeos recebem ponto azul e a linha do tempo admite arraste e zoom por gesto de pinça. | Apoia a distinção entre disponibilidade e seleção de data. O gesto de pinça é documentado no aplicativo e não comprova o gesto no cliente desktop. |
| [Intelbras SIM Next — manual](https://manuais.intelbras.com.br/manual-sim-next/simnext.html) | Escala ampliável, controles de reprodução e sincronização opcional entre canais. | Reforça a separação entre consultar disponibilidade, controlar reprodução e sincronizar câmeras; o Astra terá uma câmera por vez nesta etapa. |
| [shadcn — Base UI Date Picker](https://ui.shadcn.com/docs/components/base/date-picker) | Composição de Popover e Calendar, com seleção de data. | Base do calendário solicitado. A disponibilidade de gravações e a indicação azul são regras do Astra, a serem alimentadas pelo acervo. |

Os recortes de ISS fornecidos pelo usuário documentam visualmente régua graduada, trechos coloridos, hora pontual e coluna de nomes ajustável. Servem de referência funcional, sem reprodução do estilo.

## Estado encontrado antes da implementação

- `frontend/src/components/live.tsx` mantém uma câmera selecionada e abre `LiveRecordingPanel`; o mosaico permanece ao vivo.
- `frontend/src/components/live-recording-panel.tsx` apenas consulta disponibilidade e desenha faixas. Não há reprodução, clique para buscar imagens, arraste, marcador ou zoom.
- O painel atual usa data civil e fuso do navegador; a agenda de captura usa o fuso do site. O novo fluxo precisa fechar uma regra explícita de apresentação.
- `frontend/src/app/workspace.css` fixa a largura da coluna de nomes e oculta o painel em modo Video Wall.
- `frontend/src/lib/recording-timeline.ts` preserva lacunas e trata a duração variável de dias civis. O painel divide consultas em janelas de seis horas.
- O player de gravações no **Ao vivo** usa `frontend/src/components/recording-video.tsx` para reproduzir o acervo via HLS e mapear o tempo de mídia para o horário da gravação.
- `backend/internal/app/playback.go` limita cada consulta de segmentos a 24 horas e cada sessão de reprodução a uma hora. A consulta interna limita o retorno a 10.000 segmentos. Esses limites de transporte não devem ser confundidos com a escala visual pedida pelo usuário.
- Não foi encontrada consulta agregada de dias com gravação. O calendário precisará de disponibilidade mensal eficiente, restrita às gravações autorizadas para o operador, sem transformar falha de consulta em ausência de vídeos.
- O acervo é centralizado no Astra conforme `docs/adr/0001-gravacao-centralizada.md`. Consultar gravações diretamente de NVR/DVR continua sendo outro módulo.

### Consequências adicionais verificadas

- A mesma câmera pode aparecer em vários quadros, cada qual com `tileId` próprio. A ação deve conservar a identidade da ocorrência selecionada; o painel atual perde essa informação e guarda somente a câmera.
- Maximização de um quadro e Video Wall são modos distintos. Reduzir o layout pode retirar de exibição o quadro selecionado; sair do Ao vivo desmonta o painel. Trocar de tenant descarta o contexto de visualização. O ciclo da reprodução precisa contemplar essas saídas.
- Os segmentos preservam o site de origem para autorização, mas não uma cópia do fuso configurado no momento da captura. O cadastro da câmera pode mudar de site e o próprio fuso de um site pode ser alterado. Por isso, fuso do site atual e fuso histórico não são conceitos equivalentes.
- Operadores com concessão somente à câmera podem não consultar o cadastro de seu site. A futura entrega do fuso deve atender esses operadores sem exigir uma permissão administrativa adicional.
- O frontend já possui Popover shadcn com Radix; ainda não possui Calendar, DatePicker ou dependência de calendário. O comportamento do exemplo Base UI pode ser adaptado ao conjunto de componentes existente.

## Vocabulário

Usar as definições de Linha do tempo de gravações, Janela temporal visível, Posição de reprodução, Trecho contínuo de gravação, Gravação e Lacuna de gravação em `../GLOSSARY.md`.

Não confundir a janela temporal visível com o período que está tocando, nem a disponibilidade de gravações com a agenda futura de captura.

## Critérios de validação já estabelecidos

- No mosaico com várias câmeras, abrir a barra em uma delas não altera a visualização das demais.
- Abrir o painel não inicia uma sessão de vídeo antes da escolha do horário.
- A primeira escolha inicia a reprodução; buscas posteriores preservam pausa/reprodução. O relógio indica o instante original do acervo, inclusive após mudanças de velocidade e busca.
- Arrastar a régua pode deixar partes de dois dias na mesma janela. O vídeo em reprodução conserva sua posição durante essa navegação.
- Reduzir o zoom respeita o máximo de dois dias; ampliar conserva a referência do horário durante o gesto.
- Passar o mouse por diferentes pontos informa o instante correspondente, com segundos e identificação da data quando houver mais de um dia visível.
- Escolher outra data apenas navega. O calendário distingue dia com imagens, dia vazio, data selecionada, carregamento e erro de consulta.
- A indicação de gravação considera o acervo autorizado ao operador. Mudança de permissões não pode manter imagens ou disponibilidade antes acessíveis.
- Ajustar o divisor permite ler nomes maiores e preserva espaço útil para a linha do tempo; o ajuste também deve ser operável pelo teclado.
- Lacuna real fica visível e interrompe a reprodução. Há ação explícita para o próximo intervalo disponível.
- A reprodução atravessa partes consecutivas e a meia-noite sem parada artificial. Os controles anterior/próximo localizam trechos contínuos, e saltos para horários vazios não escondem a ausência de imagens.
- A mesma câmera repetida em outro quadro continua ao vivo. Retirar do layout o quadro em reprodução ou sair do Ao vivo encerra a sessão.
- O fuso do site atual mantém calendário, régua, tooltip, entrada manual e relógio coerentes, inclusive em histórico de outro site e em dias civis de duração variável.
- Fechar o painel ou trocar de câmera encerra a sessão anterior e restaura o modo ao vivo.
- A barra permanece utilizável no modo de tela cheia, nos temas claro e escuro e em diferentes larguras de janela.
- Consultas e sessões descartadas por uma navegação mais recente não podem sobrescrever a seleção atual. Erro, carregamento e ausência de gravação são estados distintos.

A validação de interface deverá ser complementada por testes do mapeamento de tempo e dos contratos de autorização/disponibilidade; um desenho correto da barra, por si só, não comprova reprodução de vídeo real.

Nenhum ADR novo foi criado: os requisitos visuais e as decisões de interação confirmadas não introduziram uma escolha arquitetural que satisfaça os critérios de um ADR.

## Implementação

- `frontend/src/components/live.tsx`: vínculo ao `tileId` que abriu o menu; reprodução em somente uma ocorrência da câmera; encerramento ao retirar o quadro do layout ou sair da operação. O player ao vivo dos demais quadros mantém sua identidade.
- `frontend/src/components/live-recording-panel.tsx`: estado da reprodução e da janela visível, controles, continuidade entre sessões, tratamento de lacunas, encerramento e invalidação de acesso.
- `frontend/src/components/recording-video.tsx`: sessão HLS controlada com compatibilidade de codec, renovação, limpeza de respostas atrasadas e mapeamento do tempo da mídia para o instante original. Pausa e velocidade não criam sessões novas.
- `frontend/src/components/recording-ruler.tsx`: régua com marcas adaptativas, datas, hover com segundos, pan, zoom ancorado, busca ao concluir o gesto e divisor ajustável por ponteiro/teclado com largura memorizada.
- `frontend/src/components/recording-date-picker.tsx`: composição Popover/Calendar no conjunto shadcn/Radix já usado no projeto, com navegação por teclado, disponibilidade azul e estados de consulta distintos. Também consulta os meses adjacentes presentes nas seis semanas exibidas.
- `frontend/src/lib/recording-navigation.ts`: calendário e conversão explícita para o fuso do site, incluindo rejeição de horários civis inexistentes. Quando um horário digitado se repete na mudança de horário de verão, usa a primeira ocorrência; a régua continua navegável em todos os instantes.
- `frontend/src/components/recording-timeline.css` e `frontend/src/app/workspace.css`: apresentação integrada ao Astra, temas, slider de zoom, toast, tela cheia e prevenção de sobreposição pela lista lateral em telas pequenas.
- `backend/internal/app/recording_availability.go`: consulta protegida de metadados, dias do mês, intervalos de até 48 horas e trecho anterior/próximo. Agrega partes realmente reproduzíveis no banco, aplicando a autorização pelo site de origem antes de unir os intervalos.

A rota é `GET /api/v1/tenants/{tenant}/cameras/{camera}/recording-availability`. Sem filtros, retorna o fuso do site atual. `month=AAAA-MM` retorna dias com gravações; `start/end` retorna trechos contínuos; `direction=previous|next&at=...` localiza o bloco vizinho. Ela preserva o isolamento por tenant e atende concessões somente à câmera sem exigir consulta administrativa de site. O limite de 48 horas é visual/de disponibilidade; as sessões de vídeo continuam limitadas a uma hora e são encadeadas pela reprodução.

## Validação concluída

- Suíte completa do backend com PostgreSQL real em schemas temporários: aprovada nos pacotes de aplicação, câmeras, mídia, PTZ, serviços e armazenamento. Cenários que exigem fontes opcionais permanecem condicionados às suas variáveis.
- `TestRecordingAvailabilityUsesPlayablePartsAndHistoricalAuthorization`: aprovado; cobre agregação de partes, lacunas internas e por permissão, datas mensais no fuso atual, horário de verão, extremos exclusivos, descarte, navegação e revogação.
- `TestScheduledCaptureToAuthorizedTemporalPlayback`, habilitado com servidor RTSP e vídeo sintético exclusivos do laboratório: aprovado. Captura, nova disponibilidade, entrega HTTP de bytes decodificáveis, exportação e revogação foram exercitadas. A fixture passou a usar um operador comum para testar acesso restrito, pois o administrador de instalação possui concessões integrais.
- `npm run test:live-recording`: 13 cenários aprovados com componentes reais, APIs simuladas e vídeo HLS/fMP4 realmente decodificado pelo Chrome. Inclui continuidade através da meia-noite, pausa preservada, busca somente ao soltar o marcador, descarte de respostas atrasadas, revogação, tela cheia e layouts de 1440, 390 e 320 px nos dois temas.
- Testes de calendário, conversão de fuso, zoom, mapeamento da mídia e regressão do mosaico: aprovados. TypeScript e build Next.js com Webpack: aprovados.
- Capturas inspecionadas em `output/playwright/live-recording`. O teste completo pode ser repetido conforme `frontend/tests/live-recording/README.md`.
- Backend de desenvolvimento atualizado, com executável anterior preservado para retorno. Saúde da API aprovada e frontend respondendo HTTP 200 em `https://localhost:18443`.

Essa primeira rodada utilizou dados sintéticos e schemas exclusivos, sem alterar cadastros reais. A validação com câmera cadastrada foi realizada posteriormente, conforme a seção seguinte.

## Validação com gravações reais — 17/11/2026

Validação executada na sessão autenticada do navegador, usando o painel real do Ao vivo e o acervo da câmera **HOWBE PTZ**, no Ambiente padrão e no fuso America/Sao_Paulo. Não houve simulação de API, substituição de mídia nem alteração da configuração da câmera ou da gravação.

- Busca por horário: sessão real criada para 15:20:40 e vídeo 2688 × 1520 carregado. Após o ajuste de precisão descrito abaixo, tanto a entrada quanto o relógio da reprodução indicaram 15:20:40 com o vídeo pausado e pronto para reprodução.
- Continuidade: a reprodução avançou de 15:20:40 a 15:21:32, atravessando os limites reais entre arquivos em 15:20:44.542246 e 15:21:14.663246. O tempo da mídia avançou aproximadamente 52,33 segundos durante o mesmo intervalo de tempo real, em velocidade 1×, sem erro de mídia.
- Zoom: os limites de 48 horas e um minuto foram atingidos, com os respectivos botões desabilitados nos extremos. A URL local do vídeo, a posição de mídia e a pausa permaneceram iguais durante o zoom e o arraste da régua. Durante nova reprodução, ampliar e reduzir a escala também preservou o mesmo vídeo e sua progressão.
- Lacuna real: a reprodução iniciada em 15:27:43 parou em 15:27:45.914246, exibindo Sem gravação neste período e a ação explícita para o próximo trecho. A imagem foi descarregada ao parar, em vez de permanecer como uma falsa imagem atual.
- Próximos trechos: após corrigir a precisão da busca, foi possível reproduzir o trecho curto de 15:27:49.833074 a 15:27:49.953074 e avançar explicitamente ao bloco seguinte, iniciado em 15:27:54.308448. A sessão seguinte reproduziu até 15:29:54 sem erro; a imagem real permaneceu carregada e a pausa final funcionou.
- Calendário: 17/11/2026 apareceu como dia com gravações, com indicação azul, enquanto 16 e 18/11 apareceram sem gravações, de acordo com o acervo dessa câmera.

O navegador foi deixado com o quadro dessa câmera pausado em 15:29:54 e escala de um minuto, para inspeção. A validação é específica desta câmera, do acervo disponível e deste cliente; não constitui homologação de todos os equipamentos ou codecs.

### Correções encontradas no teste real

O acervo e as respostas do backend conservam frações menores que um milissegundo. `Date.parse` descartava essas frações. No relógio, combinar esse valor truncado com o offset da mídia podia mostrar o segundo anterior em uma busca exata. No botão de próximo trecho, o instante enviado podia ficar alguns microssegundos antes do começo da gravação: o backend retornava `recording_gap` e a navegação reencontrava o mesmo bloco.

`recordingTimestamp` agora conserva a fração até a conversão final; `recordingSeekInstant` escolhe o primeiro milissegundo representável dentro do intervalo. O relógio arredonda sua apresentação ao milissegundo. A autorização e a regra do backend que interrompe em lacunas reais foram preservadas. A reprodução foi repetida no mesmo cenário que falhava e o avanço para o bloco seguinte passou.

Dois testes de regressão cobrem a busca exata com timestamps de alta precisão e a transformação do início de um bloco em um instante válido para a sessão. Os sete testes de linha do tempo/conversão de horários, o TypeScript e o build de produção com Webpack passaram após a correção.

### Evidências e observação de horário

As capturas e o relatório estruturado ficam em `output/playwright/live-recording-real-20261117/`. `11-validacao-final.jpg` mostra o estado final e `validation-result.json` registra os resultados. Os arquivos são locais e não contêm credenciais ou cabeçalhos de autenticação.

O relógio sobreposto pela própria câmera na imagem está aproximadamente **3h54min41s atrás do horário registrado no acervo**. Essa divergência foi observada visualmente e a configuração do equipamento foi preservada. A busca validada usa o horário do acervo apresentado na barra, convertido para o fuso do site.
