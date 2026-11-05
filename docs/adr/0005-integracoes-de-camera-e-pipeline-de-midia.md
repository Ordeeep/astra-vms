# Integrações de câmera separadas do pipeline de mídia

O cadastro oferece integrações genéricas e de fabricante no campo Tipo de conexão. Cada integração terá um adaptador que fornece identificação e perfis de vídeo ao pipeline compartilhado do Astra, em vez de espalhar condições por fabricante no cadastro, player e gravação. A decisão permite adicionar Hikvision, Axis e outros fabricantes sem duplicar o processamento de vídeo, ao custo de manter um contrato comum entre adaptadores e o restante do sistema.

A primeira integração de fabricante será Dahua, usando sua API para identificação e perfis e o fluxo RTSP existente para vídeo. PTZ, eventos, escrita de configuração do equipamento e leitura de gravações de cartão/NVR não pertencem à primeira entrega; a gravação centralizada definida na ADR 0001 permanece vigente.

Nas integrações de fabricante, o operador informa a porta de acesso da câmera e a integração obtém a configuração interna de vídeo e os perfis oferecidos pelo equipamento. Porta RTSP e substreams não são definidos manualmente nesse cadastro; essa edição pertence ao modo RTSP manual. O backend aplica essa distinção para não tratar dados arbitrários do formulário como capacidades descobertas do equipamento.
