"use client";
import { Card } from "@/components/ui/card";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Input } from "@/components/ui/input";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import {useEffect,useRef,useState,type FormEvent} from "react";
import Hls from "hls.js";
import {api,ApiError,mediaContext,type Tenant} from "@/lib/api";
import {allPages} from "@/components/management";
import {onEnvironmentChange} from "@/lib/events";
import {archiveVideoCodecs} from "@/lib/video-compatibility";
type Camera={id:string;name:string;capabilities:Record<string,boolean>};
type Segment={id:string;cameraId:string;siteId:string;sourceVersion:string;profile:string;start:string;end:string;bytes:number};
type Session={id:string;manifest:string;delivery?:"native"|"compatible"};
type Mapping={mediaStart:number;start:string;end:string};
function dateInput(at:Date){return new Date(at.getTime()-at.getTimezoneOffset()*60000).toISOString().slice(0,19);}
function ArchivePlayer({tenant,camera,start,end,onNext}:{tenant:string;camera:Camera;start:string;end:string;onNext:(start:string)=>void}){
 const panel=useRef<HTMLDivElement>(null),video=useRef<HTMLVideoElement>(null),[error,setError]=useState(""),[instant,setInstant]=useState(""),[next,setNext]=useState<string|null>(null),[ended,setEnded]=useState(false),[preparing,setPreparing]=useState(false);
 useEffect(()=>{
  let active=true,run=0,retried=false,session:Session|undefined,hls:Hls|undefined,timer:ReturnType<typeof setInterval>|undefined;
  let lastInstant=start;
  const context=mediaContext();
  const closeTransport=()=>{
   run++;if(timer)clearInterval(timer);timer=undefined;hls?.destroy();hls=undefined;
   if(video.current){video.current.onerror=null;video.current.ontimeupdate=null;video.current.pause();video.current.removeAttribute("src");video.current.load();}
   const previous=session;session=undefined;
   if(previous)void api(`/tenants/${tenant}/playback-sessions/${previous.id}`,"DELETE",undefined,true).catch(()=>{});
  };
  const fail=(message:string,canAdapt=false)=>{
   if(!active)return;
   if(canAdapt&&!retried){
    retried=true;closeTransport();setError("");setPreparing(true);void open(true,lastInstant);return;
   }
   closeTransport();setPreparing(false);setError(message);
  };
  async function open(forceCompatibility=false,from=start){
   const generation=++run,current=()=>active&&generation===run;
   try{
    setError("");setInstant("");setNext(null);setEnded(false);setPreparing(true);
    if(!forceCompatibility)panel.current?.scrollIntoView({behavior:"smooth",block:"start"});
    const opened=await api<Session>(`/tenants/${tenant}/playback-sessions`,"POST",{cameraId:camera.id,start:from,end,codecs:archiveVideoCodecs(),forceCompatibility},true);
    if(!current()){void api(`/tenants/${tenant}/playback-sessions/${opened.id}`,"DELETE",undefined,true).catch(()=>{});return;}
    session=opened;
    let busy=false;
    const renew=async()=>{
     if(busy||!current())return;busy=true;
     try{await api(`/tenants/${tenant}/playback-sessions/${opened.id}`,"PUT",{},true);}
     catch{if(current())fail("Consulta encerrada. Verifique suas permissões ou a disponibilidade do trecho.");}
     finally{busy=false;}
    };
    timer=setInterval(()=>void renew(),600);
    const mapURL=new URL(opened.manifest,window.location.origin);mapURL.searchParams.set("format","json");
    const map=await api<{mapping:Mapping[];offset:number;nextStart:string|null}>(mapURL.pathname.replace(/^\/api\/v1/,"")+mapURL.search,"GET",undefined,true);
    if(!current())return;
    setNext(map.nextStart);
    if(!Hls.isSupported()){fail("Não foi possível iniciar o player neste navegador.");return;}
    if(!video.current)return;
    const player=new Hls({maxBufferLength:3,maxMaxBufferLength:5,backBufferLength:2,startPosition:map.offset,fragLoadingTimeOut:30000,xhrSetup(xhr){if(!current()||!context.valid()){xhr.abort();return;}xhr.withCredentials=true;xhr.setRequestHeader("X-VMS-Context",context.headers["X-VMS-Context"]);}});
    hls=player;
    player.on(Hls.Events.ERROR,(_,data)=>{
     if(!current()||!data.fatal)return;
     fail("O trecho ficou indisponível ou não pôde ser reproduzido. Atualize a consulta.",data.type===Hls.ErrorTypes.MEDIA_ERROR);
    });
    player.on(Hls.Events.MANIFEST_PARSED,()=>{if(current())void video.current?.play().catch(()=>{if(current()){setPreparing(false);setError("Use o botão de reprodução para iniciar.");}});});
    video.current.onerror=()=>{if(current())fail("Não foi possível reproduzir a gravação.",[3,4].includes(video.current?.error?.code??0));};
    video.current.ontimeupdate=()=>{
     if(!current())return;
     const position=video.current?.currentTime??0;let match:Mapping|undefined;
     for(const m of map.mapping){if(position>=m.mediaStart)match=m;else break;}
     if(match){const at=new Date(new Date(match.start).getTime()+(position-match.mediaStart)*1000);lastInstant=at.toISOString();setInstant(at.toLocaleString());}
    };
    player.loadSource(opened.manifest);player.attachMedia(video.current);
   }catch(e){
    if(!current())return;
    if(e instanceof ApiError&&typeof e.details.nextStart==="string"){setNext(e.details.nextStart);setEnded(true);fail("Não há imagens no instante selecionado.");}
    else fail((e as Error).message);
   }
  }
  void open();
  return()=>{active=false;closeTransport();};
 },[tenant,camera.id,start,end]);
 return <Card ref={panel} className="panel archive-player gap-0 p-6"><div className="section-title"><h2>{camera.name}</h2><span>{instant||"Preparando consulta…"}</span></div><video ref={video} controls muted playsInline onPlaying={()=>{setPreparing(false);setError("");}} onEnded={()=>setEnded(true)} aria-label={`Gravação: ${camera.name}`}/>{preparing&&<p role="status" className="muted-text">Preparando vídeo para este navegador…</p>}{error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}{ended&&next?<Alert role="note" className="notice"><AlertDescription><p>O intervalo terminou. Não há imagens disponíveis até {new Date(next).toLocaleString()}.</p><Button onClick={()=>onNext(next)} variant="outline">Ir para o próximo intervalo</Button></AlertDescription></Alert>:ended&&<p>Fim do intervalo disponível.</p>}<p className="muted-text">O relógio acompanha os instantes originais. A reprodução para ao encontrar uma lacuna.</p></Card>;
}

