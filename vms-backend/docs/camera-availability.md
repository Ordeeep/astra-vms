# Disponibilidade de câmeras

A listagem de câmeras inclui `availability` nos mesmos endpoints e sob as mesmas permissões de inventário existentes. Ler a lista ou uma câmera só consulta o estado em memória; não dispara testes. O acompanhamento funciona no core mesmo sem navegador ou gravação, usando o worker privado para receber vídeo.

| Estado na API | Interface | Critério |
| --- | --- | --- |
| online | Online | Pelo menos um perfil da configuração atual forneceu uma unidade de vídeo H.264 ou H.265 com imagem. |
| offline | Offline | Duas verificações completas consecutivas sem vídeo em nenhum perfil habilitado. |
| unknown | Sem informação | Primeiro acompanhamento, resultado vencido, configuração alterada, verificação incompleta ou falha local. |
| suspended | Monitoramento suspenso | Câmera, site ou tenant desativado. Não consultar a fonte. |

Além de `state`, o resultado pode conter `code`, `checkedAt`, `lastVideoAt`, `validUntil`, `profile` e `warnings`. Os avisos identificam o perfil e o horário da falha conhecida; não contêm URLs ou credenciais. `checkedAt` é a última tentativa concluída e não deve ser confundido com `lastVideoAt`, a última evidência positiva. A primeira falha completa mantém o estado confirmado durante a tolerância; um sucesso recupera Online imediatamente.

## Recepção e reaproveitamento

A confirmação exige uma unidade de vídeo montada a partir de RTP, contendo dados de imagem; SDP, metadados, áudio e conjuntos de parâmetros sozinhos não confirmam Online. A verificação não decodifica pixels, não transcodifica nem grava arquivos. Uma cena sem movimento continua válida quando novas imagens chegam. Não há ping/ICMP nesta versão.

Antes de abrir uma conexão com a câmera, o worker procura recepção de gravação com quadro recente e plano ainda autorizado, vinculada a tenant, câmera, fonte, revisão da conexão, perfil e URL atuais. Para o ao vivo, pode ler o caminho RTSP local já preparado, da revisão atual, e confirmar uma imagem sem criar outra conexão com o equipamento. Apenas o indicador Ready do MediaMTX não basta. Se esses caminhos não confirmarem vídeo, testa as fontes disponíveis com concorrência limitada.

Um perfil funcionando basta, inclusive um substream Dahua quando o principal falha. Perfis não verificados permanecem sem diagnóstico. Um ciclo incompleto não conta como falha completa da câmera, e falhas do worker ou capacidade local não determinam Offline. A indisponibilidade do disco ou da gravação não derruba uma evidência válida de recepção.

## Limites atuais

| Parâmetro | Valor |
| --- | --- |
| Reavaliação por câmera | 30 segundos entre inícios, sem sobreposição |
| Confirmação de queda | Duas verificações completas consecutivas |
| Validade do resultado concluído | 65 segundos; inclui a próxima cadência e o orçamento da consulta |
| Atualização da listagem | 10 segundos, somente leitura |
| Reconciliação dos cadastros | A cada segundo; sem reconciliação por mais de 5 segundos, retornar Sem informação |
| Câmeras verificadas simultaneamente no core | 4 |
| Testes de fonte simultâneos no worker | 8 no total, até 4 por câmera |
| Recepção direta por perfil | Até 4 segundos |
| Recepção por fluxo local já aberto | Até 2 segundos |
| Orçamento de uma consulta | 24 segundos no worker; 26 no core |
| Idade da evidência de uma captura reaproveitada | Menos de 3 segundos |

Os testes são cancelados após sucesso de um perfil, edição, suspensão, exclusão ou encerramento do serviço. Espera por capacidade e prazo total esgotado sem concluir todos os perfis produzem Sem informação. Os limites evitam acessos ilimitados; não constituem homologação para uma quantidade garantida de câmeras. Sob carga, dimensionar esses valores preservando a distinção entre falha da fonte e verificação inconclusiva.

