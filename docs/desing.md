# Guia de design de interface

Adote a paleta `neutral` do shadcn, navegação lateral compacta, superfícies discretas e uma hierarquia clara entre título, conteúdo e ações. Priorize a leitura rápida em telas com muitos dados e reserve cor, sombra e movimento para indicar estado, interação ou sobreposição.

## 1. Tema e acabamento

- Use shadcn com o preset `radix-nova`, `baseColor: neutral` e variáveis CSS. Adote o tema claro como padrão e mantenha as mesmas funções de cor no tema escuro.
- Use os pares `background/foreground` para a página, `card/card-foreground` para painéis e `popover/popover-foreground` para superfícies flutuantes. No tema escuro, use um tom ligeiramente mais claro que o fundo para distinguir cards e popovers.
- Use `primary/primary-foreground` na ação principal, `secondary` nas ações de menor ênfase e `muted` ou `accent` em fundos suaves, hover e seleção. Use `muted-foreground` para informação complementar legível, `border` para divisórias, `input` para contorno de campos e `ring` para foco de teclado.
- Reserve verde para sucesso, âmbar para atenção e `destructive` para erro ou ação destrutiva. Combine a cor com texto ou ícone reconhecível. Em gráficos, diferencie séries também por rótulo, forma ou traço.
- Defina `--radius: 0.625rem` como base. Use cantos menores em controles, cerca de 12–14 px em cards e painéis e raio total em badges compactos. Prefira bordas finas e sombras leves. Dê mais profundidade a menus, popovers e dialogs.

## 2. Tipografia, ícones e densidade

| Elemento | Referência |
| --- | --- |
| Título de página | Manrope, 28 px, peso 800, entreletra levemente fechada |
| Título de dialog | 18 px, peso 600 |
| Título de seção ou card | Manrope, 14–16 px, peso 500–700 |
| Corpo e descrições | DM Sans, 13–14 px, com entrelinha próxima de 1,5 |
| Rótulos, controles e tabelas compactas | DM Sans, 12–14 px |
| Métricas | Manrope, cerca de 24–28 px, peso 800. Use algarismos tabulares para valores comparáveis e contadores |
| Ícones | Lucide, 14–16 px em controles compactos e 18–20 px em atalhos e títulos de seção |

- Use uma escala de espaçamento com 4, 8, 12, 16, 24 e 32 px. Comece com 6–8 px entre ícone e texto, 8–12 px entre controles, 16 px entre blocos relacionados e 24–32 px entre seções.
- Mantenha botões compactos em torno de 32–36 px e campos em torno de 36–40 px no desktop. Em telas de toque, amplie a área acionável dos controles, especialmente fechar, alternar e paginar. Use pelo menos 16 px no texto de campos editáveis em telas pequenas.
- Use frases curtas, títulos em caixa de frase e verbos claros nos botões. Reserve letras maiúsculas para rótulos breves de agrupamento. Dê nome acessível a cada botão só com ícone e use `Tooltip` quando o significado precisar aparecer visualmente.

## 3. Estrutura de página

- Componha a navegação com `Sidebar` do shadcn. Use 256 px aberta e 48 px recolhida como referência no desktop. Agrupe links, use divisórias discretas e destaque o item atual com fundo suave e um traço interno na lateral. Mantenha rótulos acessíveis e tooltips no modo recolhido. Em telas pequenas, abra a navegação em um `Sheet` lateral.
- Mantenha a barra superior compacta, com cerca de 64–70 px no desktop, borda inferior fina e posição fixa ao rolar quando houver espaço. Coloque o acionador da lateral e o caminho de navegação à esquerda, com ações de uso geral à direita.
- Centralize a área principal, com largura útil de até 1.600 px e recuo inicial de 20 px na vertical e 24 px na horizontal. Use 16 px nas laterais em telas pequenas. Listagens largas podem aproveitar toda a largura útil.
- Alinhe título e descrição à esquerda e a ação principal à direita. Coloque a barra de ferramentas abaixo do título. Em telas estreitas, quebre a linha e mantenha a ordem visual e de teclado coerentes.
- Use `Card` para painéis com um propósito próprio. Separe o cabeçalho, o conteúdo e o rodapé apenas quando houver informação ou ações para cada parte. Use `Card asChild` quando o painel precisar da semântica de `section`, `article` ou `aside`, e `Button asChild` para links de ação.
- Em grids de resumo, mantenha alinhados os títulos, valores e unidades. Passe de várias colunas para duas e depois para uma conforme o conteúdo perder espaço. Em layouts de lista e detalhe lado a lado, empilhe as duas regiões em telas estreitas.

## 4. Listagens e ferramentas

