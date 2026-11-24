"use client";

import { useEffect, useState, type MouseEvent } from "react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  SidebarGroup, SidebarMenu, SidebarMenuButton, SidebarMenuItem,
  SidebarMenuSub, SidebarMenuSubButton, SidebarMenuSubItem, useSidebar,
} from "@/components/ui/sidebar";
import {
  Building2, HardDrive, ChevronRight, CircleUserRound, ClipboardList, ListTree,
  LayoutDashboard, MapPin, MonitorPlay, Settings2, ShieldCheck, UserRound, UsersRound, Video,
  type LucideIcon,
} from "lucide-react";
import { workspacePaths, type WorkspacePage } from "@/lib/workspace-routes";

type NavigationSection = {
  id: string;
  label: string;
  icon: LucideIcon;
  items: { id: WorkspacePage; label: string; icon: LucideIcon }[];
};

const navigationGroups: NavigationSection[] = [
  {
    id: "operation", label: "Operação", icon: MonitorPlay,
    items: [
      { id: "overview", label: "Visão geral", icon: LayoutDashboard },
      { id: "live", label: "Ao vivo", icon: MonitorPlay },
      { id: "events", label: "Eventos", icon: ListTree },
    ],
  },
  {
    id: "cameras", label: "Câmeras e gravação", icon: Video,
    items: [
      { id: "sites", label: "Sites", icon: MapPin },
      { id: "cameras", label: "Câmeras", icon: Video },
    ],
  },
  {
    id: "permissions", label: "Usuários e permissões", icon: UsersRound,
    items: [
      { id: "operators", label: "Operadores", icon: UserRound },
      { id: "groups", label: "Grupos de permissões", icon: ShieldCheck },
    ],
  },
  {
    id: "system", label: "Sistema", icon: Settings2,
    items: [
      { id: "tenants", label: "Ambientes", icon: Building2 },
      { id: "storage", label: "Armazenamento", icon: HardDrive },
      { id: "operations", label: "Auditoria e diagnóstico", icon: ClipboardList },
    ],
  },
];

// Include account metadata for the breadcrumb; its entry lives only in NavUser.
export const navigation = [
  ...navigationGroups.flatMap(group => group.items),
  { id: "account", label: "Minha conta", icon: CircleUserRound },
];

type NavigationProps = { activePage: WorkspacePage; onNavigate: (page: WorkspacePage) => void };

function NavigationGroup({ group, activePage, onNavigate }: NavigationProps & {
  group: NavigationSection;
}) {
  const Icon = group.icon;
  const [open, setOpen] = useState(true);
  const { isMobile, state } = useSidebar();
  const active = group.items.some(item => item.id === activePage);

  useEffect(() => {
    if (active) setOpen(true);
  }, [activePage, active]);

  function followLink(event: MouseEvent<HTMLAnchorElement>, page: WorkspacePage) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onNavigate(page);
  }

  if (!isMobile && state === "collapsed") return group.items.map(({id, label, icon: ItemIcon}) => (
    <SidebarMenuItem key={id}>
      <SidebarMenuButton asChild tooltip={label} isActive={activePage === id}>
        <a href={workspacePaths[id]} aria-label={label} aria-current={activePage === id ? "page" : undefined} onClick={event => followLink(event, id)}>
          <ItemIcon aria-hidden="true" />
        </a>
      </SidebarMenuButton>
    </SidebarMenuItem>
  ));

  return (
    <Collapsible asChild open={open} onOpenChange={setOpen}>
    <SidebarMenuItem>
      <CollapsibleTrigger asChild>
        <SidebarMenuButton id={`navigation-${group.id}`} className="text-sm font-medium" type="button" aria-label={group.label}>
          <Icon aria-hidden="true" />
          <span className="truncate">{group.label}</span>
          <ChevronRight aria-hidden="true" className={`ml-auto transition-transform motion-reduce:transition-none${open ? " rotate-90" : ""}`} />
        </SidebarMenuButton>
      </CollapsibleTrigger>
      <CollapsibleContent asChild>
      <SidebarMenuSub aria-labelledby={`navigation-${group.id}`}>
        {group.items.map(({ id, label, icon: ItemIcon }) => (
          <SidebarMenuSubItem key={id}>
            <SidebarMenuSubButton asChild isActive={activePage === id} className="w-full">
              <a href={workspacePaths[id]} aria-current={activePage === id ? "page" : undefined} onClick={event => followLink(event, id)}>
                <ItemIcon aria-hidden="true" />
                <span>{label}</span>
              </a>
            </SidebarMenuSubButton>
          </SidebarMenuSubItem>
        ))}
      </SidebarMenuSub>
      </CollapsibleContent>
    </SidebarMenuItem>
    </Collapsible>
  );
}

export function NavMain({ activePage, onNavigate, globalAdmin, canViewEvents }: NavigationProps & { globalAdmin: boolean; canViewEvents: boolean }) {
  return (
    <SidebarGroup>
      <nav aria-label="Navegação principal">
        <SidebarMenu>
          {navigationGroups.map(group => <NavigationGroup key={group.id} group={{...group,items:group.items.filter(item=>(item.id!=="tenants"||globalAdmin)&&(item.id!=="events"||canViewEvents))}} activePage={activePage} onNavigate={onNavigate} />)}
        </SidebarMenu>
      </nav>
    </SidebarGroup>
  );
}
