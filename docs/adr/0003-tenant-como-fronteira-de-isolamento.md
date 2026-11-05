# Tenant como fronteira de isolamento

Cada tenant representa uma empresa e delimita seus sites, câmeras e gravações, enquanto operadores podem receber acesso explícito a mais de um tenant. A separação deve ser aplicada também pela API, e não apenas pela interface; isso exige preservar o contexto de autorização de cada recurso e impede tratar o tenant como um simples filtro visual.

Os tenants compartilharão o mesmo servidor e ambiente, com isolamento lógico imposto pelo código em cada acesso. A visualização sempre pertence ao tenant ativo: mesmo um operador autorizado em duas empresas precisa trocar de tenant para visualizar as câmeras da outra, sem mistura entre os ambientes.

Grupos de permissões e suas configurações pertencem a um tenant; suas concessões se somam apenas dentro dele, com acesso negado na ausência de concessão e sem negações explícitas na primeira versão. Como a maioria das instalações atenderá uma única empresa, um ambiente padrão será criado automaticamente e o seletor ficará oculto para quem tiver acesso a apenas um tenant; internamente, o modelo de isolamento será o mesmo, permitindo adicionar empresas sem migrar os cadastros existentes.

Na consolidação (Q39), foi confirmado que administrar globalmente a instalação/tenants não concede acesso implícito aos vídeos. A conta inicial recebe concessões explícitas no ambiente padrão, e acesso a outras empresas exige concessões próprias. O modelo de dados e o detalhamento das operações de gestão global permanecem em aberto.

Na modelagem de domínio, D4 esclareceu que o contexto ativo é por aba: um operador autorizado pode abrir tenants diferentes em abas distintas, sempre sem misturar seus ambientes. A troca encerra as transmissões daquele contexto; revogar acesso ao tenant afeta todas as abas dele. D3 definiu a soma como união de concessões completas de ação e escopo, sem combinações cruzadas. D15 permitiu ao administrador global conceder acesso explicitamente a si, com auditoria; gestores locais ficam limitados aos poderes que possuem. Essas decisões detalham o isolamento lógico, sem instituir separação absoluta de poderes administrativos.
