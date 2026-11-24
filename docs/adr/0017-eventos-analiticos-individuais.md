# Registro individual de alertas analíticos

Cada recebimento de um alerta de analítico de uma câmera gera um evento próprio no histórico do tenant, inclusive quando a origem reenvia o mesmo alerta. Preservar os alertas individuais permite consultar o que o Astra recebeu e aplicar futuras regras de correlação sobre esse histórico. Uma ocorrência poderá representar um caso operacional identificado a partir desses eventos.

## Horário e preservação do histórico

O horário de recebimento pelo Astra determina a ordenação, os filtros de período e o início da retenção de 90 dias. A hora informada pela câmera fica disponível no detalhe quando fornecida, para consulta mesmo quando o relógio do equipamento estiver incorreto.

A permissão Visualizar eventos pode abranger o tenant, sites ou câmeras. Para concessões por site, vale o site do alerta, preservado no evento. Isso mantém o histórico no escopo original quando uma câmera muda de local. A identificação da câmera e do site permanece consultável durante a retenção do evento após a exclusão da câmera.

## Primeira versão

A primeira versão cobre intrusão e cruzamento de linha. O fluxo será validado com eventos simulados vinculados a câmeras cadastradas, consultados pela API do histórico. A integração com equipamentos, a formação de ocorrências e o acesso à gravação pelo evento ficam para etapas posteriores.

A aba Eventos mostra os registros mais recentes do tenant ativo. A janela inicial é de 24 horas, com datas apresentadas no fuso do navegador. Os filtros de período, tipo, site e câmera são combináveis, com seleção múltipla para tipo, site e câmera.
