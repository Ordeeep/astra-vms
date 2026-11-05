# Exclusão de câmera

Fixture com o componente real e API em memória, sem acesso à instalação:

```powershell
node tests/camera-deletion/build.mjs
node tests/camera-deletion/serve.mjs
```

Abra `http://127.0.0.1:13116`. Confirme nome/site e a remoção das gravações, foco inicial em Cancelar, cancelamento sem DELETE, exclusão com a revisão correta, remoção imediata da linha e o retorno que informa limpeza em segundo plano. Não há acompanhamento nem notificações posteriores.

`?fail` simula falha ao iniciar; `?conflict` simula revisão desatualizada; ambos preservam a linha e mostram erro no diálogo. `?no-delete` mantém a edição e oculta a exclusão. `?light` seleciona tema claro. Conferir teclado e viewport de 390 px.
