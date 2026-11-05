# Fontes para publicação no GitHub

Esta variante reúne os fontes do Astra e mantém as integrações RTSP, Dahua e Hikvision, incluindo o PTZ implementado. Os manuais de fabricantes, suas extrações, capturas, links para cópias de manuais, dados de laboratório, binários e históricos Git antigos não acompanham a publicação.

## Licença

O arquivo `LICENSE` contém a licença MIT aprovada pelo mantenedor para o código próprio. Dependências de terceiros conservam suas próprias licenças; a MIT não substitui seus avisos.

Os componentes visuais derivados de React Bits não fazem parte desta variante. O login usa a cena CSS original `OrbitalDawn`, já existente no projeto.

## Integrações de fabricante

As integrações são código do Astra; os documentos do fabricante não são redistribuídos. A privacidade de um manual, isoladamente, não exige remover a implementação própria de uma integração. Eventuais compromissos contratuais de confidencialidade continuam aplicáveis, mesmo sem redistribuição do manual. A seleção e a varredura técnica não dão uma conclusão jurídica sobre contratos externos.

Os campos de acesso da câmera são preenchidos pelo operador na sua instalação. As fixtures usam equipamentos simulados e credenciais fictícias, sem conexão com a infraestrutura do mantenedor.

## Reproduzir a preparação

Na pasta original de desenvolvimento, com Python 3.12 ou superior:

```powershell
python tools/prepare-github.py --destination output/github-review
```

O destino deve ser novo. A ferramenta usa a lista explícita `tools/public-source-files.txt`, exclui os componentes visuais derivados e aplica os ajustes documentados ao README, login e documentação. O manifesto SHA-256 registra os arquivos finais. A ferramenta não cria repositório remoto nem faz push.

Na cópia gerada, antes de instalar dependências ou criar builds:

```powershell
gitleaks dir . --config .gitleaks.toml --redact=100 --no-banner
```

Confira o manifesto, os links dos documentos e os arquivos que serão enviados. A varredura reduz riscos e não prova ausência absoluta de informação confidencial. Publique apenas os fontes aprovados, com histórico novo. Não envie o workspace original, pacotes de `dist/`, diretórios de runtime ou relatórios privados de inspeção.