export function PlaybackWorkspace({tenant}:{tenant:Tenant}){
 const base=`/tenants/${tenant.id}`;
 const [cameras,setCameras]=useState<Camera[]>([]),[cameraId,setCameraId]=useState(""),[from,setFrom]=useState(dateInput(new Date(Date.now()-3600_000))),[until,setUntil]=useState(dateInput(new Date()));
 const [segments,setSegments]=useState<Segment[]>([]),[error,setError]=useState(""),[busy,setBusy]=useState(false),[queried,setQueried]=useState(false),[selection,setSelection]=useState<{camera:Camera;start:string;end:string}|null>(null);
 useEffect(()=>{let active=true;const refresh=()=>void allPages<Camera>(base+"/recording-cameras").then(items=>{if(active){setCameras(items);setSelection(current=>current&&items.some(c=>c.id===current.camera.id)?current:null);setCameraId(current=>{if(current&&!items.some(c=>c.id===current)){setSegments([]);setQueried(false);return "";}return current;});}}).catch(e=>{if(active)setError(e.message);});refresh();const unsubscribe=onEnvironmentChange(["CameraStatusChanged"],refresh);return()=>{active=false;unsubscribe();};},[tenant.id]);
 async function search(e?:FormEvent){e?.preventDefault();setBusy(true);setError("");setSelection(null);try{const start=new Date(from).toISOString(),end=new Date(until).toISOString();const result=await api<{items:Segment[]}>(base+`/cameras/${cameraId}/timeline?start=${encodeURIComponent(start)}&end=${encodeURIComponent(end)}`,"GET",undefined,true);setSegments(result.items);setQueried(true);}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 function play(at:string){const camera=cameras.find(c=>c.id===cameraId);if(!camera)return;const end=new Date(Math.min(new Date(until).getTime(),new Date(at).getTime()+600_000)).toISOString();setSelection({camera,start:at,end});}
 const totalStart=new Date(from).getTime(),totalEnd=new Date(until).getTime(),span=totalEnd-totalStart;
 const columns:DataTableColumn<Segment>[]=[
  {id:"start",header:"Início",accessor:segment=>segment.start,filter:"date",cell:segment=>new Date(segment.start).toLocaleString()},
  {id:"end",header:"Fim",accessor:segment=>segment.end,filter:"date",cell:segment=>new Date(segment.end).toLocaleTimeString()},
  {id:"profile",header:"Perfil gravado",accessor:segment=>segment.profile,filter:"select"},
  {id:"actions",header:"Consulta",filter:false,sortable:false,hideable:false,cell:segment=><Button onClick={()=>play(segment.start)} variant="ghost">Reproduzir trecho</Button>}
 ];
 return <><div className="page-heading"><div><span className="eyebrow">ACERVO</span><h1>Consultar gravações</h1><p>Busque uma câmera e um horário. Cada consulta carrega apenas a janela selecionada.</p></div></div>{error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 <Card className="search-panel gap-0 p-6"><form className="archive-search" onSubmit={search}><Label className="field-label">Câmera<NativeSelect required value={cameraId} onChange={e=>{setCameraId(e.target.value);setSegments([]);setSelection(null);setQueried(false);}} className="w-full"><NativeSelectOption value="">Selecione uma câmera</NativeSelectOption>{cameras.map(c=><NativeSelectOption key={c.id} value={c.id}>{c.name}</NativeSelectOption>)}</NativeSelect></Label><Label className="field-label">De<Input type="datetime-local" step={1} required value={from} onChange={e=>setFrom(e.target.value)}/></Label><Label className="field-label">Até<Input type="datetime-local" step={1} required value={until} onChange={e=>setUntil(e.target.value)}/></Label><Button disabled={busy} variant="default">{busy?"Consultando…":"Buscar gravações"}</Button><small>Horário do navegador: {Intl.DateTimeFormat().resolvedOptions().timeZone}. Consulte até 24 horas por busca.</small></form></Card>
 {queried&&<Card className="panel timeline-panel gap-0 p-6"><div className="section-title"><h2>Disponibilidade</h2><span>{segments.length} segmentos disponíveis</span></div><div className="archive-timeline" aria-label="Linha do tempo das gravações">{segments.map(s=>{const left=Math.max(0,(new Date(s.start).getTime()-totalStart)/span*100),right=Math.min(100,(new Date(s.end).getTime()-totalStart)/span*100);return <Button key={s.id} title={new Date(s.start).toLocaleString()} aria-label={`Reproduzir ${new Date(s.start).toLocaleTimeString()}`} style={{left:`${left}%`,width:`${Math.max(.15,right-left)}%`}} onClick={()=>play(s.start)} variant="default"/>;})}</div><div className="timeline-labels"><span>{new Date(from).toLocaleTimeString()}</span><span>{new Date(until).toLocaleTimeString()}</span></div><p className="muted-text">Ciano: imagens disponíveis. Intervalos vazios: sem imagens disponíveis para sua conta.</p>{segments.length===0&&<p>Nenhuma gravação disponível nesse período.</p>}<Label className="field-label">Ir para um instante<Input type="datetime-local" step={1} defaultValue={from} onChange={e=>{if(e.target.value)play(new Date(e.target.value).toISOString());}}/></Label></Card>}
 {selection&&<ArchivePlayer tenant={tenant.id} {...selection} onNext={play}/>}
 {queried&&<DataTable key={`${tenant.id}:${cameraId}`} data={segments} columns={columns} getRowId={segment=>segment.id} label="Trechos de gravação" searchPlaceholder="Buscar perfis e trechos…" emptyMessage="Nenhuma gravação disponível nesse período."/>}
 </>;
}
