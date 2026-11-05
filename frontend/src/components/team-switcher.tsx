"use client";

import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem, useSidebar } from "@/components/ui/sidebar";
import { Building2, Check, ChevronsUpDown } from "lucide-react";
import { AstraMark, AstraWordmark } from "@/components/astra-brand";
import type { Tenant } from "@/lib/api";

export function TeamSwitcher({ tenants, activeTenant, onSelect, onHome }: {
  tenants: Tenant[];
  activeTenant: Tenant | null;
  onSelect: (tenant: Tenant) => void;
  onHome: () => void;
}) {
  const { isMobile, state } = useSidebar();
  const label = activeTenant?.name ?? "Sem ambiente ativo";
  const content = <>
    <div className="flex aspect-square size-8 shrink-0 items-center justify-center"><AstraMark className="size-7" /></div>
    <div className="grid flex-1 text-left text-sm leading-tight group-data-[collapsible=icon]:hidden">
      <span className="font-medium"><span className="sr-only">Astra</span><AstraWordmark className="h-3 w-auto max-w-24" /></span>
      <span className="truncate text-xs text-muted-foreground">{label}</span>
    </div>
  </>;
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        {tenants.length > 1 ? <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton size="lg" aria-label={`Astra · Ambiente ativo: ${label}`} className="astra-sidebar-brand data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground">
              {content}<ChevronsUpDown className="ml-auto group-data-[collapsible=icon]:hidden" aria-hidden="true" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="min-w-56" align="start" side={isMobile ? "bottom" : "right"} sideOffset={4}>
            <DropdownMenuLabel>Ambientes autorizados</DropdownMenuLabel>
            {tenants.map(tenant => <DropdownMenuItem key={tenant.id} onSelect={() => { if (tenant.id !== activeTenant?.id) onSelect(tenant); }}>
              <Building2 aria-hidden="true" /><span className="flex-1">{tenant.name}</span>{activeTenant?.id === tenant.id && <Check aria-label="Ativo" />}
            </DropdownMenuItem>)}
          </DropdownMenuContent>
        </DropdownMenu> : <SidebarMenuButton size="lg" className="astra-sidebar-brand" tooltip={state === "collapsed" && !isMobile ? `Astra · ${label}` : undefined} aria-label={`Astra: ${label}`} onClick={onHome}>{content}</SidebarMenuButton>}
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
