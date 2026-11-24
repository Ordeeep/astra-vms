# Aplicação atômica de ações em lote de câmeras

Cada ação em lote vale para a seleção confirmada de um único tenant. O Astra valida as permissões e a elegibilidade de todas as câmeras e confirma a operação apenas quando pode registrar todas as mudanças juntas, pois uma aplicação parcial poderia deixar câmeras com agendas ou sites diferentes dos que o operador solicitou. Na exclusão, a transação retira as câmeras da operação e registra as solicitações de limpeza, que seguem em segundo plano conforme a [ADR 0009](0009-exclusao-de-camera-e-limpeza-do-acervo.md).
