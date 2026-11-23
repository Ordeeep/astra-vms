# Identificador de câmera por tenant

O Astra passa a identificar um cadastro na API pelo tenant e por um número inteiro crescente. Cada tenant começa em 1. A migração atribui os números aos cadastros existentes, inclusive aos desativados, pela ordem da primeira alocação registrada. A exclusão preserva o número atribuído e deixa uma lacuna. Câmeras já excluídas antes da migração mantêm apenas sua referência histórica.

A API representa o número em texto, como `"1"`, e continua aceitando o identificador anterior no tenant correto. Concessões por câmera em grupos de instalação também informam o `tenantId`. O catálogo mantém a chave interna, usada pelo cofre de credenciais e pelos metadados e hashes de mídia, para que a atualização preserve o acervo e permita consultar gravações que atravessam a migração.
