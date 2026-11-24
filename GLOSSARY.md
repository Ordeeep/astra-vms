# Gestão de vídeo

Vocabulário do VMS para visualização de câmeras e consulta de gravações em múltiplos locais e empresas.

## Linguagem

**VMS (Video Management System)**:
Sistema de gestão de vídeo que reúne a visualização de câmeras e a consulta de suas gravações.

**Site**:
Local físico identificado por um endereço, pertencente a um tenant, ao qual câmeras são alocadas.
_Evitar_: site web, página

**Tenant**:
Empresa que delimita um conjunto isolado de câmeras, sites e gravações. Um operador pode ter acesso autorizado a mais de um tenant.
_Evitar_: ambiente, site, usuário, operador

**Operador**:
Pessoa com acesso autorizado ao VMS e a um ou mais tenants.
_Evitar_: tenant, empresa

**Câmera**:
Cadastro do ponto monitorado, pertencente a um tenant e alocado a um site, cuja identidade e histórico permanecem quando o equipamento é substituído.
_Evitar_: endereço RTSP, equipamento físico

**ID da câmera**:
Número permanente que identifica o cadastro de uma câmera dentro de seu tenant. Câmeras de tenants diferentes podem ter o mesmo número.

**Habilitação do cadastro da câmera**:
Estado administrativo que permite ou suspende a visualização ao vivo e novas gravações de uma câmera. A operação também depende da habilitação do site e do tenant.
_Evitar_: disponibilidade de câmera, estado da gravação

**Exclusão de câmera**:
Retirada definitiva de uma câmera e de suas gravações do VMS, com encerramento de suas transmissões e captura. Exportações já geradas conservam sua própria expiração.
_Evitar_: desativação de câmera, substituição da fonte

**Edição em lote de câmeras**:
Aplicação dos mesmos ajustes escolhidos às câmeras selecionadas de um tenant. Cada câmera conserva os valores próprios dos demais ajustes.

**Fonte de vídeo**:
Origem das imagens de uma câmera, cuja substituição distingue o vídeo anterior do produzido pela nova origem. A manutenção dos dados de conexão da mesma origem não constitui sua substituição.
_Evitar_: câmera cadastrada, perfil de vídeo

**Integração de câmera**:
Forma de comunicação com a fonte de vídeo, por protocolo genérico ou por recursos específicos de um fabricante, para obter suas informações, acessar seus perfis de vídeo e operar os recursos que disponibiliza.
_Evitar_: câmera cadastrada, perfil de vídeo

**Evento analítico**:
Registro individual de um alerta de analítico de vídeo associado a uma câmera.
_Evitar_: ocorrência, notificação do sistema, evento de auditoria

**Recepção de eventos**:
Capacidade do Astra de receber os alertas emitidos pela fonte de uma câmera. Uma câmera pode fornecer vídeo durante uma interrupção da recepção de eventos.

**Período de cobertura incerta**:
Intervalo em que o Astra não consegue confirmar a recepção de todos os alertas de uma câmera. Alguns alertas desse período podem constar no histórico.

**Regra analítica esperada**:
Regra de detecção que o operador identifica como proteção esperada para uma câmera. Os horários confirmados indicam quando essa proteção é prevista.

**Supervisão de regras por canal**:
Verificação da habilitação de um tipo de analítico para uma entrada de vídeo do equipamento. Quando a câmera só fornece esse estado, a habilitação de cada linha ou região permanece indeterminada.

**Recepção de eventos em validação**:
Condição de operação de uma combinação de modelo e firmware enquanto se verificam os tipos de alerta recebidos, os recursos de supervisão das regras e a recuperação após uma desconexão.

**Horário do evento**:
Horário em que o Astra recebeu o alerta de analítico. O horário informado pela câmera é um dado do alerta.

**Ocorrência**:
Caso operacional identificado a partir de um ou mais eventos analíticos.

**Intrusão**:
Tipo de evento analítico ligado ao monitoramento de presença em uma área da imagem.

**Cruzamento de linha**:
Tipo de evento analítico que indica a passagem de um lado para o outro de uma linha virtual na imagem.

**Controle PTZ**:
Operação dos movimentos horizontal e vertical e do zoom óptico oferecidos pela fonte de uma câmera. Cada fonte pode disponibilizar apenas parte desses recursos.
_Evitar_: zoom digital do player, perfil de vídeo

**Posição inicial PTZ**:
Posição de referência já configurada na fonte de uma câmera, à qual o operador pode solicitar retorno quando esse recurso está disponível.
_Evitar_: preset 1, última posição salva, coordenada zero

**Disponibilidade de câmera**:
Condição que indica se o Astra consegue receber vídeo da fonte de uma câmera em pelo menos um de seus perfis, distinta da habilitação do cadastro e da situação da gravação.
_Evitar_: resposta ao ping, câmera habilitada, gravação em andamento

**Câmera online**:
Câmera cuja fonte fornece vídeo ao Astra em pelo menos um de seus perfis. Essa condição não garante o funcionamento de todos os perfis nem a gravação ou exibição no navegador.
_Evitar_: equipamento respondendo ao ping, todos os perfis disponíveis

**Câmera offline**:
Câmera habilitada cuja fonte foi verificada sem vídeo disponível em nenhum de seus perfis. Ausência de informação e suspensão do monitoramento são condições distintas.
_Evitar_: câmera desativada, gravação interrompida, monitoramento indisponível

