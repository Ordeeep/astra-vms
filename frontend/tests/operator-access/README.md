# Vínculos de operadores e grupos

A tela Operadores concentra o cadastro global e os grupos do ambiente selecionado. Administradores de permissões do ambiente podem editar somente os grupos de operadores já vinculados. A criação e a edição globais aceitam `membership: {tenantId, groups, revision}` e gravam conta e grupos na mesma transação; omitir `membership` preserva os vínculos.

Validação do formulário com API simulada (requer Chrome):

```powershell
cd frontend
node tests/operator-access/build.mjs
node tests/operator-access/run.mjs
```

O teste `TestOperatorProfileAndGroupsAreAtomic` usa PostgreSQL real com `VMS_TEST_DB`, cria um schema isolado e verifica rollback, revisões, remoção de grupos e isolamento entre ambientes.

Os grupos existentes da instalação são gerenciados em `/admin/installation-groups`. Os grupos do ambiente continuam em `/tenants/{tenant}/groups`. Ambos usam o mesmo editor, com a opção **Administrar instalação** acima das permissões individuais. Novos grupos são criados no ambiente selecionado ou, quando não há ambiente, na instalação.

A concessão `installation.manage`, com escopo e recurso `installation`, pode integrar um grupo do ambiente e concede todas as ações em todos os ambientes ativos, inclusive os criados depois. A interface mostra essas ações selecionadas e desabilitadas enquanto a opção está marcada, inclusive ao navegar entre sites. As concessões individuais são preservadas: ao desmarcar, a seleção anterior reaparece. O servidor calcula o acesso a partir do vínculo atual, sem copiar concessões. Grupos existentes da instalação também podem preservar concessões individuais dos ambientes.

A interface reúne os grupos na tela **Grupos de permissões**, com coluna e filtro de escopo. Grupos com administração da instalação aparecem com escopo Instalação e Acesso completo. Gestores locais não podem marcar essa opção, alterar grupos que a contenham ou atribuir/remover seus vínculos. A API aplica essas restrições e protege o último administrador ativo também na edição de grupos, vínculos e desativação de ambientes.

A fixture verifica os fluxos de operadores, a listagem unificada, o vínculo e bloqueio de todas as ações, a restauração da seleção individual, a persistência ao reabrir, os grupos existentes, a ausência de requisições administrativas por gestores locais e a disposição em telas menores. A administração completa pode ser salva mesmo quando o catálogo de sites está indisponível.

O cadastro do operador recebe `installationGroups: string[]` e o vínculo opcional `membership` do ambiente na mesma transação. O campo de resposta `globalAdmin` é uma capacidade derivada; enviar esse campo na criação/edição é rejeitado. Omitir `installationGroups` preserva os vínculos existentes.

A migração 7 converte os administradores existentes em membros de Administradores da instalação e remove a coluna antiga. Os testes `TestInstallationGroups*` verificam migração, isolamento de escopos, revogação e proteção do último administrador ativo.

A migração 8 permite que grupos de ambientes também sejam a origem da administração da instalação. Essa origem é válida enquanto o ambiente estiver ativo. `TestEnvironmentGroupLinksInstallationAdministrationAndRestoresGranularAccess` e `TestLinkedInstallationAdministrationRejectsDelegatedChangesAndProtectsLastAdmin` verificam acesso em ambiente futuro, revogação, restauração das concessões individuais e proteção contra alterações por gestores locais.
