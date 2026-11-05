"use client";

import { useEffect, useState } from "react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuButton, SidebarMenuItem,
  SidebarMenuSub, SidebarMenuSubButton, SidebarMenuSubItem,
} from "@/components/ui/sidebar";
import {
  Building2, CalendarClock, ChevronRight, CircleUserRound, ClipboardList, Download, History,
  LayoutDashboard, MapPin, MonitorPlay, Settings2, ShieldCheck, UserRound, UsersRound, Video,
} from "lucide-react";

const navigationGroups = [
  {
    id: "operation", label: "Operação", icon: MonitorPlay,
    items: [
      { id: "overview", label: "Visão geral", icon: LayoutDashboard },
      { id: "live", label: "Ao vivo", icon: MonitorPlay },
      { id: "playback", label: "Consultar gravações", icon: History },
      { id: "exports", label: "Exportações", icon: Download },
    ],
  },
  {
    id: "cameras", label: "Câmeras e gravação", icon: Video,
    items: [
      { id: "sites", label: "Sites", icon: MapPin },
      { id: "cameras", label: "Câmeras", icon: Video },
      { id: "recording", label: "Gravação e agenda", icon: CalendarClock },
    ],
  },
  {
    id: "permissions", label: "Usuários e permissões", icon: UsersRound,
    items: [
      { id: "operators", label: "Operadores", icon: UserRound },
      { id: "groups", label: "Grupos", icon: ShieldCheck },
      { id: "admin", label: "Acessos", icon: UsersRound },
    ],
  },
  {
    id: "system", label: "Sistema", icon: Settings2,
    items: [
      { id: "tenants", label: "Ambientes", icon: Building2 },
      { id: "operations", label: "Auditoria e diagnóstico", icon: ClipboardList },
    ],
  },
];

// Include account metadata for the breadcrumb; its entry lives only in NavUser.
export const navigation = [
  ...navigationGroups.flatMap(group => group.items),
  { id: "account", label: "Minha conta", icon: CircleUserRound },
];

type NavigationProps = { activePage: string; onNavigate: (page: string) => void };

function NavigationGroup({ group, activePage, onNavigate }: NavigationProps & {
  group: typeof navigationGroups[number];
}) {
  const Icon = group.icon;
  const [open, setOpen] = useState(true);
  const active = group.items.some(item => item.id === activePage);

  useEffect(() => {
    if (active) setOpen(true);
  }, [activePage, active]);

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
              <button type="button" aria-label={label} aria-current={activePage === id ? "page" : undefined} onClick={() => onNavigate(id)}>
                <ItemIcon aria-hidden="true" />
                <span>{label}</span>
              </button>
            </SidebarMenuSubButton>
          </SidebarMenuSubItem>
        ))}
      </SidebarMenuSub>
      </CollapsibleContent>
    </SidebarMenuItem>
    </Collapsible>
  );
}

export function NavMain({ activePage, onNavigate, globalAdmin }: {
  activePage: string;
  onNavigate: (page: string) => void;
  globalAdmin: boolean;
}) {
  return (
    <SidebarGroup>
      <SidebarGroupLabel>Central de vídeo</SidebarGroupLabel>
      <nav aria-label="Navegação principal">
        <SidebarMenu>
          {navigationGroups.map(group => <NavigationGroup key={group.id} group={{...group,items:group.items.filter(item=>!["operators","tenants"].includes(item.id)||globalAdmin)}} activePage={activePage} onNavigate={onNavigate} />)}
        </SidebarMenu>
      </nav>
    </SidebarGroup>
  );
}
