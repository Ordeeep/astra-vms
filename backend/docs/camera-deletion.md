# Exclusão de câmera

`DELETE /api/v1/tenants/{tenant}/cameras/{camera}` recebe `{"revision":N}`, exige sessão, CSRF, contexto do tenant e a ação `camera.delete` sobre a câmera, seu site atual ou o tenant. Administradores globais podem executar a ação; `inventory.manage` não a concede. Não se exige nova autorização sobre os sites anteriores da mesma câmera.

Uma transação marca o cadastro como excluído, remove conexões e credenciais de todas as suas fontes, bloqueia novos usos, encerra a autoridade das sessões e cancela exportações ainda em processamento. Slots dos mosaicos salvos ficam vazios, preservando o layout. A identificação da câmera, a origem histórica e a auditoria permanecem. O cadastro excluído não pode ser reativado nem usado para novas sessões.

A resposta `202` informa `camera_deleted` e “Câmera excluída. As gravações serão removidas em segundo plano.” Uma repetição autorizada da mesma exclusão retorna `202` sem duplicar a operação. Câmera de outro tenant retorna `404`; revisão desatualizada retorna `412`; falta de permissão retorna `403`. Falhas anteriores à confirmação da transação não iniciam a limpeza.

A migração 5 acrescenta `cameras.deleted_at`, `camera_deletions` e a proteção contra criação concorrente de sessões após a exclusão. A tarefa persiste no PostgreSQL; a rotina aguarda o encerramento da captura e dos consumidores antes de remover arquivos. O worker atende `POST /archive/camera-cleanup`, autenticado pela credencial privada de serviço, com tenant, câmera e cursor. Cada passagem percorre até 100 objetos de gravação do tenant, incluindo fontes e sites anteriores. A consulta física inclui o último trecho finalizado e objetos ainda não importados pelo core. Exportações concluídas e dados de outras câmeras/tenants são preservados.

O cursor só avança após uma página bem-sucedida; falha de transporte ou arquivo protegido permite repetir a página com segurança. A rotina retoma após reinício, com intervalo crescente entre tentativas limitado a 60 segundos. `camera.deleted`, `camera.cleanup_pending` e `camera.cleanup_completed` registram aceitação, impedimento e conclusão na auditoria. Tentativas repetidas com o mesmo motivo atualizam a tarefa sem repetir indefinidamente o evento de falha.

O fluxo não cria painel de exclusões, progresso ou notificações posteriores. Falhas ao iniciar aparecem no diálogo e preservam a câmera na lista. Falhas posteriores ficam na auditoria. Core e worker devem ser atualizados juntos para disponibilizar o novo comando privado.

## Validação

Os testes `TestCameraDeletion*` exigem `VMS_TEST_DB` e verificam autorização, revisão, isolamento de tenant, revogação, preservação de exportações prontas, remoção de credenciais e retomada após falha e reinício. Os testes `TestCameraCleanup*`, `TestCameraStop*` e `TestCameraWithout*` usam armazenamento temporário real para verificar paginação, proteção de leitores, preservação de outros objetos e finalização do último trecho.
