# Controle das sessões de vídeo

O navegador recebe um identificador opaco de sessão e uma rota de sinalização do core. Credenciais RTSP, token do worker e endereço privado do MediaMTX permanecem no servidor.

O core verifica cookie, contexto da aba, tenant, câmera ativa, versão da fonte e concessão atual. O worker recebe uma autorização com prazo absoluto de até 1,4 s; esperar por locks ou transporte não estende esse prazo. O cliente renova pela API a cada 400 ms, e cada renovação revalida as permissões.

Durante a autenticação WHEP, o worker vincula o token a um único ID nativo e a um único path. Uma autenticação para outro path/ID é recusada. Após receber a resposta de criação da sessão nativa, o core confirma essa etapa ao worker por `POST /leases/{token}/established`, usando a credencial privada de serviço. O worker recusa confirmações tardias de tokens expirados ou revogados.

Essa confirmação distingue dois casos de encerramento:

- Antes da confirmação, `404` no controle do motor pode ser uma corrida com a criação. O vínculo é mantido durante a janela de proteção, e a tentativa é repetida.
- Depois da confirmação, `404` significa que a sessão previamente criada já terminou. `200` também confirma o encerramento. Nesses casos não se repetem comandos para a mesma sessão.

O token revogado continua inválido mesmo depois de liberar o ID nativo. Uma confirmação atrasada não revive o token.

O guard inspeciona expirações a cada 100 ms. Um lote usa até 32 comandos concorrentes e um orçamento total de 250 ms. Falha de controle encerra o processo de mídia pertencente ao worker, evitando entrega sem autoridade. As respostas são consumidas com limite de 64 KiB; o pool preserva até 32 conexões por destino para evitar criação contínua de sockets.

O prazo aprovado de dois segundos exige medição real de entrega; esses limites internos e testes automatizados não substituem o ensaio de rede/carga. A pré-versão identificou uma falha de disponibilidade na repetição de encerramentos; o teste de aberturas sucessivas permanece parte da regressão.

Logs do MediaMTX passam por filtragem de URLs antes de serem escritos. A filtragem acumula uma linha limitada, protegendo também URLs divididas entre chamadas de escrita. Linhas maiores que 64 KiB são omitidas.
