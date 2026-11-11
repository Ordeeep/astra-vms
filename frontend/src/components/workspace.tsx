"use client";
import { Button } from "@/components/ui/button";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import LoginBackground from "@/components/login-background";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { AppSidebar } from "@/components/app-sidebar";
import { Account } from "@/components/account";
import { AstraBrand } from "@/components/astra-brand";
import { navigation } from "@/components/nav-main";
import { ArrowRight, Grid2X2 } from "lucide-react";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, setContext, setCSRF, type Identity, type Tenant } from "@/lib/api";
import {Administration} from "@/components/management";
import {SitesWorkspace} from "@/components/sites";
import {CamerasWorkspace} from "@/components/cameras";
import {LiveHeaderControls} from "@/components/live-header-controls";
import {LiveWorkspace} from "@/components/live";
import {LiveViewProvider} from "@/components/live-view-context";
import {RecordingWorkspace} from "@/components/recording";
import {PlaybackWorkspace} from "@/components/playback";
import {ExportsWorkspace} from "@/components/exports";
import {OperationsWorkspace} from "@/components/operations";
import {type Notification} from "@/lib/events";

export default function Workspace() {
  const [identity, setIdentity] = useState<Identity | null>(null);
  const [tenant, setTenant] = useState<Tenant | null>(null);
  const [contextID, setContextID] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [page, setPage] = useState("overview");
  const generation = useRef(0);
  const contextRef = useRef("");

  async function selectTenant(next: Tenant) {
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
    try {
      await api("/auth/logout", "POST");
      generation.current++; setContext(""); contextRef.current = ""; setContextID(""); setCSRF("");
      setIdentity(null); setTenant(null); setError(""); setPage("overview");
    } catch(e) { setError((e as Error).message); }
  }
  if (loading) return <main className="loading"><AstraBrand/><Spinner className="size-6" aria-label="Conectando à instalação"/><p>Conectando à sua instalação…</p></main>;
  if (!identity) return <Login error={error} onSuccess={refreshIdentity}/>;
  return <SidebarProvider className="workspace" open={true}>
    <LiveViewProvider key={contextID||"no-context"} tenantId={tenant?.id} enabled={page==="live"&&!!contextID}>
    <AppSidebar identity={identity} tenant={tenant} activePage={page}
      onNavigate={setPage} onTenantSelect={next=>void selectTenant(next)} onLogout={()=>void logout()} />
    <SidebarInset className="min-w-0">
      <header className="flex h-10 shrink-0 items-center gap-2 border-b">
        <div className="flex min-w-0 items-center gap-2 px-4">
          <SidebarTrigger className="-ml-1 md:hidden" aria-label="Alternar menu lateral" />
          <Separator orientation="vertical" className="mr-2 md:hidden data-vertical:h-4 data-vertical:self-auto" />
          <Breadcrumb><BreadcrumbList>
            <BreadcrumbItem className="hidden md:block">Astra</BreadcrumbItem>
            <BreadcrumbSeparator className="hidden md:block" />
            <BreadcrumbItem><BreadcrumbPage>{navigation.find(item=>item.id===page)?.label}</BreadcrumbPage></BreadcrumbItem>
          </BreadcrumbList></Breadcrumb>
        </div>
        {page==="live"&&tenant&&<LiveHeaderControls/>}
      </header>
      <div className={`content${page==="live"&&tenant?" live-content":""}`} key={contextID||"no-context"}>
        {error && <Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {!contextID&&<Button onClick={()=>void refreshIdentity().catch(e=>{if(e instanceof ApiError&&e.status===401)setIdentity(null);else setError((e as Error).message);})} variant="outline">Atualizar meus acessos</Button>}
        {page==="operations"&&tenant?<OperationsWorkspace tenant={tenant} identity={identity}/>:page==="account" ? <Account user={identity.user} onUpdate={user=>setIdentity(current=>current ? {...current,user} : current)}/> :page==="exports"&&tenant?<ExportsWorkspace tenant={tenant}/>:page==="recording"?<RecordingWorkspace tenant={tenant} identity={identity}/>:page==="playback"&&tenant?<PlaybackWorkspace tenant={tenant}/>:page==="live"&&tenant?<LiveWorkspace tenant={tenant} globalAdmin={identity.user.globalAdmin}/>: page==="sites"&&tenant?<SitesWorkspace tenant={tenant}/>:page==="cameras"&&tenant?<CamerasWorkspace tenant={tenant}/>:(page==="admin"||page==="operators"||page==="groups"||page==="tenants")?<Administration key={page} section={page==="admin"?"access":page} identity={identity} tenant={tenant} onRefresh={refreshIdentity}/>:<>
          <div className="page-heading"><div><span className="eyebrow">CENTRAL DE OPERAÇÃO</span><h1>Visão geral</h1><p>Seu ambiente de monitoramento, em um só lugar.</p></div><Badge className="badge" variant="outline"><span className="status-dot"/>Conectado</Badge></div>
          <Card className="panel welcome gap-0 p-6"><div className="welcome-icon"><Grid2X2 className="size-8" aria-hidden="true"/></div><h2>{tenant ? `Bem-vindo a ${tenant.name}` : "Acesso à instalação"}</h2><p>{tenant ? "Este ambiente mantém seus sites, câmeras e gravações separados dos demais." : "Sua conta ainda não possui um ambiente ativo disponível."}</p></Card>
        </>}
      </div>
    </SidebarInset>
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
    <div className="login-background" aria-hidden="true">
      <LoginBackground/>
    </div>
    <div className="login-story"><AstraBrand/><div><span className="eyebrow">MONITORAMENTO E GRAVAÇÕES</span><h1>Monitore ao vivo.<br/><em>Consulte gravações.</em></h1><p>Acompanhe as câmeras dos seus locais e encontre gravações por câmera, data e horário.</p></div><small>Astra · Gestão de vídeo na sua infraestrutura</small></div>
    <section className="login-panel"><form onSubmit={submit}><span className="eyebrow">ACESSO À CENTRAL</span><h2>Bem-vindo de volta</h2><p>Entre com sua conta da instalação.</p>
      {error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      <Label className="field-label">Usuário ou e-mail<Input className="h-11" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} maxLength={254} required autoFocus placeholder="Seu usuário ou e-mail"/></Label>
      <Label className="field-label">Senha<Input className="h-11" name="password" type="password" autoComplete="current-password" required placeholder="Sua senha"/></Label>
      <Button className="full" disabled={busy} variant="default">{busy?"Entrando…":"Entrar"}{busy?<Spinner aria-hidden="true"/>:<ArrowRight aria-hidden="true"/>}</Button>
    </form></section>
  </main>;
}
