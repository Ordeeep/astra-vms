# Pacote nativo Ubuntu 24.04

Alvo inicial: Ubuntu 24.04 x86_64, systemd e acervo em ext4 local com blocos de 4096 bytes. Windows conserva seus próprios adaptadores e instalador. Não há suporte declarado a NFS/SMB, overlay, tmpfs, Btrfs, XFS ou ARM neste primeiro pacote.

## Build no Linux

Instale Go compatível com `backend/go.mod`, Node.js 24 com npm, FFmpeg/ffprobe, Python 3 e o binário Linux do MediaMTX. Use distribuições oficiais, confira os SHA-256 e preserve suas licenças. O build precisa de internet para npm/módulos Go/fontes. Não copie `node_modules` ou `.next` de Windows.

Na raiz do repositório:

```bash
export MEDIAMTX=/caminho/mediamtx/mediamtx
export ASTRA_VERSION=ubuntu-20261119-01
bash backend/deploy/linux/build-package.sh
```

Mantenha `LICENSE` junto ao MediaMTX e o arquivo `LICENSE` na raiz da distribuição Node. O empacotador inclui esses materiais. O resultado em `backend/dist/astra-linux-amd64-<versão>` contém core, worker, Node, MediaMTX, frontend standalone, scripts, units e manifesto SHA-256. PostgreSQL e FFmpeg permanecem pacotes do sistema operacional e precisam ser mantidos no destino. O manifesto verifica integridade; sua autenticidade depende do canal pelo qual o pacote foi obtido.

O frontend é compilado por `npm ci` e `npm run build`. O backend compila apenas `cmd/vms` e `cmd/vms-worker`; o supervisor SCM Windows não integra o pacote Linux. `BUILD-INFO` identifica runtimes e revisão informada; use `ASTRA_SOURCE_REVISION` ao compilar a partir de snapshot sem Git.

## Instalar e operar

Consulte [deploy Linux](../../../docs/deploy/linux.md). O instalador requer root, Ubuntu 24.04 x86_64, PostgreSQL 16 local em TCP 5432 e FFmpeg/ffprobe no PATH. Não instala dependências automaticamente e não reconfigura banco, Docker, firewall ou serviços de desktop existentes.

Os diretórios de instalação são fixos nesta primeira versão:

| Caminho | Conteúdo |
| --- | --- |
| `/opt/astra/releases/<versão>` | Pacotes preservados, pertencentes a root |
| `/opt/astra/current` | Link da versão ativa |
| `/etc/astra` | Identidade da instalação e ambientes privados |
| `/var/lib/astra/core` | Certificado, chave de câmera e dados privados do core |
| `/var/lib/astra/media` | Configuração privada do worker |
| `/srv/astra/archive` | Vídeos, exportações e ledger |

O worker possui escrita somente em seus dados e no acervo, além do temporário privado. Alterar o caminho do acervo requer migração específica e ajuste de `ReadWritePaths`/`RequiresMountsFor`; a interface não migra um acervo já configurado.

O gerenciamento Linux usa um pequeno processo guardião reexecutado para cada ferramenta de mídia. Ele recebe EOF quando o dono termina, inclusive abruptamente, e encerra o grupo do comando e seus descendentes que não se desassociam. Cancelamento e `Stop` usam esse guardião. Os serviços systemd usam `KillMode=mixed` para permitir fechamento pelo processo principal e eliminar o cgroup após o prazo de parada. Ferramentas que daemonizam ou abandonam seu grupo não fazem parte desse contrato de CLI.

O gravador usa `flock`, `statfs`, `fallocate(KEEP_SIZE)`, medição de blocos alocados e fsync de diretório. Não substitui reserva física por arquivo esparso; falha de reserva mantém o gravador indisponível e a cobrança conservadora. O acervo é privado do worker: não abra, renomeie ou exclua seus arquivos com ferramentas externas enquanto ele estiver ativo.

Nesta primeira implementação, reservas abertas maiores que 16 KiB admitem temporariamente até seis vezes o tamanho dos dados para cobrir o pior caso da árvore de extents do ext4, sem presumir contiguidade ou ocupação mínima dos nós. Apenas os dados são pré-alocados; ao finalizar, a cobrança passa a ser a alocação física observada, incluindo os metadados. Esse comportamento pode antecipar recusa ou sobrescrita perto da cota e limita o tamanho de uma exportação em andamento em relação ao orçamento disponível. Dimensione folga para arquivos abertos e valide a carga; otimizar essa reserva requer outra demonstração de limite físico. O cálculo Windows permanece inalterado.

## Verificação

Em um PostgreSQL descartável, configure `VMS_TEST_DB`, `VMS_TEST_MEDIAMTX` e, para captura integrada, `VMS_TEST_RTSP` com fonte sintética. Nunca use o banco da instalação para a suíte. Use um `TMPDIR` em ext4 local com blocos de 4096 bytes.

```bash
cd backend
go build -o bin/vms-worker ./cmd/vms-worker
go test ./... -p 1 -count=1 -timeout 600s
go test -race ./internal/storage ./internal/media ./internal/servicehost -count=1 -timeout 600s
```

`VMS_TEST_MULTISOURCE=1` habilita o ensaio opcional com oito fontes sintéticas. Esses ensaios verificam contratos funcionais; capacidade e homologação de produção dependem também de câmeras físicas, clientes em outras máquinas, operação prolongada e testes de recuperação.
