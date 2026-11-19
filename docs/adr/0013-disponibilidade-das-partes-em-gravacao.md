# Disponibilidade das partes completas de uma gravação em andamento

A linha do tempo deve crescer com vídeo reproduzível em até cinco segundos em operação normal e continuar atualizando durante a navegação. Em 19/11/2026 foi aprovada a publicação de snapshots das partes completas e duráveis dos arquivos ainda em gravação, conservando arquivos de aproximadamente 30 segundos e reaproveitando o canal SSE do contexto de visualização. Finalizar arquivos a cada dois segundos simplificaria a leitura, mas multiplicaria aproximadamente por quinze a quantidade de arquivos e registros do acervo.

Cada snapshot associa o índice a um limite de bytes confirmados e a uma revisão crescente. Leitores já abertos não incorporam bytes posteriores; exportações preservam o prefixo verificado na prévia; remoções e recuperações inválidas geram invalidação e não podem ser revertidas por revisões atrasadas. O servidor continua sendo a fonte autorizada da disponibilidade, e eventos indicam quais consultas precisam ser atualizadas.

A meta depende também do intervalo entre quadros de acesso aleatório da câmera. Fontes que impeçam esse atraso terão a disponibilidade real apresentada com indicação de atraso, sem reconfiguração automática da câmera nem transcodificação adicional para cumprir a meta.
