"use client";

import {useEffect,useState,useRef,useMemo,type FormEvent} from "react";
import {EditorDialog,FormDialog} from "@/components/form-dialog";
import {Spinner} from "@/components/ui/spinner";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import {Button} from "@/components/ui/button";
import {AlertDescription,Alert} from "@/components/ui/alert";
import {Label} from "@/components/ui/label";
import {Input} from "@/components/ui/input";
import {NativeSelectOption,NativeSelect} from "@/components/ui/native-select";
import {Checkbox} from "@/components/ui/checkbox";
import {Badge} from "@/components/ui/badge";
import {Tabs,TabsList,TabsTrigger,TabsContent} from "@/components/ui/tabs";
import {Activity,Layers,SlidersHorizontal} from "lucide-react";
import {api,type Tenant} from "@/lib/api";
import {allPages,type Site,type Camera,type Profile} from "@/components/management";
import {CameraConnectionEditor,emptyCameraConnection,savedCameraConnection,type CameraConnection,type ConnectionType} from "@/components/camera-connection-editor";
import {CameraDiagnostic} from "@/components/diagnostic";
import {onEnvironmentChange} from "@/lib/events";
import {CameraAvailabilityBadge,CameraAvailabilityCounts} from "@/components/camera-availability";
import {availabilityLabels,cameraAvailability} from "@/lib/camera-availability";

