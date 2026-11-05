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

**Exclusão de câmera**:
Retirada definitiva de uma câmera e de suas gravações do VMS, com encerramento de suas transmissões e captura. Exportações já geradas conservam sua própria expiração.
_Evitar_: desativação de câmera, substituição da fonte

**Fonte de vídeo**:
Origem das imagens de uma câmera, cuja substituição distingue o vídeo anterior do produzido pela nova origem. A manutenção dos dados de conexão da mesma origem não constitui sua substituição.
_Evitar_: câmera cadastrada, perfil de vídeo

**Integração de câmera**:
Forma de comunicação com a fonte de vídeo, por protocolo genérico ou por recursos específicos de um fabricante, para obter suas informações, acessar seus perfis de vídeo e operar os recursos que disponibiliza.
_Evitar_: câmera cadastrada, perfil de vídeo

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

**Grupo de permissões**:
Conjunto de permissões pertencente a um tenant e atribuível a operadores, que determina quais áreas, recursos e ações do VMS eles podem acessar nesse tenant.
_Evitar_: tenant, grupo de câmeras

**Concessão de acesso**:
Autorização que reúne uma ação e seu escopo de recursos em um tenant; a combinação de grupos preserva esse vínculo.
_Evitar_: acesso irrestrito, permissão global de vídeo

**Perfil de vídeo**:
Versão do vídeo disponibilizada por uma câmera, identificada como stream principal ou substream, com suas características de imagem.
_Evitar_: mosaico, gravação

**Transmissão de vídeo**:
Fluxo de imagens entregue durante uma visualização, distinto do perfil que fornece suas características e do registro preservado no acervo.
_Evitar_: perfil de vídeo, gravação

**Agenda de gravação**:
Conjunto de períodos em que uma câmera deve ser gravada continuamente.
_Evitar_: retenção, gravação por movimento

**Retenção**:
Prazo configurado para conservar gravações antes de sua remoção automática. O histórico efetivamente disponível também depende da capacidade de armazenamento e da política de sobrescrita.
_Evitar_: agenda de gravação

**Cota de armazenamento**:
Limite máximo configurável de armazenamento de gravações de um tenant, incluindo as que estão em andamento, que não pode ser excedido mesmo quando houver espaço livre no disco compartilhado. Exportações usam orçamento separado.
_Evitar_: retenção, reserva de capacidade

**Exportação**:
Arquivo de vídeo de um intervalo de uma câmera para obtenção por um operador autorizado, que preserva a duração original com tela preta nos períodos sem gravação.
_Evitar_: gravação, reprodução

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

**Acervo**:
Conjunto de gravações preservadas de um tenant, com a origem de cada registro mantida ao longo das mudanças de câmera e site.
_Evitar_: exportações, espaço livre

**Lacuna de gravação**:
Intervalo sem imagens preservadas disponíveis no acervo, cuja causa deve ser distinguida, como ausência de captura ou descarte. Preenchimento preto em uma exportação representa essa ausência e não constitui gravação da câmera.
_Evitar_: imagem preta capturada, vídeo recuperado
