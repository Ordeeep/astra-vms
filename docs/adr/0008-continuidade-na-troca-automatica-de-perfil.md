# Continuidade visual na troca automática de perfil ao vivo

Na visualização ao vivo, a seleção automática de qualidade será mantida, mas a troca de perfil preservará a reprodução da transmissão atual enquanto o novo perfil é preparado. A imagem só será substituída quando a nova transmissão estiver pronta para exibição, evitando uma tela de carregamento durante a transição.

Se a preparação do novo perfil falhar, a transmissão atual continuará em exibição enquanto estiver funcionando e autorizada. Um aviso discreto informará que a qualidade solicitada não ficou disponível, sem interromper a imagem atual.

Durante a maximização, as transmissões das outras câmeras do mosaico permanecerão conectadas nos perfis que já utilizavam. Quando a regra de qualidade exigir outro perfil para a câmera maximizada, uma transmissão adicional será preparada sem alterar as demais. Assim que o novo vídeo estiver pronto, ele substituirá a transmissão anterior dessa câmera, encerrando sua conexão e sessão. Ao restaurar, o perfil do mosaico será preparado novamente, mantendo a imagem atual até a substituição estar pronta. Quando o perfil já for o mesmo, a transmissão existente será reutilizada sem uma conexão adicional.

Revisão de 16/11/2026: a manutenção simultânea dos dois perfis da câmera em destaque foi removida por consumir rede e processamento durante toda a maximização. A sobreposição passa a durar apenas a preparação da troca, incluindo o retorno ao mosaico; o custo é aguardar essa preparação para recuperar a qualidade anterior. Permanecem válidas a prioridade da escolha explícita do operador e as regras de adaptação da ADR 0007. A regra de uma posição orienta apenas a visualização em destaque, preservando as transmissões das outras câmeras do mosaico.
