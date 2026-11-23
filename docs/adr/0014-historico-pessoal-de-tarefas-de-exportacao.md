# Histórico pessoal de tarefas de exportação

A Janela de tarefas usa a fila persistente de exportações como histórico pessoal do operador no tenant ativo. A API exige autoria na consulta e no cancelamento, inclusive para administradores. O download também exige acesso vigente à gravação. O resultado permanece consultável quando o arquivo expira, para preservar o histórico sem ampliar o acesso ao vídeo nem prolongar seu prazo de armazenamento.

Cada solicitação aceita indica uma nova tarefa no botão da janela. A tabela registra o momento do pedido, o nome da câmera, o intervalo do vídeo, o progresso e o autor. Os nomes são preservados como estavam na aceitação. O operador inicia o download na linha concluída. O cancelamento selecionado atua sobre uma tarefa ativa e o cancelamento geral, após confirmação, alcança toda a fila pessoal do tenant.
