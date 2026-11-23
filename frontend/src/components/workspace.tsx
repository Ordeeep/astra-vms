"use client";
import { Button } from "@/components/ui/button";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { ThemeToggle } from "@/components/theme-toggle";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { AppSidebar } from "@/components/app-sidebar";
import { Account } from "@/components/account";
import { AstraBrand } from "@/components/astra-brand";
import { navigation } from "@/components/nav-main";
import { ArrowRight, Grid2X2 } from "lucide-react";
import { usePathname } from "next/navigation";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, setContext, setCSRF, type Identity, type Tenant } from "@/lib/api";
import {Administration} from "@/components/management";
import {SitesWorkspace} from "@/components/sites";
import {CamerasWorkspace} from "@/components/cameras";
import {LiveHeaderActions, LiveHeaderControls} from "@/components/live-header-controls";
import {LiveWorkspace} from "@/components/live";
import {LiveViewProvider} from "@/components/live-view-context";
import {StorageWorkspace} from "@/components/storage";
import {RecordingExportProvider} from "@/components/exports";
import {OperationsWorkspace} from "@/components/operations";
import {type Notification} from "@/lib/events";
import { workspacePageAt, workspacePaths, type WorkspacePage } from "@/lib/workspace-routes";

export default function Workspace() {
  const [identity, setIdentity] = useState<Identity | null>(null);
  const [tenant, setTenant] = useState<Tenant | null>(null);
  const [contextID, setContextID] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const pathname = usePathname();
  const page = workspacePageAt(pathname);
  const currentPath = useRef(pathname);
  const generation = useRef(0);
  const contextRef = useRef("");
  const navigationGuard=useRef<(()=>boolean)|null>(null);
  function navigate(nextPage: WorkspacePage) {
    const nextPath = workspacePaths[nextPage];
    if (nextPath === window.location.pathname) return;
    if (nextPage !== page && navigationGuard.current && !navigationGuard.current()) return;
    window.history.pushState(null, "", nextPath);
  }

  useEffect(() => {
    currentPath.current = pathname;
  }, [pathname]);

  useEffect(() => {
    function guardHistoryNavigation(event: PopStateEvent) {
      const previousPath = currentPath.current;
      if (!navigationGuard.current || workspacePageAt(window.location.pathname) === workspacePageAt(previousPath) || navigationGuard.current()) return;
      // A popstate arrives after the address changes. Restore the editor's URL before Next handles it.
      event.stopImmediatePropagation();
      window.history.pushState(null, "", previousPath);
    }
    window.addEventListener("popstate", guardHistoryNavigation, true);
    return () => window.removeEventListener("popstate", guardHistoryNavigation, true);
  }, []);

  async function selectTenant(next: Tenant) {
    if(navigationGuard.current&&!navigationGuard.current())return;
    const version = ++generation.current;
    const previous = contextRef.current;
    contextRef.current = "";
    setContext(""); setContextID(""); setTenant(null);
    if (previous) await api(`/contexts/${previous}`, "DELETE").catch(() => {});
    try {
      const created = await api<{id:string}>(`/contexts`, "POST", {tenantId: next.id});
      if (generation.current !== version) { await api(`/contexts/${created.id}`, "DELETE"); return; }
      contextRef.current = created.id; setContext(created.id); setContextID(created.id); setTenant(next);
      sessionStorage.setItem("vms-tenant", next.id); setError("");
    } catch (e) { if (generation.current === version) setError((e as Error).message); }
  }
  async function refreshIdentity() {
    const me = await api<Identity>("/me");
    setCSRF(me.csrfToken); setIdentity(me);
    const saved = sessionStorage.getItem("vms-tenant");
    const selected = me.tenants.find(t => t.id === saved && t.active) ?? me.tenants.find(t => t.active);
    if (selected) await selectTenant(selected);
    else { generation.current++;setContext("");contextRef.current="";setContextID("");setTenant(null); }
  }
  useEffect(() => {
    let active = true;
    api<Identity>("/me").then(async me => {
      if (!active) return;
      setCSRF(me.csrfToken); setIdentity(me);
      const selected = me.tenants.find(t => t.id === sessionStorage.getItem("vms-tenant") && t.active) ?? me.tenants.find(t => t.active);
      if (selected) await selectTenant(selected);
    }).catch(e => { if (active && (!(e instanceof ApiError) || e.status !== 401)) setError(e.message); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; generation.current++; };
  }, []);
  useEffect(()=>{
    if(!contextID||!tenant)return;
    let received=false,active=true;
    const source=new EventSource(`/api/v1/tenants/${tenant.id}/events?context=${encodeURIComponent(contextID)}`);
    source.onerror=()=>{
      if(active&&contextRef.current===contextID)window.dispatchEvent(new CustomEvent("vms:change",{detail:{type:"Disconnected",contextId:contextID,tenantId:tenant.id}}));
    };
    source.onmessage=message=>{
      if(!active||contextRef.current!==contextID)return;
      let event:Notification;
      try{event=JSON.parse(message.data) as Notification;}catch{return;}
      if(event.contextId!==contextID||event.tenantId!==tenant.id)return;
      if(event.type==="ContextClosed"){
        source.close();generation.current++;
        setContext("");contextRef.current="";setContextID("");setTenant(null);
        setError("O acesso a este ambiente foi encerrado. Atualize seus acessos para continuar.");
        return;
      }
      // Refresh authorization without remounting unrelated live players.
      if(event.type==="AuthorizationChanged"||(received&&["Connected","ResyncRequired"].includes(event.type))){
        void api<Identity>("/me").then(me=>{if(active&&contextRef.current===contextID){setIdentity(me);setCSRF(me.csrfToken);}}).catch(()=>{});
      }
      received=true;
      window.dispatchEvent(new CustomEvent<Notification>("vms:change",{detail:event}));
    };
    return()=>{active=false;source.close();};
  },[contextID,tenant?.id]);
  async function logout() {
    if(navigationGuard.current&&!navigationGuard.current())return;
    try {
      await api("/auth/logout", "POST");
      generation.current++; setContext(""); contextRef.current = ""; setContextID(""); setCSRF("");
      setIdentity(null); setTenant(null); setError("");
      window.history.replaceState(null, "", "/");
    } catch(e) { setError((e as Error).message); }
  }
  if (loading) return <main className="loading"><AstraBrand/><Spinner className="size-6" aria-label="Conectando à instalação"/><p>Conectando à sua instalação…</p></main>;
  if (!identity) return <Login error={error} onSuccess={refreshIdentity}/>;
  const showLiveControls = page === "live" && !!tenant;
  return <SidebarProvider className="workspace">
    <LiveViewProvider key={contextID||"no-context"} tenantId={tenant?.id} enabled={page==="live"&&!!contextID}>
    <RecordingExportProvider tenantId={tenant?.id}>
    <AppSidebar collapsible="icon" identity={identity} tenant={tenant} activePage={page}
      onNavigate={navigate} onTenantSelect={next=>void selectTenant(next)} onLogout={()=>void logout()} />
    <SidebarInset className="min-w-0">
      <header className="workspace-header" data-live={showLiveControls || undefined}>
        <div className="workspace-path">
          <SidebarTrigger />
          <Separator orientation="vertical" className="data-vertical:h-4 data-vertical:self-auto" />
          <Breadcrumb><BreadcrumbList>
            <BreadcrumbItem className="hidden md:block">Astra</BreadcrumbItem>
            <BreadcrumbSeparator className="hidden md:block" />
            <BreadcrumbItem><BreadcrumbPage>{navigation.find(item=>item.id===page)?.label}</BreadcrumbPage></BreadcrumbItem>
          </BreadcrumbList></Breadcrumb>
        </div>
        {showLiveControls&&<LiveHeaderControls/>}
        <div className="workspace-actions">
          {showLiveControls ? <LiveHeaderActions/> : <ThemeToggle/>}
        </div>
      </header>
      <div className={`content${page==="live"&&tenant?" live-content":""}`} key={contextID||"no-context"}>
        {error && <Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {!contextID&&<Button onClick={()=>void refreshIdentity().catch(e=>{if(e instanceof ApiError&&e.status===401)setIdentity(null);else setError((e as Error).message);})} variant="outline">Atualizar meus acessos</Button>}
        {page==="operations"&&tenant?<OperationsWorkspace tenant={tenant} identity={identity}/>:page==="account" ? <Account user={identity.user} onUpdate={user=>setIdentity(current=>current ? {...current,user} : current)}/> :page==="storage"?<StorageWorkspace tenant={tenant} identity={identity}/>:page==="live"&&tenant?<LiveWorkspace tenant={tenant} globalAdmin={identity.user.globalAdmin}/>: page==="sites"&&tenant?<SitesWorkspace tenant={tenant}/>:page==="cameras"&&tenant?<CamerasWorkspace tenant={tenant} registerCloseGuard={guard=>{navigationGuard.current=guard;}} onStorage={()=>{navigationGuard.current=null;navigate("storage");}}/>:(page==="operators"||page==="groups"||page==="tenants")?<Administration key={page} section={page} identity={identity} tenant={tenant} onRefresh={refreshIdentity}/>:<>
          <div className="page-heading"><div><h1>Visão geral</h1><p>Seu ambiente de monitoramento, em um só lugar.</p></div><Badge variant="outline"><span className="status-dot" aria-hidden="true"/>Conectado</Badge></div>
          <Card asChild className="panel welcome gap-0 p-6"><section><div className="welcome-icon"><Grid2X2 className="size-5" aria-hidden="true"/></div><h2>{tenant ? `Bem-vindo a ${tenant.name}` : "Acesso à instalação"}</h2><p>{tenant ? "Este ambiente mantém seus sites, câmeras e gravações separados dos demais." : "Sua conta ainda não possui um ambiente ativo disponível."}</p></section></Card>
        </>}
      </div>
    </SidebarInset>
    </RecordingExportProvider>
    </LiveViewProvider>
  </SidebarProvider>;
}
function Login({error:initialError,onSuccess}:{error:string;onSuccess:()=>Promise<void>}) {
  const [error,setError]=useState(initialError),[busy,setBusy]=useState(false);
  async function submit(event:FormEvent<HTMLFormElement>) {
    event.preventDefault();setBusy(true);setError("");
    const values=new FormData(event.currentTarget);
    try {
      const result=await api<{csrfToken:string}>("/auth/login","POST",{username:values.get("username"),password:values.get("password")});
      setCSRF(result.csrfToken);await onSuccess();
    } catch(e) {setError((e as Error).message);} finally {setBusy(false);}
  }
  return <main className="login-page">
    <header className="login-header"><AstraBrand/><ThemeToggle /></header>
    <Card asChild className="login-panel">
      <section aria-labelledby="login-title"><form onSubmit={submit} aria-busy={busy}>
      <h1 id="login-title">Entrar no Astra</h1><p>Use sua conta da instalação para acessar a central de vídeo.</p>
      {error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      <Label className="field-label">Usuário ou e-mail<Input className="h-11" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} maxLength={254} required autoFocus placeholder="Seu usuário ou e-mail"/></Label>
      <Label className="field-label">Senha<Input className="h-11" name="password" type="password" autoComplete="current-password" required placeholder="Sua senha"/></Label>
      <Button className="full" disabled={busy} variant="default">{busy?"Entrando…":"Entrar"}{busy?<Spinner aria-hidden="true"/>:<ArrowRight aria-hidden="true"/>}</Button>
    </form></section></Card>
  </main>;
}
