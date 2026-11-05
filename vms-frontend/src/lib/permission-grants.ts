export type Grant = {action: string; scope: string; resourceId: string};
export type GrantTarget = Pick<Grant, "scope" | "resourceId">;

export const permissionActions = [
  {id: "live", label: "Assistir ao vivo", description: "Visualizar as imagens das câmeras em tempo real.", section: "operation"},
  {id: "ptz.control", label: "Controlar PTZ", description: "Movimentar a câmera e controlar seu zoom óptico. Requer acesso ao vivo às mesmas câmeras.", section: "operation"},
  {id: "playback", label: "Consultar gravações", description: "Buscar e reproduzir gravações das câmeras.", section: "operation"},
  {id: "export", label: "Exportar gravações", description: "Exportar trechos de vídeo. Requer consulta às gravações das mesmas câmeras.", section: "operation"},
  {id: "inventory.manage", label: "Gerenciar sites e câmeras", description: "Editar os cadastros e as conexões das câmeras.", section: "administration"},
  {id: "camera.delete", label: "Excluir câmeras", description: "Excluir câmeras e todas as suas gravações no tenant.", section: "administration"},
  {id: "permissions.manage", label: "Administrar permissões", description: "Gerenciar grupos e acessos dos operadores.", section: "administration", tenantOnly: true},
  {id: "mosaics.manage", label: "Administrar mosaicos", description: "Gerenciar os mosaicos compartilhados do tenant.", section: "administration", tenantOnly: true},
  {id: "recording.manage", label: "Configurar gravação", description: "Ajustar as configurações de gravação das câmeras.", section: "administration"},
  {id: "audit.view", label: "Consultar auditoria", description: "Consultar os registros de atividade do tenant.", section: "administration", tenantOnly: true},
] as const;

export function sameTarget(grant: GrantTarget, target: GrantTarget) {
  return grant.scope === target.scope && grant.resourceId === target.resourceId;
}

/** Keep every unrelated action/scope pair, including grants outside the loaded catalog. */
export function setTargetAction(grants: Grant[], target: GrantTarget, action: string, enabled: boolean): Grant[] {
  const matches = (grant: Grant) => sameTarget(grant, target) && grant.action === action;
  if (!enabled) return grants.filter(grant => !matches(grant));
  return grants.some(matches) ? grants : [...grants, {...target, action}];
}

/** Parent grants only cover the same action; never combine actions with other scopes. */
export function targetActionState(grants: Grant[], target: GrantTarget, action: string, tenantId: string, siteId?: string) {
  const direct = grants.some(grant => sameTarget(grant, target) && grant.action === action);
  const parents = grants.filter(grant => grant.action === action && !sameTarget(grant, target) && (
    (target.scope !== "tenant" && grant.scope === "tenant" && grant.resourceId === tenantId) ||
    (target.scope === "camera" && !!siteId && grant.scope === "site" && grant.resourceId === siteId)
  ));
  return {direct, parents, checked: direct || parents.length > 0};
}
