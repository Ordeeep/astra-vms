"use client";

import {useEffect,useState,useRef,useMemo,type FormEvent} from "react";
import {ConfirmationDialog,FormDialog} from "@/components/form-dialog";
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
import {Activity,Layers,Settings,SlidersHorizontal,CalendarClock} from "lucide-react";
import {api,ApiError,type Tenant} from "@/lib/api";
import {allPages,type Site,type Camera,type CameraDisplayMode,type Profile} from "@/components/management";
import {CameraConnectionEditor,emptyCameraConnection,savedCameraConnection,type CameraConnection,type ConnectionType} from "@/components/camera-connection-editor";
import {CameraDiagnostic} from "@/components/diagnostic";
import {onEnvironmentChange} from "@/lib/events";
import {CameraAvailabilityBadge,CameraAvailabilityCounts} from "@/components/camera-availability";
import {useIsMobile} from "@/hooks/use-mobile";
import {availabilityLabels,cameraAvailability} from "@/lib/camera-availability";

import {CameraRecordingEditor} from "@/components/camera-recording-editor";
import {newRecordingConfig,recordingLabels,type RecordingConfig,type RecordingStatus,type RecordingStatusPage} from "@/lib/camera-recording";

export function CamerasWorkspace({tenant,onStorage,registerCloseGuard}:{tenant:Tenant;onStorage?:()=>void;registerCloseGuard?:(guard:(()=>boolean)|null)=>void}){
 const isMobile=useIsMobile();
 const base=`/tenants/${tenant.id}`;const [sites,setSites]=useState<Site[]>([]),[cameras,setCameras]=useState<Camera[]>([]);
 const [error,setError]=useState(""),[busy,setBusy]=useState(false),[loading,setLoading]=useState(true);
 const [refreshError,setRefreshError]=useState("");
 const [editor,setEditor]=useState<{type:string;camera?:Camera}|null>(null);
 const [deleting,setDeleting]=useState<Camera|null>(null),[result,setResult]=useState("");
 const heading=useRef<HTMLHeadingElement|null>(null);
 const removed=useRef(new Set<string>());
 const [cameraSite,setCameraSite]=useState("");
 const editorTrigger=useRef<HTMLElement|null>(null);
 const editorContent=useRef<HTMLDivElement>(null);
 const deleteTrigger=useRef<HTMLElement|null>(null);
 const [cameraName,setCameraName]=useState(""),[cameraActive,setCameraActive]=useState(true);
 const [displayMode,setDisplayMode]=useState<CameraDisplayMode>("contain");
 const [profiles,setProfiles]=useState<Profile[]>([{id:"main",name:"Principal",url:""}]);
 const [connectionType,setConnectionType]=useState<ConnectionType>("rtsp"),[connection,setConnection]=useState<CameraConnection>(emptyCameraConnection);
 const [caps,setCaps]=useState<Record<string,boolean>>({});
 const [connectionChanged,setConnectionChanged]=useState(false);
 const [now,setNow]=useState(()=>Date.now());
 const [recording,setRecording]=useState<RecordingConfig>(newRecordingConfig),[recordingLoading,setRecordingLoading]=useState(false),[recordingFailed,setRecordingFailed]=useState(false);
 const [recordingStatuses,setRecordingStatuses]=useState<RecordingStatus[]>([]),[recordingStatusError,setRecordingStatusError]=useState("");
 const [readiness,setReadiness]=useState<{storageConfigured:boolean;quotaConfigured:boolean}|null>(null);
 const [impact,setImpact]=useState<number|null>(null),[confirmImpact,setConfirmImpact]=useState(false),[conflict,setConflict]=useState(false);
 const draftBase=useRef(""),editorEpoch=useRef(0);
 const canInventory=!!editor&&(!editor.camera||!!editor.camera.capabilities["inventory.manage"]);
 const canRecording=!!editor&&(editor.camera?!!editor.camera.capabilities["recording.manage"]:!!(caps["recording.manage"]||sites.find(site=>site.id===cameraSite)?.capabilities?.["recording.manage"]));
 const draft=JSON.stringify({name:cameraName,site:cameraSite,active:cameraActive,displayMode,type:connectionType,connection,profiles,recording});
 const dirty=!!editor&&draft!==draftBase.current;
 const canLeave=()=>!busy&&(!dirty||window.confirm("Descartar as alterações não salvas desta câmera?"));
 function closeEditor(){editorEpoch.current++;setEditor(null);setError("");}
 useEffect(()=>{registerCloseGuard?.(canLeave);return()=>registerCloseGuard?.(null);},[dirty,busy,registerCloseGuard]);
 useEffect(()=>{if(!dirty)return;const unload=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue="";};window.addEventListener("beforeunload",unload);return()=>window.removeEventListener("beforeunload",unload);},[dirty]);
 useEffect(()=>{
  let active=true,pending=false;
  setRecordingStatuses([]);setReadiness(null);setRecordingStatusError("");
  const poll=async()=>{if(pending)return;pending=true;try{
   const first=await api<RecordingStatusPage>(base+"/recording-status","GET",undefined,true);
   const items=[...first.items];let cursor=first.nextCursor;
   while(cursor){const next=await api<RecordingStatusPage>(base+"/recording-status?cursor="+encodeURIComponent(cursor),"GET",undefined,true);items.push(...next.items);cursor=next.nextCursor;}
   if(active){setRecordingStatuses(items);setReadiness(first);setRecordingStatusError("");}
  }catch{if(active){setRecordingStatuses([]);setReadiness(null);setRecordingStatusError("Não foi possível atualizar o estado da gravação.");}}finally{pending=false;}};
  void poll();const timer=window.setInterval(()=>{if(!document.hidden)void poll();},2000);
  const unsubscribe=onEnvironmentChange(["CaptureStateChanged","AuthorizationChanged"],()=>void poll());
  return()=>{active=false;window.clearInterval(timer);unsubscribe();};
 },[tenant.id]);
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
  return()=>{loadEpoch.current++;editorEpoch.current++;window.clearInterval(poll);unsubscribe();};
 },[tenant.id]);
 useEffect(()=>{
  const deadlines=cameras.map(camera=>Date.parse(camera.availability?.validUntil??"")).filter(deadline=>Number.isFinite(deadline)&&deadline>now);
  if(!deadlines.length)return;
  const timer=window.setTimeout(()=>setNow(Date.now()),Math.min(60_000,Math.max(1,Math.min(...deadlines)-Date.now()+1)));
  return()=>window.clearTimeout(timer);
 },[cameras,now]);
 async function loadRecording(camera:Camera,epoch:number){
  setRecordingLoading(true);setRecordingFailed(false);
  try{const value=await api<RecordingConfig>(base+`/cameras/${camera.id}/recording`,"GET",undefined,true);
   if(editorEpoch.current!==epoch)return;
   setRecording(value);draftBase.current=JSON.stringify({...JSON.parse(draftBase.current),recording:value});
  }catch(e){if(editorEpoch.current===epoch){setRecordingFailed(true);setError((e as Error).message);}}
  finally{if(editorEpoch.current===epoch)setRecordingLoading(false);}
 }
 function open(type:string,camera?:Camera){
  const epoch=++editorEpoch.current;
  const initial={name:camera?.name??"",site:camera?.siteId??"",active:camera?.active??true,displayMode:camera?.displayMode??"contain",type:camera?.connectionType??"rtsp",connection:savedCameraConnection(camera?.connection),profiles:camera?.profiles.map(p=>({...p,url:""}))??[{id:"main",name:"Principal",url:""}],recording:newRecordingConfig()};
  draftBase.current=JSON.stringify(initial);
  editorTrigger.current=document.activeElement as HTMLElement;setCameraSite(initial.site);setCameraName(initial.name);setCameraActive(initial.active);setError("");
  setDisplayMode(initial.displayMode);
  setEditor({type:camera&&!camera.capabilities["inventory.manage"]&&camera.capabilities["recording.manage"]?"recording":type,camera});
  setConnectionType(initial.type);setConnection(initial.connection);setConnectionChanged(false);setProfiles(initial.profiles);
  setRecording(initial.recording);setImpact(null);setConfirmImpact(false);setConflict(false);setRecordingFailed(false);setRecordingLoading(false);
  if(camera?.capabilities["recording.manage"])void loadRecording(camera,epoch);
 }
 async function reopen(){
  if(!editor?.camera||!canLeave())return;
  try{const camera=await api<Camera>(base+`/cameras/${editor.camera.id}`,"GET",undefined,true);open(editor.type,camera);}catch(e){setError((e as Error).message);}
 }
 function confirmDelete(camera:Camera){if(!canLeave())return;deleteTrigger.current=document.activeElement as HTMLElement;setError("");setResult("");setDeleting(camera);}
 async function removeCamera(){
  if(!deleting||busy)return;
  setBusy(true);setError("");
  try{
   await api(base+`/cameras/${deleting.id}`,"DELETE",{revision:deleting.revision},true);
   removed.current.add(deleting.id);
   setCameras(current=>current.filter(camera=>camera.id!==deleting.id));
   setDeleting(null);setEditor(null);setResult("Câmera excluída. As gravações serão removidas em segundo plano.");
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
  e.preventDefault();if(!editor||busy||(!canInventory&&!canRecording)||recordingLoading||recordingFailed||conflict)return;
  if(canRecording&&recording.enabled&&!profiles.some(profile=>profile.id===recording.profile&&profile.enabled!==false)){setEditor({...editor,type:"recording"});setError("Escolha um perfil válido ou desabilite a gravação antes de salvar.");return;}
  if(impact!==null&&!confirmImpact){setEditor({...editor,type:"recording"});setError("Confirme a aplicação da retenção ao histórico existente.");return;}
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
   const recordingChanged=JSON.stringify(recording)!==JSON.stringify(JSON.parse(draftBase.current).recording);
   const recordingInput=canRecording&&(!editor.camera||!canInventory||recordingChanged)?{recording:{...recording,confirmImpact}}:{};
   if(editor.camera)await api(base+`/cameras/${editor.camera.id}/configuration`,"PATCH",{revision:editor.camera.revision,...(canInventory?{name:f.get("name"),siteId:cameraSite,active:cameraActive,displayMode,...(connectionChanged?{source:connectionInput()}:{})}:{recordingOnly:true}),...recordingInput},true);
   else await api(base+"/cameras","POST",{name:f.get("name"),siteId:cameraSite,displayMode,...connectionInput(),...recordingInput},true);
   editorEpoch.current++;setEditor(null);setResult("Câmera salva. O estado da gravação indica quando a captura estiver em andamento.");await load();
  }catch(e){
   if(e instanceof ApiError&&e.code==="retention_impact_confirmation"){setImpact(Number(e.details.eligibleBytes));setConfirmImpact(false);setEditor({...editor,type:"recording"});}
   else if(e instanceof ApiError&&e.code==="revision_conflict"){setConflict(true);setError("A câmera ou a configuração de gravação foi alterada por outro operador. Suas alterações foram mantidas. Reabra a versão atual para continuar.");}
   else {setError((e as Error).message);if(e instanceof ApiError&&["recording_profile_unavailable","invalid_recording_config"].includes(e.code)&&canRecording)setEditor({...editor,type:"recording"});}
  }finally{setBusy(false);}
 }

 const displayedCameras=useMemo(()=>cameras.map(camera=>({...camera,availability:cameraAvailability(camera,now)})),[cameras,now]);
 const columns:DataTableColumn<Camera>[]=[
  {id:"name",header:"Câmera",accessor:c=>c.name,cell:c=><strong>{c.name}</strong>,hideable:false},
  {id:"site",header:"Site",accessor:c=>sites.find(s=>s.id===c.siteId)?.name??"Site restrito",filter:"select"},
  {id:"integration",header:"Integração",accessor:c=>c.connectionType==="hikvision"?"Hikvision":c.connectionType==="dahua"?"Dahua":"RTSP",filter:"select"},
  {id:"profiles",header:"Perfis",accessor:c=>c.profiles.length?c.profiles.map(p=>p.id):["Aguardando identificação"],filter:"select",cell:c=><>{!c.profiles.length&&<small>Aguardando identificação</small>}{c.profiles.map(p=><Badge className="mini-badge" key={p.id} variant="secondary">{p.id}</Badge>)}</>},
  {id:"operation",header:"Operação",accessor:c=>c.effectiveActive?"Habilitada":"Desativada",filter:"select",cell:c=><Badge className="badge" variant={!c.effectiveActive?"secondary":"outline"}>{c.effectiveActive?"Habilitada":"Desativada"}</Badge>},
  {id:"availability",header:"Disponibilidade",accessor:c=>availabilityLabels[cameraAvailability(c,now).state],filter:"select",options:Object.values(availabilityLabels).map(label=>({value:label,label})),cell:c=><CameraAvailabilityBadge camera={c} now={now}/>},
  {id:"recording",header:"Gravação",accessor:(c:Camera)=>{const status=recordingStatuses.find(s=>s.cameraId===c.id);return status?recordingLabels[status.code]??status.code:recordingStatusError?"Estado indisponível":"—";},filter:"select",cell:(c:Camera)=>{const status=recordingStatuses.find(s=>s.cameraId===c.id);return <Badge variant={status?.code==="recording"?"outline":"secondary"}>{status?recordingLabels[status.code]??status.code:recordingStatusError?"Estado indisponível":"—"}</Badge>;}},

 ];
 return <>
  <div className="page-heading"><div><h1 ref={heading} tabIndex={-1}>Câmeras</h1><p>Gerencie as câmeras e suas fontes de vídeo por site neste tenant.</p></div>{(caps["inventory.manage"]||sites.some(s=>s.capabilities?.["inventory.manage"]))&&<Button onClick={()=>open("camera")} variant="default">+ Nova câmera</Button>}</div>
  {!loading&&<CameraAvailabilityCounts cameras={displayedCameras} now={now}/>}
  {recordingStatusError&&<Alert role="alert" variant="destructive"><AlertDescription>{recordingStatusError}</AlertDescription></Alert>}
  {refreshError&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>Não foi possível atualizar as câmeras. {refreshError}</AlertDescription></Alert>}
  {result&&<p role="status">{result}</p>}
  {error&&!editor&&!deleting&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
  {editor&&<FormDialog noValidate hasChanges={dirty} className="camera-details-dialog" returnFocus={editorTrigger.current} fallbackFocus={()=>heading.current} saveDisabled={(!canInventory&&!canRecording)||recordingLoading||recordingFailed||conflict} saveLabel="Salvar" title={editor.camera?"Editar câmera":"Cadastro da câmera"} onClose={closeEditor} onSubmit={save} busy={busy} footerActions={editor.camera?.capabilities["camera.delete"]&&<Button type="button" variant="destructive" disabled={busy} onClick={()=>confirmDelete(editor.camera!)}>Excluir câmera</Button>}>
   <Tabs value={editor.type} onValueChange={type=>{
    if(editorContent.current)editorContent.current.scrollTop=0;
    setEditor({...editor,type});
    if(!conflict&&!recordingFailed)setError("");
   }} orientation={isMobile?"horizontal":"vertical"} className="camera-editor-layout">
    <TabsList aria-label="Edição da câmera" className="camera-editor-nav">
     {canInventory&&<TabsTrigger value="general"><Settings aria-hidden="true"/>Geral</TabsTrigger>}
     <TabsTrigger value="camera"><SlidersHorizontal aria-hidden="true"/>Informações</TabsTrigger>
     {canInventory&&<TabsTrigger value="streams"><Layers aria-hidden="true"/>Streams</TabsTrigger>}
     {(canRecording||editor.camera?.capabilities.playback)&&<TabsTrigger value="recording"><CalendarClock aria-hidden="true"/>Gravação</TabsTrigger>}
     {editor.camera&&canInventory&&<TabsTrigger value="diagnostic"><Activity aria-hidden="true"/>Diagnóstico</TabsTrigger>}
    </TabsList>
    <div ref={editorContent} className="camera-editor-content">
   {error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
   {conflict&&<Button type="button" variant="outline" onClick={()=>void reopen()}>Reabrir versão atual</Button>}
   {canInventory&&<TabsContent value="general" className="camera-editor-section">
    <div className="grid max-w-72 gap-2">
     <Label htmlFor="camera-display-mode">Visualizando</Label>
     <NativeSelect id="camera-display-mode" className="w-full" value={displayMode} onChange={event=>setDisplayMode(event.target.value as CameraDisplayMode)}>
      <NativeSelectOption value="cover">Espalhar pela célula</NativeSelectOption>
      <NativeSelectOption value="4:3">Forçar 4:3</NativeSelectOption>
      <NativeSelectOption value="contain">Manter a proporção</NativeSelectOption>
     </NativeSelect>
    </div>
   </TabsContent>}
   {!canInventory&&<TabsContent value="camera" className="camera-editor-section"><h3>Identificação da câmera</h3><p><strong>{editor.camera?.name}</strong></p><p>Site: {sites.find(site=>site.id===cameraSite)?.name??"Site restrito"}</p></TabsContent>}
   {canInventory&&<CameraConnectionEditor key={`${connectionType}:${editor.camera?.id??"new"}`} tenant={tenant.id} siteId={cameraSite} cameraId={editor.camera?.id} maintenance={!!editor.camera&&connectionType===editor.camera.connectionType} savedProfileIds={editor.camera?.profiles.map(p=>p.id)} connectionType={connectionType} onTypeChange={changeConnectionType} connection={connection} onConnectionChange={value=>{setConnectionChanged(true);setConnection(value);}} profiles={profiles} onProfilesChange={value=>{setConnectionChanged(true);setProfiles(value);}} render={(fields,streams)=><>
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
   </>}/> }

   {!canRecording&&editor.camera?.capabilities.playback&&<TabsContent value="recording" className="camera-editor-section"><h3>Gravação</h3><p>Estado atual: {recordingLabels[recordingStatuses.find(status=>status.cameraId===editor.camera?.id)?.code??""]??"Não informado"}</p><p>Última escrita: {(()=>{const written=recordingStatuses.find(status=>status.cameraId===editor.camera?.id)?.lastWrite;return written?new Date(written).toLocaleString():"Nenhuma escrita informada";})()}</p><p>Você tem acesso à consulta; a configuração exige permissão de gestão de gravação.</p></TabsContent>}
   {canRecording&&<TabsContent forceMount hidden={editor.type!=="recording"} value="recording" data-camera-tab="recording" className="camera-editor-section">
    {recordingLoading?<p role="status">Carregando configuração de gravação…</p>:recordingFailed?<Button type="button" variant="outline" onClick={()=>void loadRecording(editor.camera!,editorEpoch.current)}>Tentar carregar gravação novamente</Button>:<CameraRecordingEditor config={recording} onChange={value=>{setRecording(value);setImpact(null);setConfirmImpact(false);}} profiles={profiles} timezone={sites.find(site=>site.id===cameraSite)?.timezone??recordingStatuses.find(status=>status.cameraId===editor.camera?.id)?.timezone??"Fuso do site"} status={recordingStatuses.find(status=>status.cameraId===editor.camera?.id)} storagePending={!!readiness&&(!readiness.storageConfigured||!readiness.quotaConfigured)} canManageStorage={!!onStorage&&!!caps["recording.manage"]} onStorage={()=>{if(canLeave()){draftBase.current=draft;setEditor(null);onStorage?.();}}} impact={impact} confirmImpact={confirmImpact} onConfirmImpact={setConfirmImpact}/>}
   </TabsContent>}
   {editor.camera&&canInventory&&<TabsContent value="diagnostic" className="camera-editor-section">{editor.type==="diagnostic"&&<CameraDiagnostic key={editor.camera.id} tenant={tenant.id} camera={editor.camera}/>}</TabsContent>}
    </div>
   </Tabs>
  </FormDialog>}
  {deleting&&<ConfirmationDialog title="Excluir câmera" returnFocus={deleteTrigger.current} fallbackFocus={()=>heading.current}
   description={`A câmera “${deleting.name}” do site “${sites.find(site=>site.id===deleting.siteId)?.name??"Site restrito"}” e todas as suas gravações serão excluídas.`}
   busy={busy} confirmLabel={busy?"Excluindo…":"Excluir câmera"} onCancel={()=>{setDeleting(null);setError("");}} onConfirm={()=>void removeCamera()}>
   {error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
  </ConfirmationDialog>}
  {loading?<p className="flex items-center gap-2" role="status"><Spinner aria-hidden="true"/>Carregando câmeras…</p>:<DataTable key={tenant.id} data={displayedCameras} columns={columns} getRowId={c=>c.id} onRowClick={c=>open("camera",c)} canActivateRow={c=>!!(c.capabilities["inventory.manage"]||c.capabilities["recording.manage"]||c.capabilities["camera.delete"]||c.capabilities.playback)} rowLabel={c=>`Editar câmera ${c.name}`} label="Câmeras" searchPlaceholder="Buscar câmeras…" emptyMessage={<><h2>Nenhuma câmera disponível</h2><p>Cadastre uma câmera ou solicite acesso ao administrador.</p></>}/>}
 </>;
}
