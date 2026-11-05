# Issue tracker: Markdown local

Tarefas e especificações ficam em `.scratch/`.

## Convenções

- Uma pasta por funcionalidade: `.scratch/<feature-slug>/`.
- Especificação: `spec.md` dentro dessa pasta.
- Um arquivo por tarefa: `issues/<NN>-<slug>.md`, numerado desde `01`.
- Estado de triagem: linha `Status:` no início da tarefa,
  com os valores definidos em `triage-labels.md`.
- Comentários: acrescentar ao final, sob `## Comments`.

## Operações

Publicar significa criar o arquivo correspondente na pasta da funcionalidade.
Consultar significa ler o arquivo indicado pelo usuário.
Se apenas um número for informado, localizar a tarefa;
pedir esclarecimento se houver mais de uma correspondência.

## Wayfinder

- Mapa: `.scratch/<effort>/map.md`, com notas, decisões e dúvidas.
- Tickets: `.scratch/<effort>/issues/NN-<slug>.md`.
- Tipo em `Type:`: research, prototype, grilling ou task.
- O fluxo Wayfinder usa `Status: open`, `claimed` ou `resolved`.
- Dependências em `Blocked by: NN, NN`.
- Escolher o primeiro ticket por número que esteja aberto e
  cujas dependências estejam resolvidas.
- Antes de trabalhar, salvar `Status: claimed`.
- Ao concluir, acrescentar `## Answer`, salvar `Status: resolved`
  e registrar no mapa um resumo com link para a resposta.
