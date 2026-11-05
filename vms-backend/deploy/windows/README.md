# Pacote Windows do VMS

Este pacote local usa quatro serviços: `VMS-Database` (PostgreSQL), `VMS-Web`
(Next.js/Node), `VMS-Media` (worker/MediaMTX/FFmpeg) e `VMS-Core` (API/HTTPS).
As portas internas usam loopback. O host de serviços reinicia componentes que
encerram e o Windows reinicia o host. Uma parada do core/worker fecha primeiro
a entrada do processo para permitir encerramento normal; após oito segundos
os processos pertencentes ao componente são terminados.

## Construção

No repositório backend, execute `deploy/windows/build-package.ps1`, informando
o executável MediaMTX homologado com `-MediaMTX`. O script recebe caminhos
explícitos para PostgreSQL, Node, FFmpeg, ffprobe, Go e o frontend. Ele compila
os dois repositórios e inclui os runtimes necessários para operação offline.
O pacote em `dist/` tem manifesto SHA-256. Não copie configurações de
desenvolvimento nem segredos para o pacote.

`verify-package.ps1 -Bundle <pasta>` confere os hashes e executa o Node incluído
sob o supervisor, em porta temporária de loopback. O ensaio solicita a página,
encerra somente o filho identificado e verifica seu reinício. Não exige
privilégio administrativo e não registra serviços; não comprova SCM/reboot.
Ao terminar, confere novamente os arquivos para impedir que caches do Node
alterem o pacote. A interface usa cache limitado em memória; a gravação do
cache ISR em disco está desabilitada na versão fixada do Next.js.

As versões dos runtimes e as obrigações de redistribuição devem ser conferidas
em `THIRD-PARTY.md` antes de publicar uma distribuição para clientes.

## Instalação

Em PowerShell elevado, dentro do pacote:

```powershell
.\install.ps1
```

O padrão é `https://localhost:18444`, dados em `C:\ProgramData\VMS`,
binários em `C:\Program Files\VMS`, PostgreSQL exclusivo na porta 56433.
O script recusa diretórios de dados não vazios sem identidade de instalação,
serviços conflitantes, caminhos com reparse points e pacotes alterados.
As contas virtuais `NT SERVICE\VMS-*` recebem acesso aos respectivos dados.

Para uma instalação acessível na rede, defina `-PublicHost` com o DNS/IP
usado pelos navegadores, `-Listen 0.0.0.0:18444` e `-AllowNetworkAccess`.
As regras criadas abrangem somente redes Domain/Private: TCP HTTPS e UDP
18189 para WebRTC. Publicação através de NAT/proxy e redes de cliente exigem
homologação própria; abrir HTTPS não garante conectividade do vídeo.

O core gera um certificado local com esse nome no primeiro início. Confie
nesse certificado nos clientes ou configure `--cert` e `--key` com o
certificado fornecido pela infraestrutura no arquivo privado do componente.
Nenhuma autoridade raiz é instalada automaticamente.

Entre com `admin/admin`. A senha pode ser alterada em Minha conta.
Escolha a pasta local de gravação e as cotas em Gravação e agenda. A pasta
`C:\ProgramData\VMS\archive` já possui a ACL do worker. Para outro volume NTFS,
o administrador deve conceder Modify a `NT SERVICE\VMS-Media` na pasta
específica. Não conceda acesso à raiz de um disco.

## Atualização e recuperação

Execute o instalador do novo pacote com os mesmos `-InstallRoot` e
`-DataRoot`. Ele preserva identidade, senhas, chaves, configurações, PostgreSQL
e acervo. Faz backup de metadados antes de parar os serviços da aplicação.
Cada versão ocupa uma pasta nova; versões anteriores são mantidas.
O binário PostgreSQL fica separado e este atualizador não troca sua versão.

O backup inclui catálogo, configuração e chaves; **não copia os vídeos**.
Para restauração completa, preserve também o volume do acervo e seu ledger
`ledger.db`. Não combine o índice antigo com arquivos já sobrescritos.
Nunca restaure o banco sem a chave `camera-key.bin`. Planeje a cópia dos
vídeos com os serviços de mídia parados. A restauração do catálogo usa
`pg_restore` no cluster exclusivo; faça essa operação em uma cópia antes
de substituir uma instalação em uso.

Falha durante atualização deve ser investigada nos logs e no estado dos
serviços. Não apague o diretório de dados. Retornar a um binário anterior
exige verificar compatibilidade das migrações; não há downgrade automático.

Logs de processo têm duas gerações de até aproximadamente 5 MiB por serviço.
Auditoria funcional fica no PostgreSQL e é consultável no navegador.
O diagnóstico diferencia mídia indisponível, falta de quadros, capacidade
e agenda. A recuperação de arquivos abertos publica apenas fragmentos
confirmados, com hash conferido; a cauda incompleta permanece fora do acervo.

## Homologação

O supervisor é exercitado sem privilégios em testes de processos.
Instalação SCM, contas virtuais, ACLs, reboot e atualização em Windows 11 limpo
precisam ser executados no ambiente de homologação com privilégios administrativos.
Um build e testes de processos não comprovam esses critérios.

Referências: [serviços Go no Windows](https://pkg.go.dev/golang.org/x/sys/windows/svc),
[Next.js standalone](https://nextjs.org/docs/app/api-reference/config/next-config-js/output)
e [pg_ctl](https://www.postgresql.org/docs/16/app-pg-ctl.html).
