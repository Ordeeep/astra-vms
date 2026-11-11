# Astra · Gestão de vídeo

Astra é um sistema de gestão de vídeo (VMS) com interface em português no navegador. Um servidor central acessa fontes RTSP, apresenta câmeras ao vivo e mantém um acervo local para reprodução e exportação.

**Estado: em desenvolvimento.** Código próprio disponível sob a [licença MIT](LICENSE). Veja [publicação e privacidade](docs/publication.md). A instalação em produção ainda requer homologação.

## Recursos

- Login por usuário ou e-mail, conta pessoal e alteração de senha.
- Tenants, operadores e grupos de permissões; sites e câmeras pertencem a um tenant.
- Cadastro de fontes RTSP e teste de conexão sem salvar a câmera.
- Visualização ao vivo por WebRTC, mosaicos, seleção de perfis e maximização de câmera.
- Agenda de gravação, retenção e cotas de armazenamento.
- Linha do tempo, reprodução e exportação MP4 acompanhada de relatório JSON; períodos sem gravação aparecem como lacunas.
- Auditoria, diagnóstico e atualizações de estado por eventos SSE.

Administrar a instalação não concede acesso automático ao vídeo de todos os tenants. A API verifica as permissões de cada recurso.

As integrações Dahua e Hikvision são mantidas, incluindo os recursos PTZ já implementados. Manuais de fabricantes e suas extrações não são distribuídos com o código.

## Arquitetura

| Diretório | Responsabilidade |
| --- | --- |
| `frontend/` | Interface em Next.js, React e TypeScript; servidor Node.js em loopback |
| `backend/cmd/vms/` | Entrada HTTPS, API, autenticação, autorização e proxy da interface |
| `backend/cmd/vms-worker/` | Processamento de mídia, gravações e exportações com MediaMTX e FFmpeg |
| `backend/cmd/vms-service/` | Supervisor dos serviços nativos do Windows |
| `backend/internal/` | Domínio, integrações, mídia e armazenamento |
| `docs/adr/` | Decisões de arquitetura |

PostgreSQL guarda os metadados. Vídeos e índices ficam no disco local. A operação é local, sem serviço de nuvem obrigatório. Baixar dependências e compilar a interface pode exigir internet, inclusive para a fonte usada por `next/font/google`.

O workspace original mantém frontend e backend em repositórios Git separados. A ferramenta de preparação reúne os arquivos selecionados em uma cópia de fontes sem os históricos desses repositórios. Não execute `git add .` no workspace original para fazer a primeira publicação.

## Requisitos

O alvo inicial é **Windows 11**, com armazenamento NTFS. Linux e Docker ainda não possuem um procedimento de instalação homologado.

- Go 1.27 ou compatível com `backend/go.mod`.
- Node.js 24 e npm para os comandos documentados.
- PostgreSQL 16 para o procedimento de referência.
- MediaMTX, FFmpeg e ffprobe, obtidos separadamente de suas distribuições oficiais.
- Navegador com WebRTC e suporte ao codec utilizado; H.264 é o ponto de partida para validação.
- Conectividade do servidor com as câmeras e espaço dimensionado para o uso.

As versões das bibliotecas JavaScript estão em `package-lock.json`; as dos módulos Go estão em `go.mod` e `go.sum`. A seleção de fontes não inclui binários de terceiros nem bancos de laboratório.

## Baixar, executar e modificar

Quando o repositório estiver publicado, use **Code → Download ZIP** no GitHub ou clone a URL exibida pelo próprio repositório. Abra a pasta que contém este README, `frontend` e `backend`.

Consulte [desenvolvimento local](docs/development.md) para preparar PostgreSQL, compilar e iniciar os três processos sem depender do laboratório do mantenedor.

Para construir uma instalação com serviços Windows, consulte [empacotamento e instalação](backend/deploy/windows/README.md). O pacote é gerado a partir dos fontes e exige caminhos explícitos para os runtimes. Os avisos e materiais de redistribuição de cada binário precisam ser conferidos antes de disponibilizar um instalador.

## Primeiro acesso

1. Abra o endereço HTTPS configurado no core; no desenvolvimento, o padrão é `https://localhost:18443`.
2. Em uma instalação nova, entre com **`admin` / `admin`** e altere imediatamente a senha em **Minha conta**, antes de permitir acesso pela rede. Essa credencial de bootstrap é pública e está no código; não é um segredo particular da instalação.
3. Cadastre um site e uma câmera RTSP em **Câmeras e locais**. Use credenciais da sua própria câmera, sem copiá-las para arquivos versionados.
4. Arraste a câmera para o mosaico em **Ao vivo**.
5. Configure a pasta de gravação, cotas e agenda; depois confira os intervalos disponíveis no acervo.
6. Cadastre operadores e conceda apenas as permissões necessárias em cada tenant.

O core gera um certificado local no primeiro início. Para acesso em rede, configure certificado, DNS, firewall e conectividade WebRTC conforme o ambiente. Mantenha as portas de controle do banco, worker e MediaMTX em loopback.

## Verificações de desenvolvimento

Na pasta `frontend`:

```powershell
npm ci
npm run typecheck
npm run test:recording-timeline
npm run build
```

Na pasta `backend`:

```powershell
go test ./...
```

Sem as variáveis de integração, parte dos testes Go é pulada. Um resultado verde nessas condições não comprova PostgreSQL, vídeo real nem instalação Windows. Veja [validação](backend/docs/validation.md) para os requisitos adicionais.

## Limites conhecidos

O projeto não tem comprovação de capacidade para um número garantido de câmeras. Ainda são necessários ensaios com câmeras físicas, diferentes codecs e navegadores, clientes separados do servidor, operação prolongada, disco cheio e instalação/atualização/reinício em Windows limpo. Testes sintéticos não substituem essa homologação.

A gravação é centralizada: uma falha de conectividade com a câmera pode criar uma lacuna que o servidor não recupera automaticamente. Integração com gravações de cartão, NVR ou DVR não faz parte do procedimento descrito.

## Contribuir

Consulte [CONTRIBUTING.md](CONTRIBUTING.md). Inclua passos para reproduzir o problema e dados fictícios. Nunca anexe credenciais, vídeos de pessoas, dumps de banco ou documentação privada de fabricantes em issues, commits ou pull requests.

O vocabulário está em [GLOSSARY.md](GLOSSARY.md). As regras de autorização devem continuar sendo verificadas no backend.

## Licença e terceiros

Licença MIT para o código próprio: consulte [LICENSE](LICENSE).

As dependências conservam suas próprias licenças. O login usa o Topography do React Bits sob [MIT + Commons Clause](frontend/public/licenses/react-bits.txt). A exportação pública usa a cena CSS original OrbitalDawn. Consulte [THIRD-PARTY.md](backend/deploy/windows/THIRD-PARTY.md) antes de distribuir binários.
