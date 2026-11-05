# Contribuir com o Astra

O código próprio é distribuído sob a licença MIT, apresentada em LICENSE. Dependências de terceiros conservam seus próprios termos.

Leia o [README](README.md), o [glossário](GLOSSARY.md), as [decisões de arquitetura](docs/adr/) e o [guia de desenvolvimento](docs/development.md). Frontend e backend devem manter o contrato `/api/v1` compatível.

Descreva o comportamento esperado e como reproduzir o problema. Prefira alterações pequenas e explique sua validação. Mudanças de autenticação, isolamento entre tenants, armazenamento ou mídia exigem testes pertinentes a esses limites.

Use nomes fictícios, domínios reservados de exemplo e fontes sintéticas. Não inclua senhas de câmeras, URLs autenticadas reais, tokens, chaves, bancos de dados, gravações, capturas de instalações reais ou documentação restrita de fabricantes.

Código de terceiros deve indicar origem e preservar os avisos exigidos. Não copie documentação privada para comentários, fixtures ou testes. Caso a redistribuição de uma integração dependa de autorização, resolva isso antes de propor sua publicação.

Execute os comandos do README para a parte alterada e informe quais integrações foram exercitadas ou puladas. Não use banco de produção nos testes. Relate limitações de hardware, codecs e rede quando forem relevantes.

Para possíveis vulnerabilidades ou exposição de credenciais, não publique os valores em uma issue. Antes da abertura do repositório, o mantenedor deve definir um canal privado de relato; não há um endereço de segurança configurado neste documento.
