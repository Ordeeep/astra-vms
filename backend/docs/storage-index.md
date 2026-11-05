# Índice local do gravador

O PostgreSQL mantém o domínio, as permissões e o catálogo consultável pelo VMS. O processo de mídia mantém um ledger privado de admissão de escrita, independente da disponibilidade do core. A versão 2 desse ledger usa bbolt 1.5.0, com sincronização transacional habilitada; não acrescenta um serviço à instalação.

O ledger anterior retinha todos os índices de quadros em um mapa. Um ensaio com 1.000 índices de 64 KiB reteve aproximadamente 70 MiB no heap. A versão nova separa os cabeçalhos de alocação dos índices detalhados e utiliza índices ordenados em disco para selecionar os objetos mais antigos, consultar o catálogo e percorrer exportações. A contagem de bytes por tenant/tipo fica em memória; ela cresce com a configuração de tenants, não com o acervo. Escritores e leitores ativos continuam em memória.

Reservar ou ampliar um arquivo exige confirmar a transação do ledger antes da alocação física e da escrita. Reduzir sua cobrança exige observar a alocação física real. Exclusão que falha mantém a cobrança. Checkpoints sincronizam o vídeo antes de confirmar tamanho, hash e proveniência no ledger. No reinício, somente checkpoints íntegros viram objetos consultáveis.

As consultas de produção usam páginas de até 100 objetos. A reconciliação de início percorre os cabeçalhos e os arquivos em lotes; somente carrega índices detalhados para objetos alterados ou recuperados. O sistema não monta uma lista de todos os arquivos ou índices para servir uma página do catálogo. Identidades de comandos e gerações aposentadas ficam persistidas em disco, impedindo reutilização após reinício.

A adoção de instalações de laboratório anteriores importa `index.json` e `ledger.log` em uma transação e só então marca o formato novo. Os originais ficam preservados com sufixo `.legacy`. Essa adoção inicial ainda lê o snapshot antigo em memória; ela se destina aos acervos da pré-versão. Não é um mecanismo de migração de acervos arbitrários em produção. Não execute um worker antigo sobre um acervo já migrado.

O ensaio automatizado mede heap retido, não memória residente do sistema operacional, latência de disco, crescimento do arquivo bbolt ou capacidade P1. O índice usa mapeamento de memória e páginas reutilizáveis; a qualificação de 24 horas precisa medir também working set, espaço de metadados e desempenho de sobrescrita. O espaço operacional do índice deve entrar no dimensionamento do volume e da margem; não corresponde à cota de vídeo do tenant.

Referência de implementação: [bbolt — transações, cursores e limitações](https://pkg.go.dev/go.etcd.io/bbolt).