Mudanças de conexão, fonte, perfis ou site invalidam o resultado e cancelam testes antigos. Alterar somente o nome preserva o acompanhamento. Desativar câmera, site ou tenant cancela os testes correspondentes; reativação e reinício do core exigem nova evidência. Os resultados não são restaurados como atuais após reinício. Câmeras excluídas saem do acompanhamento e respostas atrasadas são descartadas.

## Interface e atualização dos serviços

A coluna Disponibilidade apresenta ícone e texto, filtro por estado, detalhes com motivo e horários, e avisos conhecidos de perfis. Os contadores abrangem as câmeras carregadas e autorizadas na listagem. O frontend também expira resultados localmente, trata falhas de atualização como Sem informação e descarta respostas de tenants anteriores. O estado de habilitação permanece separado.

Core, worker e frontend devem ser atualizados juntos: o worker ganhou `POST /availability`, protegido pela autenticação privada de serviço, e os planos de captura incluem a revisão de conexão. Reiniciar core e worker é necessário para carregar o código Go recompilado. Não há migração de banco para esta funcionalidade.

Histórico de quedas, notificações, apresentação no mosaico e remoção de substreams manuais RTSP não fazem parte da entrega.

## Validação

Na pasta do backend:

```powershell
go test ./... -count=1
go test -race ./internal/app ./internal/media -run Availability -count=1
```

Os testes de recepção usam um servidor RTSP sintético local e exercitam H.264/H.265, metadados sem imagem, autenticação, algum perfil funcionando, reaproveitamento de gravação e de ao vivo, capacidade e acesso privado. As transições, validade, revisão, suspensão e isolamento do cache são verificadas sem infraestrutura adicional.

`TestCameraAvailabilityAPIReusesCacheAndInvalidatesEdits` exige `VMS_TEST_DB` de laboratório e cria seu próprio schema. Verifica acompanhamento sem espectador, cancelamento durante edição, invalidação imediata, nome, reativação, exclusão e ausência de testes disparados pelas consultas GET. Não imprime a conexão do banco. Consulte também [validação geral](validation.md).

A fixture do frontend está em [tests/camera-availability](../../vms-frontend/tests/camera-availability/README.md). A validação realizada em 01/11/2026 passou com PostgreSQL de laboratório, fontes RTSP sintéticas, detector de corridas do Go, typecheck, build e navegador real com API simulada. Esses resultados não substituem ensaios prolongados e com câmeras físicas.

### Verificação pontual das câmeras cadastradas

Em 01/11/2026, a verificação das 8 câmeras cadastradas, todas Dahua, confirmou vídeo real nos 24 perfis disponíveis (principal, sub1 e sub2). Um teste independente com FFmpeg decodificou 3 quadros por perfil, sem salvar imagens ou vídeos. A leitura das configurações usou PostgreSQL em modo somente leitura; as configurações permaneceram iguais durante essa verificação.

Uma leitura inicial e quatro rodadas posteriores da API indicaram as 8 câmeras Online, com resultados válidos e idade máxima observada de aproximadamente 28 segundos. Outras quatro atualizações reais da listagem coincidiram com os estados e contadores da API, mostrando 8 Online e zero nos demais estados, com horários e detalhes disponíveis.

Na abertura inicial da listagem foi observado um estado Sem informação temporário na CAM01-CEI AUDIFAX RIOS, já recuperado na consulta seguinte. A causa dessa observação inicial não foi capturada; não deve ser atribuída a uma queda da câmera nem a um defeito de exibição sem evidência adicional. As quatro comparações posteriores da interface não reproduziram a ocorrência.

As evidências locais estão em `output/playwright/camera-availability-real/` na raiz: `result.json` (verificação de vídeo e leituras da API), `browser-result.json` (comparação da interface), `summary.json` e `indicators.png`. Os relatórios não contêm credenciais ou URLs de fontes. Não houve câmera Offline ou falha parcial nos perfis durante a verificação independente; portanto este ensaio não valida transições provocadas de queda/recuperação em equipamentos físicos nem constitui homologação prolongada ou de capacidade.
