export const workspacePaths = {
  overview: "/visao-geral",
  live: "/ao-vivo",
  events: "/eventos",
  sites: "/sites",
  cameras: "/cameras",
  operators: "/operadores",
  groups: "/grupos-de-permissoes",
  tenants: "/ambientes",
  storage: "/armazenamento",
  operations: "/auditoria-e-diagnostico",
  account: "/minha-conta",
} as const;

export type WorkspacePage = keyof typeof workspacePaths;

export function workspacePageAt(pathname: string): WorkspacePage {
  const path = pathname.replace(/\/$/, "");
  return (Object.keys(workspacePaths) as WorkspacePage[]).find(page => workspacePaths[page] === path) ?? "overview";
}
