# Componentes de terceiros

O pacote é preparado para validação interna. A publicação externa depende da
conferência dos avisos, licenças e materiais exigidos para os binários
efetivamente incluídos; o manifesto registra os hashes desses arquivos.

| Componente | Origem | Observação |
|---|---|---|
| Go e módulos | go.mod, go.sum e `go version -m` | Preservar avisos das dependências e inventário das versões |
| Next.js/React e módulos | package-lock.json | Bundle standalone; avisos dos módulos distribuídos |
| Node.js | nodejs.org | Incluir LICENSE correspondente ao runtime |
| PostgreSQL | postgresql.org / distribuição instalada | Preservar COPYRIGHT e avisos da distribuição |
| MediaMTX | github.com/bluenviron/mediamtx | Incluir licença da versão selecionada |
| FFmpeg/ffprobe | build selecionado pelo empacotador | A configuração de build determina as obrigações aplicáveis |

O laboratório usa um build FFmpeg completo com GPL. Não presumir que qualquer
binário FFmpeg tem a mesma licença. Para uma distribuição, registrar versão,
configuração de compilação, origem e fornecer os materiais correspondentes
exigidos pela licença. Veja a [documentação oficial do FFmpeg](https://ffmpeg.org/legal.html).
Não declarar este documento, isoladamente, como validação de redistribuição.