export function CamerasWorkspace({tenant}:{tenant:Tenant}){
 const base=`/tenants/${tenant.id}`;const [sites,setSites]=useState<Site[]>([]),[cameras,setCameras]=useState<Camera[]>([]);
 const [error,setError]=useState(""),[busy,setBusy]=useState(false),[loading,setLoading]=useState(true);
 const [refreshError,setRefreshError]=useState("");
 const [editor,setEditor]=useState<{type:string;camera?:Camera}|null>(null);
 const [deleting,setDeleting]=useState<Camera|null>(null),[result,setResult]=useState("");
 const heading=useRef<HTMLHeadingElement|null>(null);
 const removed=useRef(new Set<string>());
 const [cameraSite,setCameraSite]=useState("");
 const editorTrigger=useRef<HTMLElement|null>(null);
 const [cameraName,setCameraName]=useState(""),[cameraActive,setCameraActive]=useState(true);
 const [profiles,setProfiles]=useState<Profile[]>([{id:"main",name:"Principal",url:""}]);
 const [connectionType,setConnectionType]=useState<ConnectionType>("rtsp"),[connection,setConnection]=useState<CameraConnection>(emptyCameraConnection);
 const [caps,setCaps]=useState<Record<string,boolean>>({});
 const [connectionChanged,setConnectionChanged]=useState(false);
 const [now,setNow]=useState(()=>Date.now());
 const loadEpoch=useRef(0),loadSequence=useRef(0),pendingLoads=useRef(0);
 const activeTenant=useRef(tenant.id);activeTenant.current=tenant.id;
 async function load(){
  const epoch=loadEpoch.current,sequence=++loadSequence.current;pendingLoads.current++;
  const current=()=>activeTenant.current===tenant.id&&loadEpoch.current===epoch&&loadSequence.current===sequence;
  try{
   const [s,c,p]=await Promise.all([allPages<Site>(base+"/sites"),allPages<Camera>(base+"/cameras"),api<Record<string,boolean>>(base+"/capabilities","GET",undefined,true)]);
   if(!current())return;
   setSites(s);setCameras(c.filter(camera=>!removed.current.has(camera.id)));setCaps(p);setRefreshError("");setNow(Date.now());
  }catch(e){if(current()){
   setRefreshError((e as Error).message);
   setCameras(existing=>existing.map(camera=>({...camera,availability:{...camera.availability,state:"unknown",code:"monitor_unavailable",warnings:[]}})));
  }}finally{pendingLoads.current--;if(current())setLoading(false);}
 }
 useEffect(()=>{
  removed.current.clear();setCameras([]);setSites([]);setCaps({});setRefreshError("");setError("");setResult("");setEditor(null);setDeleting(null);setLoading(true);void load();
  const unsubscribe=onEnvironmentChange(["CameraStatusChanged"],()=>void load());
  const poll=window.setInterval(()=>{if(!document.hidden&&pendingLoads.current===0)void load();},10_000);
  return()=>{loadEpoch.current++;window.clearInterval(poll);unsubscribe();};
 },[tenant.id]);
 useEffect(()=>{
  const deadlines=cameras.map(camera=>Date.parse(camera.availability?.validUntil??"")).filter(deadline=>Number.isFinite(deadline)&&deadline>now);
  if(!deadlines.length)return;
  const timer=window.setTimeout(()=>setNow(Date.now()),Math.min(60_000,Math.max(1,Math.min(...deadlines)-Date.now()+1)));
  return()=>window.clearTimeout(timer);
 },[cameras,now]);
 function open(type:string,camera?:Camera){
  editorTrigger.current=document.activeElement as HTMLElement;setCameraSite(camera?.siteId??"");setCameraName(camera?.name??"");setCameraActive(camera?.active??true);setError("");setEditor({type,camera});
  setConnectionType(camera?.connectionType??"rtsp");setConnection(savedCameraConnection(camera?.connection));
  setConnectionChanged(false);
  setProfiles(camera?.profiles.map(p=>({...p,url:""}))??[{id:"main",name:"Principal",url:""}]);
 }
 function confirmDelete(camera:Camera){editorTrigger.current=document.activeElement as HTMLElement;setError("");setResult("");setDeleting(camera);}
 async function removeCamera(){
  if(!deleting||busy)return;
  setBusy(true);setError("");
  try{
   await api(base+`/cameras/${deleting.id}`,"DELETE",{revision:deleting.revision},true);
   removed.current.add(deleting.id);
   setCameras(current=>current.filter(camera=>camera.id!==deleting.id));
   setDeleting(null);setResult("Câmera excluída. As gravações serão removidas em segundo plano.");
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }
 function connectionInput(){return {connectionType,...(connectionType!=="rtsp"?{connection:{host:connection.host.trim(),protocol:connection.protocol,httpPort:connection.httpPort,username:connection.username,...(connection.password?{password:connection.password}:{})}}:{profiles})};}
 function changeConnectionType(type:ConnectionType){
  if(!editor)return;
  setConnectionChanged(true);setConnectionType(type);
  const original=type===editor.camera?.connectionType;
  setConnection(savedCameraConnection(original?editor.camera?.connection:undefined));
  setProfiles(original?editor.camera!.profiles.map(p=>({...p,url:""})):type!=="rtsp"?[]:[{id:"main",name:"Principal",url:""}]);
  setError("");
 }
 async function save(e:FormEvent<HTMLFormElement>){
  e.preventDefault();if(!editor||editor.type==="diagnostic")return;
  const form=e.currentTarget;
  if(!form.checkValidity()){
   const invalid=form.querySelector<HTMLInputElement|HTMLSelectElement>('input:invalid,select:invalid');
   const type=invalid?.closest<HTMLElement>('[data-camera-tab]')?.dataset.cameraTab??"camera";
   setEditor({...editor,type});
   requestAnimationFrame(()=>{invalid?.focus();invalid?.reportValidity();});
   return;
  }
  const f=new FormData(form);setBusy(true);setError("");
  try{
   if(editor.camera)await api(base+`/cameras/${editor.camera.id}/configuration`,"PATCH",{name:f.get("name"),siteId:cameraSite,active:cameraActive,revision:editor.camera.revision,...(connectionChanged?{source:connectionInput()}:{})},true);
   else await api(base+"/cameras","POST",{name:f.get("name"),siteId:cameraSite,...connectionInput()},true);
   setEditor(null);await load();
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }
 // A local expiry changes the table's data too, so its cached filter values
 // agree with badges and counters even if the next HTTP response is delayed.
 const displayedCameras=useMemo(()=>cameras.map(camera=>({...camera,availability:cameraAvailability(camera,now)})),[cameras,now]);
 const columns:DataTableColumn<Camera>[]=[
  {id:"name",header:"Câmera",accessor:c=>c.name,cell:c=><strong>{c.name}</strong>,hideable:false},
  {id:"site",header:"Site",accessor:c=>sites.find(s=>s.id===c.siteId)?.name??"Site restrito",filter:"select"},
  {id:"integration",header:"Integração",accessor:c=>c.connectionType==="hikvision"?"Hikvision":c.connectionType==="dahua"?"Dahua":"RTSP",filter:"select"},
  {id:"profiles",header:"Perfis",accessor:c=>c.profiles.length?c.profiles.map(p=>p.id):["Aguardando identificação"],filter:"select",cell:c=><>{!c.profiles.length&&<small>Aguardando identificação</small>}{c.profiles.map(p=><Badge className="mini-badge" key={p.id} variant="secondary">{p.id}</Badge>)}</>},
  {id:"operation",header:"Operação",accessor:c=>c.effectiveActive?"Habilitada":"Desativada",filter:"select",cell:c=><Badge className="badge" variant={!c.effectiveActive?"secondary":"outline"}>{c.effectiveActive?"Habilitada":"Desativada"}</Badge>},
  {id:"availability",header:"Disponibilidade",accessor:c=>availabilityLabels[cameraAvailability(c,now).state],filter:"select",options:Object.values(availabilityLabels).map(label=>({value:label,label})),cell:c=><CameraAvailabilityBadge camera={c} now={now}/>},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:c=><div className="row-actions">{c.capabilities["inventory.manage"]&&<Button onClick={()=>open("camera",c)} variant="ghost">Editar</Button>}{c.capabilities["camera.delete"]&&<Button data-camera-delete={c.id} onClick={()=>confirmDelete(c)} variant="destructive" aria-label={`Excluir câmera ${c.name}`}>Excluir</Button>}</div>},
 ];
 return <>
  <div className="page-heading"><div><h1 ref={heading} tabIndex={-1}>Câmeras</h1><p>Gerencie as câmeras e suas fontes de vídeo por site neste tenant.</p></div>{(caps["inventory.manage"]||sites.some(s=>s.capabilities?.["inventory.manage"]))&&<Button onClick={()=>open("camera")} variant="default">+ Nova câmera</Button>}</div>
  {!loading&&<CameraAvailabilityCounts cameras={displayedCameras} now={now}/>}
  {refreshError&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>Não foi possível atualizar as câmeras. {refreshError}</AlertDescription></Alert>}
  {result&&<p role="status">{result}</p>}
  {error&&!editor&&!deleting&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
  {deleting&&<EditorDialog title="Excluir câmera" showClose={false} returnFocus={editorTrigger.current} fallbackFocus={()=>document.querySelector<HTMLButtonElement>(`button[data-camera-delete="${deleting.id}"]`)??heading.current} busy={busy} onClose={()=>{setDeleting(null);setError("");}}>
   <p>Excluir a câmera “{deleting.name}” do site “{sites.find(site=>site.id===deleting.siteId)?.name??"Site restrito"}” e todas as suas gravações?</p>
   {error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
   <div className="form-actions"><Button type="button" variant="ghost" disabled={busy} onClick={()=>{setDeleting(null);setError("");}}>Cancelar</Button><Button type="button" variant="destructive" disabled={busy} onClick={()=>void removeCamera()}>{busy?"Excluindo…":"Excluir"}</Button></div>
  </EditorDialog>}
  {editor&&<FormDialog noValidate showClose={false} className="camera-details-dialog" returnFocus={editorTrigger.current} saveLabel={editor.type==="diagnostic"?null:editor.camera?"Salvar informações":"Salvar"} title={editor.camera?"Editar câmera":"Cadastro da câmera"} onClose={()=>{setEditor(null);setError("");}} onSubmit={save} busy={busy}>
   <Tabs value={editor.type} onValueChange={type=>{setEditor({...editor,type});setError("");}} orientation="vertical" className="camera-editor-layout">
    <TabsList aria-label="Edição da câmera" className="camera-editor-nav">
     <TabsTrigger value="camera"><SlidersHorizontal aria-hidden="true"/>Informações</TabsTrigger>
     <TabsTrigger value="streams"><Layers aria-hidden="true"/>Streams</TabsTrigger>
     {editor.camera&&<TabsTrigger value="diagnostic"><Activity aria-hidden="true"/>Diagnóstico</TabsTrigger>}
    </TabsList>
    <div className="camera-editor-content">
   {error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
   <CameraConnectionEditor key={`${connectionType}:${editor.camera?.id??"new"}`} tenant={tenant.id} siteId={cameraSite} cameraId={editor.camera?.id} maintenance={!!editor.camera&&connectionType===editor.camera.connectionType} savedProfileIds={editor.camera?.profiles.map(p=>p.id)} connectionType={connectionType} onTypeChange={changeConnectionType} connection={connection} onConnectionChange={value=>{setConnectionChanged(true);setConnection(value);}} profiles={profiles} onProfilesChange={value=>{setConnectionChanged(true);setProfiles(value);}} render={(fields,streams)=><>
   {/* Keep fields mounted for validation and preserve connection-test results between sections. */}
   <TabsContent forceMount value="camera" hidden={editor.type!=="camera"} data-camera-tab="camera" className="camera-editor-section">
    <h3>Identificação e conexão</h3>
    <div className="form-grid camera-identification-grid">
     <Label className="field-label">Nome<Input name="name" required maxLength={200} value={cameraName} onChange={e=>setCameraName(e.target.value)}/></Label>
     <Label className="field-label">Site<NativeSelect name="site" required value={cameraSite} onChange={e=>setCameraSite(e.target.value)} className="w-full"><NativeSelectOption value="" disabled>Selecione um site</NativeSelectOption>{editor.camera&&!sites.some(s=>s.id===editor.camera!.siteId)&&<NativeSelectOption value={editor.camera.siteId}>Site restrito</NativeSelectOption>}{sites.filter(s=>s.id===editor.camera?.siteId||s.capabilities?.["inventory.manage"]).map(s=><NativeSelectOption key={s.id} value={s.id}>{s.name}{!s.active?" · Desativado":""}</NativeSelectOption>)}</NativeSelect></Label>
    </div>
    {editor.camera&&<Label className="check"><Checkbox name="active" checked={cameraActive} onCheckedChange={checked=>setCameraActive(checked===true)}/>Cadastro ativo</Label>}
    <section aria-label="Conexão e configurações" className="camera-connection-section">
     <h3>Conexão</h3>
     {fields}
    </section>
   </TabsContent>
   <TabsContent forceMount value="streams" hidden={editor.type!=="streams"} data-camera-tab="streams" className="camera-editor-section"><h3>Streams de vídeo</h3>{streams}</TabsContent>
   </>}/>

   {editor.camera&&<TabsContent value="diagnostic" className="camera-editor-section">{editor.type==="diagnostic"&&<CameraDiagnostic key={editor.camera.id} tenant={tenant.id} camera={editor.camera}/>}</TabsContent>}
    </div>
   </Tabs>
  </FormDialog>}
  {loading?<p className="flex items-center gap-2" role="status"><Spinner aria-hidden="true"/>Carregando câmeras…</p>:<DataTable key={tenant.id} data={displayedCameras} columns={columns} getRowId={c=>c.id} label="Câmeras" searchPlaceholder="Buscar câmeras…" emptyMessage={<><h2>Nenhuma câmera disponível</h2><p>Cadastre uma câmera ou solicite acesso ao administrador.</p></>}/>}
 </>;
}
