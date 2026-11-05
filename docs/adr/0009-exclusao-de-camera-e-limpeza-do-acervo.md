# Exclusão definitiva de câmera com limpeza do acervo em segundo plano

Decisões confirmadas em 31/10/2026 e revisadas em 01/11/2026, com desenho fechado e implementação autorizada em 01/11/2026. Após a confirmação, a câmera fica indisponível, suas transmissões e novas gravações são encerradas e a remoção das gravações existentes ocorre em segundo plano, sem opção de desfazer. A separação permite retirar a câmera de operação sem aguardar a limpeza de todo o acervo.

Exportações já geradas seguem sua própria política de expiração. Arquivos já baixados pelo operador estão fora do alcance do VMS.

A confirmação é simples: informa que a câmera identificada e pertencente ao site informado será excluída junto com suas gravações. Não exige digitação do nome nem apresenta estimativas de quantidade, tamanho ou período.

A câmera sai da lista operacional. Não haverá aba de exclusões, painel de progresso nem notificações de conclusão ou falha. Essa revisão substitui a aprovação inicial de acompanhamento persistente na interface; o estado interno continua persistente para permitir a limpeza após fechar a janela ou reiniciar o servidor. O sistema não deve afirmar que os arquivos já foram removidos enquanto houver pendências.

Reproduções de gravações são encerradas e exportações ainda em processamento são canceladas. Novos usos são bloqueados, e a remoção aguarda a liberação dos arquivos pelos consumidores e pela captura. Essa decisão evita que o uso continuado adie indefinidamente a limpeza, respeitando a proteção temporária enquanto os arquivos ainda estão abertos.

Se o armazenamento estiver indisponível ou houver arquivos que não possam ser removidos, a câmera continua excluída e a limpeza permanece pendente. O processamento retoma automaticamente, inclusive após reiniciar o servidor. Falhas posteriores ficam registradas na auditoria, sem avisos na tela. Não será criada uma interface de acompanhamento ou de retentativa manual.

A identificação mínima da câmera e a auditoria da exclusão são preservadas, incluindo solicitante, data e resultado. Isso mantém a explicação da operação e as referências das exportações restantes; os dados de conexão e as credenciais são removidos.

A exclusão exige uma permissão específica, separada da edição de câmeras, sem concessão automática a todos que já podem editar o cadastro. O administrador global mantém acesso à ação.

A exclusão abrange somente a câmera e seu histórico dentro do mesmo tenant. Mudanças de site não dividem esse histórico; não será exigida autorização adicional em cada site anterior. Esta funcionalidade não move câmeras entre tenants nem alcança dados de outros tenants, mantendo a fronteira estabelecida na ADR 0003. A implementação de novos mecanismos de isolamento entre tenants fica fora deste escopo.

## Retorno da ação

Ao aceitar a exclusão, informar: “Câmera excluída. As gravações serão removidas em segundo plano.” Se não for possível iniciar, mostrar o erro e manter a câmera na lista. Falhas posteriores da limpeza geram novas tentativas automáticas e registros na auditoria, sem notificações.

## Execução

A retirada da câmera, a revogação dos usos e a solicitação persistente de limpeza são gravadas na mesma transação. O cadastro preserva uma marca de exclusão e a identificação histórica, sem permitir reativação. A limpeza física percorre o catálogo do armazenamento em páginas após encerrar a captura, incluindo o último trecho finalizado e arquivos ainda não importados pelo servidor. A conclusão e as falhas ficam na auditoria existente.
