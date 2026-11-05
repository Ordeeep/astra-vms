# Astra · frontend

Interface do Astra, sistema local de gestão de vídeo (VMS), em Next.js/TypeScript, com serviço Node.js próprio. Repositório independente de `vms-backend`.

Para o ambiente completo, consulte [desenvolvimento local](../docs/development.md). A licença do código próprio é MIT; veja [publicação](../docs/publication.md).

`npm ci`, `npm run typecheck` e `npm run build` preparam a interface. `npm start` executa o artefato standalone em loopback; o core Go fornece a entrada HTTPS e encaminha a interface. O navegador chama `/api/v1` na mesma origem.

Há telas para ambientes/acessos, inventário, mosaicos/qualidade, gravação/agenda/cotas, reprodução, exportação, auditoria/diagnóstico e conta. O contexto da aba isola os recursos; notificações SSE atualizam os snapshots sem conceder acesso. Os endpoints do core revalidam a autorização.

A listagem de Câmeras mostra disponibilidade, detalhes de falhas por perfil, horários, contadores e filtro de estado. Os indicadores consultam os resultados do servidor a cada 10 segundos e expiram resultados antigos. Veja [o contrato](../vms-backend/docs/camera-availability.md) e [a fixture de validação](tests/camera-availability/README.md).

O pacote Windows é montado pelo backend e inclui os dois artefatos. Consulte [instalação Windows](../vms-backend/deploy/windows/README.md). Contrato público compatível: versão maior 1.

A interface foi exercitada no Chrome com RTSP H.264 sintético e acervo real gerado no laboratório. A matriz completa de navegadores, hardware, carga e latência ainda precisa de homologação.

## Cache e pacote Windows

O serviço Node lê os arquivos da versão instalada sem gravar nela. O cache em memória tem limite de 8 MiB e `experimental.isrFlushToDisk` está desabilitado na versão fixada do Next.js. Dados do operador continuam vindo da API Go. A verificação do pacote solicita página, ícone e rota inexistente e exige que os hashes e a lista de arquivos permaneçam intactos; repetir esse ensaio ao atualizar Next.js. Referência: [self-hosting do Next.js](https://nextjs.org/docs/app/guides/self-hosting).

