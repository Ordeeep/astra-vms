"use client";

import type { ComponentProps } from "react";
import { NavMain } from "@/components/nav-main";
import { NavUser } from "@/components/nav-user";
import { TeamSwitcher } from "@/components/team-switcher";
import {
  Sidebar, SidebarContent, SidebarFooter, SidebarHeader, useSidebar,
} from "@/components/ui/sidebar";
import type { Identity, Tenant } from "@/lib/api";

type AppSidebarProps = ComponentProps<typeof Sidebar> & {
  identity: Identity;
  tenant: Tenant | null;
  activePage: string;
  onNavigate: (page: string) => void;
  onTenantSelect: (tenant: Tenant) => void;
  onLogout: () => void;
};

// Adapted from the official sidebar-07 block to the Astra identity and context.
export function AppSidebar({
  identity, tenant, activePage, onNavigate, onTenantSelect, onLogout, ...props
}: AppSidebarProps) {
  const { setOpenMobile } = useSidebar();
  function navigate(page: string) {
    onNavigate(page);
    setOpenMobile(false);
  }

  return (
    <Sidebar {...props}>
      <SidebarHeader>
        <TeamSwitcher
          tenants={identity.tenants.filter(item => item.active)}
          activeTenant={tenant}
          onSelect={item => { onTenantSelect(item); setOpenMobile(false); }}
          onHome={() => navigate("overview")}
        />
      </SidebarHeader>
      <SidebarContent>
        <NavMain activePage={activePage} onNavigate={navigate} globalAdmin={identity.user.globalAdmin} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={identity.user} onAccount={() => navigate("account")} onLogout={onLogout} />
      </SidebarFooter>
    </Sidebar>
  );
}
