"use client";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Card } from "@/components/ui/card";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import {useEffect,useRef,useState} from "react";
import {api,type Identity,type Tenant} from "@/lib/api";
import {onEnvironmentChange} from "@/lib/events";
type Audit={id:string;actor:string;action:string;resourceId:string;occurredAt:string;revision:number|null;outcome:string;reason:string};
type Event={id:string;name:string;code:string;occurredAt:string};
type Status={cameraId:string;name:string;code:string;lastFrame:string|null;lastWrite:string|null;updatedAt:string|null};
type Diagnostic={database:string;workerAvailable:boolean;storageConfigured:boolean;usage:Record<string,number>;cameraCount:number;activeCameras:number;observedAt:string;items:Status[];nextCursor:string};
export const stateNames:Record<string,string>={recording:"Gravando",connecting:"Conectando",outside_schedule:"Fora da agenda",recording_disabled:"Gravação desativada",camera_inactive:"Câmera, site ou captura pausados",not_configured:"Não configurada",profile_unavailable:"Perfil indisponível",capture_unavailable:"Captura indisponível",source_unavailable:"Sem imagens da fonte",storage_capacity:"Sem capacidade",storage_unavailable:"Armazenamento indisponível",worker_unavailable:"Serviço de mídia indisponível",authority_expired:"Comunicação com o controle interrompida"};
const actionNames:Record<string,string>={"ptz.command":"Comando PTZ enviado","operator.login":"Entrada na instalação","operator.logout":"Saída da instalação","operator.password_changed":"Senha alterada","live.started":"Visualização ao vivo iniciada","playback.started":"Consulta de gravação iniciada","export.requested":"Exportação solicitada","export.finished":"Exportação finalizada","export.cancelled":"Exportação cancelada","recording.configured":"Agenda configurada","recording.policy_changed":"Política de gravação alterada","quota.reduction_requested":"Redução de cota solicitada","quota.reduction_applied":"Cota reduzida","storage.configured":"Armazenamento configurado"};
export function OperationsWorkspace({tenant,identity}:{tenant:Tenant;identity:Identity}){
 return <OperationsContent key={tenant.id} tenant={tenant} identity={identity}/>;
}
const outcomeNames:Record<string,string>={succeeded:"Concluído",completed:"Concluído",failed:"Falhou",cancelled:"Cancelado",queued:"Na fila",running:"Em andamento",expired:"Expirado"};
const dateCell=(value:string|null)=>value?new Date(value).toLocaleString():"—";
function OperationsContent({tenant,identity}:{tenant:Tenant;identity:Identity}){
 const expanded=useRef(false),auditRequest=useRef(0),eventRequest=useRef(0),auditPending=useRef(false),eventPending=useRef(false);
 const base=`/tenants/${tenant.id}`;
 const [tab,setTab]=useState("diagnostic"),[diagnostic,setDiagnostic]=useState<Diagnostic|null>(null),[events,setEvents]=useState<Event[]>([]),[audit,setAudit]=useState<Audit[]>([]),[cursor,setCursor]=useState(""),[eventCursor,setEventCursor]=useState(""),[error,setError]=useState(""),[busy,setBusy]=useState(false),[global,setGlobal]=useState(false),[action,setAction]=useState(""),[appliedAction,setAppliedAction]=useState("");
 async function loadAudit(append=false){
  if(auditPending.current)return;auditPending.current=true;
  const request=++auditRequest.current;setBusy(true);setError("");
  try{const path=global?"/admin/audit":base+"/audit";const value=await api<{items:Audit[];nextCursor:string}>(path+`?action=${encodeURIComponent(appliedAction)}&cursor=${append?encodeURIComponent(cursor):""}`,"GET",undefined,!global);if(request!==auditRequest.current)return;setAudit(items=>append?Array.from(new Map([...items,...value.items].map(item=>[item.id,item])).values()):value.items);setCursor(value.nextCursor);}
  catch(e){if(request===auditRequest.current)setError((e as Error).message);}
  finally{if(request===auditRequest.current){auditPending.current=false;setBusy(false);}}
 }
 useEffect(()=>{
  expanded.current=false;
  if(tab==="audit"){void loadAudit();return()=>{auditRequest.current++;auditPending.current=false;};}
  let active=true,loading=false;
  const poll=async()=>{if(loading)return;loading=true;try{
   let d=await api<Diagnostic>(base+"/diagnostics","GET",undefined,true);const items=[...d.items];
   while(d.nextCursor&&active){d=await api<Diagnostic>(base+`/diagnostics?cursor=${encodeURIComponent(d.nextCursor)}`,"GET",undefined,true);items.push(...d.items);}
   const e=await api<{items:Event[];nextCursor:string}>(base+"/recording-events","GET",undefined,true);
   if(active){setDiagnostic({...d,items});if(!expanded.current){setEvents(e.items);setEventCursor(e.nextCursor);}setError("");}
  }catch(e){if(active)setError((e as Error).message);}finally{loading=false;}};
  void poll();const timer=setInterval(()=>void poll(),1000);const unsubscribe=onEnvironmentChange(["CaptureStateChanged","CameraStatusChanged","StoragePressure"],()=>void poll());
  return()=>{active=false;eventRequest.current++;eventPending.current=false;clearInterval(timer);unsubscribe();};
 },[tenant.id,tab,global,appliedAction]);
 async function olderEvents(){
  if(eventPending.current)return;eventPending.current=true;expanded.current=true;setBusy(true);const request=++eventRequest.current;
  try{const e=await api<{items:Event[];nextCursor:string}>(base+`/recording-events?cursor=${encodeURIComponent(eventCursor)}`,"GET",undefined,true);if(request!==eventRequest.current)return;setEvents(items=>Array.from(new Map([...items,...e.items].map(item=>[item.id,item])).values()));setEventCursor(e.nextCursor);}
  catch(e){if(request===eventRequest.current)setError((e as Error).message);}
  finally{if(request===eventRequest.current){eventPending.current=false;setBusy(false);}}
 }
 const statusColumns:DataTableColumn<Status>[]=[
  {id:"name",header:"Câmera",accessor:c=>c.name,hideable:false},
  {id:"state",header:"Estado",accessor:c=>stateNames[c.code]??c.code,filter:"select"},
  {id:"lastFrame",header:"Último quadro",accessor:c=>c.lastFrame,filter:"date",cell:c=>dateCell(c.lastFrame)},
  {id:"lastWrite",header:"Última escrita",accessor:c=>c.lastWrite,filter:"date",cell:c=>dateCell(c.lastWrite)},
 ];
 const eventColumns:DataTableColumn<Event>[]=[
  {id:"when",header:"Quando",accessor:e=>e.occurredAt,filter:"date",cell:e=>dateCell(e.occurredAt)},
  {id:"camera",header:"Câmera",accessor:e=>e.name,hideable:false},
  {id:"state",header:"Mudança observada",accessor:e=>stateNames[e.code]??e.code,filter:"select"},
 ];
 const auditColumns:DataTableColumn<Audit>[]=[
  {id:"when",header:"Quando",accessor:e=>e.occurredAt,filter:"date",cell:e=>dateCell(e.occurredAt)},
  {id:"actor",header:"Operador",accessor:e=>e.actor,hideable:false},
  {id:"action",header:"Ação",accessor:e=>actionNames[e.action]??e.action,filter:"select"},
  {id:"outcome",header:"Resultado",accessor:e=>outcomeNames[e.outcome]??"Registrado",filter:"select",cell:e=><>{outcomeNames[e.outcome]??"Registrado"}{e.reason&&<small>{e.reason}</small>}</>},
  {id:"resource",header:"Recurso",accessor:e=>e.resourceId,cell:e=><><code>{e.resourceId}</code>{e.revision!==null&&<small>Revisão {e.revision}</small>}</>},
 ];
 return <><div className="page-heading"><div><span className="eyebrow">OPERAÇÃO DA CENTRAL</span><h1>Auditoria e diagnóstico</h1><p>Estado atual e histórico das ações neste ambiente.</p></div></div><Tabs value={tab} onValueChange={id=>{setTab(id);setBusy(false);setError("");}}><TabsList className="tabs" aria-label="Operação"><TabsTrigger value="diagnostic">Diagnóstico</TabsTrigger><TabsTrigger value="audit">Auditoria</TabsTrigger></TabsList><TabsContent value={tab}>{error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 {tab==="diagnostic"&&diagnostic&&<>
  <div className="stats-grid"><Card className="panel metric gap-3 p-6"><span>Serviço de mídia</span><strong>{diagnostic.workerAvailable?"Disponível":"Indisponível"}</strong></Card><Card className="panel metric gap-3 p-6"><span>Câmeras ativas</span><strong>{diagnostic.activeCameras}<small>de {diagnostic.cameraCount} cadastradas</small></strong></Card><Card className="panel metric gap-3 p-6"><span>Armazenamento</span><strong>{diagnostic.storageConfigured?"Configurado":"Pendente"}</strong></Card></div>
  <p className="muted-text">Verificado em {dateCell(diagnostic.observedAt)}. A disponibilidade do serviço não substitui o estado de cada captura.</p>
  <DataTable key={`${tenant.id}:diagnostic`} data={diagnostic.items} columns={statusColumns} getRowId={c=>c.cameraId} label="Diagnóstico das câmeras" searchPlaceholder="Buscar câmeras e estados…" emptyMessage="Nenhuma câmera disponível para diagnóstico."/>
  <h2 className="section-spaced">Histórico da captura</h2>
  <DataTable key={`${tenant.id}:events`} data={events} columns={eventColumns} getRowId={e=>e.id} label="Histórico da captura" searchPlaceholder="Buscar no histórico carregado…" emptyMessage="Ainda não há mudanças registradas." partial hasMore={!!eventCursor} onLoadMore={olderEvents} loadingMore={busy}/>
 </>}
 {tab==="audit"&&<>
  {identity.user.globalAdmin&&<Label className="check"><Checkbox checked={global} onCheckedChange={checked=>{auditRequest.current++;auditPending.current=false;setBusy(false);setGlobal(checked===true);setAudit([]);setCursor("");}}/>Exibir ações da instalação (contas e serviços)</Label>}
  <form className="audit-filter" onSubmit={e=>{e.preventDefault();if(action!==appliedAction){setAudit([]);setCursor("");setAppliedAction(action);}else void loadAudit();}}>
   <Label className="field-label">Ação no histórico completo<NativeSelect value={action} onChange={e=>setAction(e.target.value)} className="w-full"><NativeSelectOption value="">Todas as ações</NativeSelectOption>{Object.entries(actionNames).map(([key,label])=><NativeSelectOption key={key} value={key}>{label}</NativeSelectOption>)}</NativeSelect></Label>
   <Button disabled={busy} variant="outline">Atualizar</Button>
  </form>
  <DataTable key={`${tenant.id}:audit:${global}:${appliedAction}`} data={audit} columns={auditColumns} getRowId={e=>e.id} label="Auditoria" searchPlaceholder="Buscar nas ações carregadas…" emptyMessage="Nenhuma ação encontrada neste escopo." loading={busy&&!audit.length} partial hasMore={!!cursor} onLoadMore={()=>loadAudit(true)} loadingMore={busy}/>
 </>}
 </TabsContent></Tabs></>;
}
