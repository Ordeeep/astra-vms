# Editor de grupos de permissões

`node --experimental-strip-types --test tests/permission-grants.node.mjs` verifica a conservação dos pares ação/escopo, origens de acesso, remoção de duplicatas e ausência de concessões implícitas de consulta ao selecionar exportação.

Para validar o componente de administração completo sem gravar na instalação:

```powershell
node tests/permission-groups/build.mjs
node tests/permission-groups/serve.mjs
```

Abra `http://127.0.0.1:13113`. A fixture usa os componentes reais e responde a todas as chamadas da API em memória. Inclui catálogo paginado, criação e edição, conservação da revisão, último payload salvo, erro de revisão simulável e alternância de tema. `?catalog-error` simula uma falha inicial do catálogo, que deve bloquear a gravação até uma nova tentativa bem-sucedida.

Verifique edição apenas por tenant/site, ações válidas para todas as câmeras do site, filtro sem acentos, acessos herdados e navegação à origem, cancelamento sem persistência, erro de gravação mantendo o rascunho, restauração de foco e largura de 390 px. Não deve haver busca ou seleção de câmeras, nem chamadas ao catálogo de câmeras. As duas concessões antigas por câmera da fixture devem continuar no payload, com um aviso visível, sem conversão automática para site. `?many-sites` carrega 180 sites para verificar paginação, busca e rolagem da lista sem aumentar o diálogo.