**Contexto de visualização**:
Ambiente de operação de um único tenant em uma aba do navegador, separado dos ambientes abertos nas demais abas.
_Evitar_: conta do operador, instalação

**Mosaico**:
Composição de visualização simultânea de imagens de várias câmeras.
_Evitar_: site, tenant

**Debug do mosaico**:
Modo de diagnóstico da visualização ao vivo que identifica as câmeras e mostra FPS, resolução, codec e bitrate de vídeo recebido pelo navegador.

**Grupo de permissões**:
Conjunto de permissões pertencente a um tenant e atribuível a operadores, que determina quais áreas, recursos e ações do VMS eles podem acessar nesse tenant.
_Evitar_: tenant, grupo de câmeras

**Concessão de acesso**:
Autorização que reúne uma ação e seu escopo de recursos em um tenant; a combinação de grupos preserva esse vínculo.
_Evitar_: acesso irrestrito, permissão global de vídeo

**Perfil de vídeo**:
Versão do vídeo disponibilizada por uma câmera, identificada como stream principal ou substream, com suas características de imagem.
_Evitar_: mosaico, gravação

**Modo de exibição da câmera**:
Ajuste de como a imagem da câmera ocupa a célula na visualização ao vivo, preenchendo a célula, forçando a proporção 4:3 ou mantendo a proporção original.
_Evitar_: resolução do perfil de vídeo

**Transmissão de vídeo**:
Fluxo de imagens entregue durante uma visualização, distinto do perfil que fornece suas características e do registro preservado no acervo.
_Evitar_: perfil de vídeo, gravação

**Agenda de gravação**:
Definição da captura contínua 24 horas por dia ou das faixas semanais de gravação no horário local do site da câmera. Uma agenda semanal vazia deixa a câmera fora do horário de gravação em todos os dias.
_Evitar_: retenção, gravação por movimento

**Configuração de gravação**:
Definição individual de uma câmera que reúne a habilitação da gravação, o perfil de vídeo gravado, a agenda e a retenção própria ou herdada do tenant.
_Evitar_: perfil de vídeo, agenda de gravação, configuração de armazenamento

**Retenção**:
Prazo configurado para conservar gravações antes de sua remoção automática. O histórico efetivamente disponível também depende da capacidade de armazenamento e da política de sobrescrita.
_Evitar_: agenda de gravação

**Cota de armazenamento**:
Limite máximo configurável de armazenamento de gravações de um tenant, incluindo as que estão em andamento, que não pode ser excedido mesmo quando houver espaço livre no disco compartilhado. Exportações usam orçamento separado.
_Evitar_: retenção, reserva de capacidade

**Exportação**:
Arquivo de vídeo de um intervalo de uma câmera para obtenção por um operador autorizado, que preserva a duração original com tela preta nos períodos sem gravação.
_Evitar_: gravação, reprodução

**Tarefa de exportação**:
Solicitação de um operador para gerar uma exportação de um intervalo de gravação de uma câmera, com progresso e resultado próprios.

**Janela de tarefas**:
Visão pessoal das tarefas que um operador solicitou no tenant ativo, incluindo o progresso e o resultado de cada uma.

**Orçamento de exportações**:
Limite de armazenamento de exportações de um tenant, separado de sua cota de gravações e sujeito também ao limite físico global.
_Evitar_: cota de gravações, retenção de gravações

**Proteção temporária de gravação**:
Impedimento limitado de descarte dos trechos já em uso para reprodução ou exportação, que continuam contabilizados na cota de gravações.
_Evitar_: retenção permanente, espaço adicional

**Visualização ao vivo**:
Exibição das imagens atuais de uma câmera, sujeita ao atraso entre a captura e a apresentação ao usuário.
_Evitar_: gravação, reprodução de gravação

**Gravação**:
Registro de vídeo preservado para consulta posterior.
_Evitar_: visualização ao vivo

**Linha do tempo de gravações**:
Representação dos períodos com gravações disponíveis de uma câmera e das lacunas entre eles.
_Evitar_: agenda de gravação

**Disponibilidade de gravações**:
Períodos do acervo de uma câmera com imagens efetivamente disponíveis para reprodução pelo operador. Não inclui a previsão de captura nem imagens ainda não disponíveis para consulta.
_Evitar_: disponibilidade de câmera, agenda de gravação, gravação prevista

**Janela temporal visível**:
Intervalo do acervo apresentado na linha do tempo de gravações, que pode atravessar datas e ser diferente do período em reprodução.
_Evitar_: dia selecionado, duração do vídeo

**Posição de reprodução**:
Instante do acervo selecionado para consultar as imagens gravadas. Na consulta conjunta do mosaico, todas as câmeras compartilham essa posição, e cada linha do tempo indica a disponibilidade da respectiva câmera.
_Evitar_: horário ao vivo, posição do mouse

**Trecho contínuo de gravação**:
Período com imagens disponíveis de uma câmera sem lacunas de gravação, que pode atravessar a meia-noite e reunir várias partes do acervo.
_Evitar_: arquivo de vídeo, dia de gravação

**Acervo**:
Conjunto de gravações preservadas de um tenant, com a origem de cada registro mantida ao longo das mudanças de câmera e site.
_Evitar_: exportações, espaço livre

**Lacuna de gravação**:
Intervalo sem imagens preservadas disponíveis no acervo, cuja causa deve ser distinguida, como ausência de captura ou descarte. Preenchimento preto em uma exportação representa essa ausência e não constitui gravação da câmera.
_Evitar_: imagem preta capturada, vídeo recuperado
