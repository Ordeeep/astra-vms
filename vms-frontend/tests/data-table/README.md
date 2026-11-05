# Validação das tabelas

Fixture do componente real `DataTable` com 37 registros sintéticos e filtros de texto, seleção, data e número. Não acessa backend, autenticação nem câmeras. Usa webpack/SWC já distribuídos pelo Next e PostCSS/Tailwind já instalados no projeto.

Na pasta `vms-frontend`:

```powershell
node tests/data-table/build.mjs
node tests/data-table/serve.mjs
```

Abra `http://127.0.0.1:13111` com o Playwright CLI. O servidor fica restrito ao loopback.

O botão “Alternar tema” no topo permite conferir os temas claro e escuro. `?dark` inicia no tema escuro; para integração, use `?integration&dark#cameras`. A fonte de fallback da fixture é Arial, pois o carregamento de `next/font` pertence à aplicação Next completa.

Verifique:

- Busca por `camera 01` encontra `Câmera 01`, sem depender de acentos.
- Filtro Operação permite combinar valores; filtro Câmera fica nas opções do cabeçalho.
- Cabeçalhos Cadastro e Quantidade oferecem intervalos e ordenação por valores reais.
- A primeira tabela tem quatro páginas inicialmente; alterar o tamanho ou filtros volta à primeira.
- “Simular polling” troca a referência dos dados, preservando a página atual.
- Na última página, “Reduzir para 3 registros” ajusta a paginação para uma página válida.
- Menu Colunas oculta e restaura colunas; Câmera é obrigatória.
- Histórico declara o escopo carregado e amplia os dados com “Carregar registros anteriores”.
- A terceira tabela mostra seu estado vazio; busca sem correspondência mostra estado de filtros vazios e permite limpar.
- Popovers, menus e navegação são utilizáveis por teclado; a largura de 390 px mantém a rolagem horizontal da tabela.

Não há testes `@playwright/test` neste diretório; a fixture destina-se à validação pelo navegador real via CLI.

Para executar os roteiros a partir da raiz `projeto-vms`, com o servidor ativo:

```powershell
New-Item -ItemType Directory -Force output/playwright/data-table
npx --yes --package @playwright/cli playwright-cli -s=tables open http://127.0.0.1:13111
npx --yes --package @playwright/cli playwright-cli -s=tables run-code --filename vms-frontend/tests/data-table/check.cjs
npx --yes --package @playwright/cli playwright-cli -s=tables run-code --filename vms-frontend/tests/data-table/integration-check.cjs
npx --yes --package @playwright/cli playwright-cli -s=tables close
```

`check.cjs` verifica filtros combinados, intervalos numéricos e de datas,
ordenação, visibilidade, teclado, estados vazios, paginação durante polling,
carregamento parcial e layout em 390 px. `integration-check.cjs` verifica as
11 tabelas dos workspaces, busca em arrays, ações, 237 trechos no acervo,
cursores e descarte de respostas antigas ao trocar aba ou escopo da auditoria.
As capturas são gravadas em `output/playwright/data-table/` na raiz.

Para smoke de integração, abra `http://127.0.0.1:13111/?integration#cameras`. Os links no topo montam os workspaces reais de câmeras, sites, administração, exportações, gravação, consulta e operação. Todos os endpoints são simulados em memória, inclusive páginas de API com cursor; chamadas desconhecidas retornam 404 simulado. `window.syntheticTableRequests` registra as requisições para inspeção no navegador. Formulários de alteração e players não são simulados neste smoke.

Na tela `playback`, selecione a primeira câmera e busque para obter 237 segmentos, cobrindo a remoção do antigo corte em 200. Em `operations`, alterne Diagnóstico/Auditoria e carregue registros anteriores. O link `management` abre Acessos; `tenants`, `groups` e `operators` abrem as telas dedicadas. Os demais cadastros têm entre 14 e 37 registros para exercitar a paginação.