- Use um `DataTable` compartilhado. Organize busca e filtros à esquerda e controles de colunas e ações da listagem à direita. Deixe a ordenação disponível nos cabeçalhos e reúna ordenações com vários critérios em um popover quando necessário. Mostre filtros ativos e ofereça limpeza direta.
- Use cabeçalho de cerca de 40 px, linhas de 48 px e recuo lateral de aproximadamente 12 px nas células como ponto de partida. Dê mais altura a linhas com conteúdo de múltiplas linhas. Alinhe números comparáveis à direita e use cabeçalho fixo quando a tabela tiver rolagem vertical própria.
- Reserve a primeira coluna para seleção quando houver ações em lote. Destaque linhas selecionadas, mostre a contagem e indique se a seleção vale para a página ou para todos os resultados. Agrupe ações de linha à direita e use `DropdownMenu` para as menos frequentes.
- Separe visualmente a paginação no rodapé. Mostre o intervalo de resultados ou a contagem selecionada, o tamanho da página e a página atual. Em telas estreitas, deixe a barra de ferramentas e o rodapé quebrarem linha e mantenha a rolagem horizontal dentro da tabela.

## 5. Formulários e ações

- Use primitivas shadcn como `Label`, `Input`, `Textarea`, `Select`, `NativeSelect`, `Checkbox`, `RadioGroup` e `Switch`. Posicione o rótulo acima do campo, coloque ajuda e erros abaixo e associe-os de forma acessível. Use placeholder para exemplos curtos.
- Organize campos relacionados em até duas colunas no desktop. Dê largura total a textos longos e campos que precisem de espaço. Empilhe tudo em uma coluna nas larguras menores.
- Use um `Button` sólido para a ação principal de cada área, `outline` ou `secondary` para ações auxiliares e `ghost` para ações de baixa ênfase. Combine ícone e texto quando o rótulo ajudar a entender a ação. Use `Badge` para exibir um estado e um controle interativo para alterá-lo.
- Durante o envio, mantenha o botão no mesmo lugar, sinalize o progresso e evite cliques repetidos. Em caso de erro, preserve os valores digitados e mostre o problema junto ao campo ou ao bloco afetado.

## 6. Dialogs, painéis e menus

| Componente | Quando usar | Composição |
| --- | --- | --- |
| `Dialog` | Formulário curto, detalhe pontual ou decisão reversível | Centralizado, com título, descrição quando útil e botão de fechar. Use cerca de 480–560 px para uma coluna e 640–760 px para formulários mais amplos |
| `AlertDialog` | Confirmação de uma ação destrutiva ou difícil de reverter | Cerca de 440–520 px, com consequência explícita, `Cancelar` e um botão que nomeie a ação. Dê foco inicial a `Cancelar` e exija uma escolha explícita |
| `Sheet` | Filtros extensos, opções contextuais ou navegação móvel | Abra à direita para ferramentas e à esquerda para navegação. Use cerca de 360–400 px como referência para ferramentas, com largura adaptada à tela |
| `Popover` | Escolha rápida, filtro ou ajuste ligado a um controle | Ancore ao acionador, respeite a área visível e limite a altura de listas com rolagem interna |
| `DropdownMenu` | Ações secundárias de um item ou grupo | Use rótulos curtos e separadores entre grupos quando ajudarem a leitura |
| `Tabs` | Alternar entre vistas relacionadas no mesmo contexto | Mantenha poucos rótulos, alinhados ao início, com estado ativo visível e mudança por teclado |

- Estruture dialogs com `DialogHeader`, título, corpo e `DialogFooter`. Para formulários longos, use cabeçalho e rodapé fora da região rolável e deixe apenas o corpo rolar. Separe essas regiões com bordas sutis quando a rolagem exigir.
- Use 20–24 px de espaço interno como referência. Reserve 12–16 px nas bordas da janela e limite a altura com `dvh` para manter o cabeçalho e as ações alcançáveis em telas pequenas. Prefira uma página dedicada quando a tarefa ocupar a maior parte da janela ou exigir vários passos.
- Alinhe as ações à direita no desktop, com `Cancelar` antes da ação principal. Em telas pequenas, empilhe os botões em largura total quando os rótulos não couberem lado a lado.
- Use portais do Radix e um fundo escurecido consistente, em torno de 50% de opacidade. Mantenha menus e seletores abertos dentro de um dialog em uma camada acima dele. Use transições breves de abertura e fechamento e respeite a preferência por movimento reduzido.
- Leve o foco para dentro do modal ao abrir, mantenha a navegação por Tab dentro dele e devolva o foco ao acionador ao fechar. Permita Escape em dialogs comuns. Se houver alterações a descartar, confirme antes de fechar por Escape, pelo botão de fechar ou por clique fora.

## 7. Estados e revisão visual

- Use `Skeleton` com as dimensões aproximadas do conteúdo inicial. Ao atualizar dados, mantenha a estrutura estável e preserve a posição de leitura sempre que possível.
- Diferencie lista inicialmente vazia de busca sem resultados. Na primeira, mostre o próximo passo se houver. Na segunda, ofereça ajustar a busca ou limpar os filtros.
- Use `Alert` próximo do conteúdo para mensagens que precisam continuar visíveis e notificações breves para confirmações. Preserve contraste e foco visível nos modos claro e escuro.
- Revise em larguras de aproximadamente 390, 768, 1280 e 1600 px. Confira textos longos, zoom de 200%, rolagem interna de tabelas e dialogs, estado selecionado, mensagens de validação e botões carregando.
- Percorra a interface com Tab e Shift+Tab. Verifique também Escape nas sobreposições, Enter e Espaço nos controles aplicáveis, setas em menus e abas, e o retorno do foco após fechar.
