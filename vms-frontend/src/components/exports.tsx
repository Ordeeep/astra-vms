"use client";
import { Progress } from "@/components/ui/progress";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import {useEffect,useRef,useState,type FormEvent} from "react";
import {api,mediaContext,type Tenant} from "@/lib/api";
import {allPages} from "@/components/management";
import {EditorDialog} from "@/components/form-dialog";
import {onEnvironmentChange} from "@/lib/events";
type Camera={id:string;name:string;capabilities:Record<string,boolean>};
type Preview={id:string;revision:number;start:string;end:string;gaps:{start:string;end:string}[];fps:number;width:number;height:number;estimatedBytes:number;frames:number;normalization:string};
type Job={id:string;requestedBy:string;status:string;code:string;createdAt:string;expires:string;result:{frames:number;total:number;bytes:number;sha256?:string}};
const states:Record<string,string>={queued:"Na fila",running:"Gerando arquivo",completed:"Disponível",failed:"Falhou",cancelled:"Cancelada",expired:"Expirada"};
const reasons:Record<string,string>={source_changed_repreview:"Uma origem ficou indisponível. Faça outra prévia.",source_or_encoding_failed:"A leitura ou o processamento do vídeo falhou. Faça outra prévia.",export_cancelled:"Geração interrompida.",requestor_access_revoked_or_expired:"A autorização ou o prazo do trabalho terminou.",export_budget_unavailable:"O orçamento de exportações não comporta o arquivo.",worker_job_lost:"O serviço de mídia reiniciou durante a geração.",cancelled_by_operator:"Cancelada pelo operador.",export_authority_expired:"A comunicação com o controle de acesso foi interrompida."};
const local=(at:Date)=>new Date(at.getTime()-at.getTimezoneOffset()*60000).toISOString().slice(0,19);
export function ExportsWorkspace({tenant}:{tenant:Tenant}){
 const base=`/tenants/${tenant.id}`;
 const [cameras,setCameras]=useState<Camera[]>([]),[camera,setCamera]=useState(""),[from,setFrom]=useState(local(new Date(Date.now()-300_000))),[until,setUntil]=useState(local(new Date()));
 const [preview,setPreview]=useState<Preview|null>(null),[jobs,setJobs]=useState<Job[]>([]),[error,setError]=useState(""),[message,setMessage]=useState(""),[busy,setBusy]=useState(false);
 const key=useRef(""),editorTrigger=useRef<HTMLElement|null>(null),editorVersion=useRef(0);
 const [editorOpen,setEditorOpen]=useState(false);
 function closeEditor(){editorVersion.current++;setEditorOpen(false);setPreview(null);setError("");}
 function openEditor(){editorVersion.current++;editorTrigger.current=document.activeElement as HTMLElement;setPreview(null);setError("");setMessage("");setFrom(local(new Date(Date.now()-300_000)));setUntil(local(new Date()));setEditorOpen(true);}
 useEffect(()=>{
  let active=true,loading=false;
  const refreshCameras=async()=>{
   try{
    const available=(await allPages<Camera>(base+"/recording-cameras")).filter(c=>c.capabilities.export);
    if(active){setCameras(available);setCamera(current=>available.some(c=>c.id===current)?current:"");}
   }catch(e){if(active){setCameras([]);setError((e as Error).message);}}
  };
  const poll=async()=>{if(loading)return;loading=true;try{const v=await allPages<Job>(base+"/exports");if(active)setJobs(v.sort((a,b)=>b.createdAt.localeCompare(a.createdAt)));}catch(e){if(active)setError((e as Error).message);}finally{loading=false;}};
  void refreshCameras();void poll();
  const timer=setInterval(()=>void poll(),2000);
  const unsubscribe=onEnvironmentChange(["ExportStateChanged"],()=>void poll());
  const unsubscribeCameras=onEnvironmentChange(["ArchiveChanged","CameraStatusChanged"],()=>void refreshCameras());
  return()=>{active=false;clearInterval(timer);unsubscribe();unsubscribeCameras();};
 },[tenant.id]);
 async function prepare(e:FormEvent){e.preventDefault();const version=editorVersion.current;setBusy(true);setError("");setMessage("");setPreview(null);try{const v=await api<Preview>(base+"/export-previews","POST",{cameraId:camera,start:new Date(from).toISOString(),end:new Date(until).toISOString(),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone},true);if(version===editorVersion.current){key.current=crypto.randomUUID();setPreview(v);}}catch(e){if(version===editorVersion.current)setError((e as Error).message);}finally{setBusy(false);}}
 async function generate(){if(!preview)return;setBusy(true);setError("");try{const job=await api<Job>(base+"/exports","POST",{previewId:preview.id,revision:preview.revision,idempotencyKey:key.current},true);setJobs(current=>[job,...current.filter(j=>j.id!==job.id)]);setPreview(null);setEditorOpen(false);setMessage("Exportação solicitada. Você pode acompanhar a geração nesta tela.");}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 async function cancel(id:string){setError("");try{await api(base+`/exports/${id}/cancel`,"POST",{},true);setJobs(items=>items.map(j=>j.id===id?{...j,status:"cancelled"}:j));}catch(e){setError((e as Error).message);}}
 const download=(id:string,kind:string)=>`/api/v1${base}/exports/${id}/${kind}?context=${encodeURIComponent(mediaContext().headers["X-VMS-Context"])}`;
 const columns:DataTableColumn<Job>[]=[
  {id:"createdAt",header:"Solicitação",accessor:job=>job.createdAt,filter:"date",cell:job=>new Date(job.createdAt).toLocaleString()},
  {id:"id",header:"Identificador",accessor:job=>job.id,cell:job=><code title={job.id}>{job.id.slice(0,8)}</code>},
  {id:"status",header:"Estado",accessor:job=>states[job.status]??job.status,filter:"select",cell:job=><><Badge className="badge" variant={job.status==="completed"?"outline":"secondary"}>{states[job.status]??job.status}</Badge>{job.status==="running"&&job.result.total>0&&<div className="mt-2 space-y-1"><Progress value={Math.min(100,100*job.result.frames/job.result.total)} aria-label="Quadros processados"/><small>{Math.min(100,Math.floor(100*job.result.frames/job.result.total))}% dos quadros processados</small></div>}{job.code&&<small>{reasons[job.code]??"O trabalho não pôde ser concluído."}</small>}</>},
  {id:"expires",header:"Disponível até",accessor:job=>job.expires,filter:"date",cell:job=>new Date(job.expires).toLocaleString()},
  {id:"actions",header:"Arquivos",filter:false,sortable:false,hideable:false,cell:job=><div className="row-actions">{job.status==="completed"&&new Date(job.expires).getTime()>Date.now()?<><Button asChild variant="outline"><a href={download(job.id,"content")}>Baixar MP4</a></Button><Button asChild variant="ghost"><a href={download(job.id,"report")}>Relatório JSON</a></Button></>:["queued","running"].includes(job.status)?<Button onClick={()=>void cancel(job.id)} variant="ghost">Cancelar</Button>:<span>—</span>}</div>}
 ];
 return <><div className="page-heading"><div><span className="eyebrow">ARQUIVOS DO AMBIENTE</span><h1>Exportar gravações</h1><p>Selecione o intervalo, confira a cobertura e gere o vídeo com seu relatório de origem.</p></div><Button onClick={openEditor} disabled={busy} variant="default">+ Nova exportação</Button></div>
 {error&&!editorOpen&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}{message&&<Alert role="status" className="success"><AlertDescription>{message}</AlertDescription></Alert>}
 {editorOpen&&<EditorDialog title="Nova exportação" onClose={closeEditor} returnFocus={editorTrigger.current} wide busy={busy}>
 {error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 <form onSubmit={prepare} aria-busy={busy}><fieldset className="dialog-form-fields" disabled={busy}><div className="form-grid"><Label className="field-label">Câmera para exportação<NativeSelect required value={camera} onChange={e=>{setCamera(e.target.value);setPreview(null);}} className="w-full"><NativeSelectOption value="">Selecione uma câmera</NativeSelectOption>{cameras.map(c=><NativeSelectOption key={c.id} value={c.id}>{c.name}</NativeSelectOption>)}</NativeSelect></Label><Label className="field-label">Início da exportação<Input type="datetime-local" required step={1} value={from} onChange={e=>{setFrom(e.target.value);setPreview(null);}}/></Label><Label className="field-label">Fim da exportação<Input type="datetime-local" required step={1} value={until} onChange={e=>{setUntil(e.target.value);setPreview(null);}}/></Label></div></fieldset><div className="form-actions"><Button disabled={busy} variant="default">{busy?"Preparando…":"Preparar prévia"}</Button><Button type="button" variant="ghost" onClick={closeEditor} disabled={busy}>Cancelar</Button></div><small>Horários em {Intl.DateTimeFormat().resolvedOptions().timeZone}. Até 24 horas por arquivo; o orçamento do ambiente limita o tamanho disponível.</small></form>
 {preview&&<Card className="panel export-preview gap-0 p-6"><div className="section-title"><h2>Confira antes de gerar</h2><Badge className="badge" variant="outline">{preview.width} × {preview.height} · {preview.fps} fps</Badge></div><p>{new Date(preview.start).toLocaleString()} até {new Date(preview.end).toLocaleString()}</p><p>{preview.normalization}. Duração aproximada: {(preview.frames/preview.fps).toFixed(1)} segundos. Estimativa de espaço: {(preview.estimatedBytes/1024**2).toFixed(1)} MiB; o tamanho final pode variar.</p>{preview.gaps.length>0?<Alert role="note" className="notice"><AlertDescription><strong>{preview.gaps.length} lacuna(s) serão preenchidas com tela preta.</strong><p>Esses quadros são gerados pelo Astra e não representam imagens capturadas. O relatório JSON identifica os intervalos.</p><ul>{preview.gaps.slice(0,30).map((gap,i)=><li key={i}>{new Date(gap.start).toLocaleString()} — {new Date(gap.end).toLocaleString()}</li>)}</ul>{preview.gaps.length>30&&<p>Os demais intervalos constarão no relatório.</p>}</AlertDescription></Alert>:<p>Há imagens em todo o intervalo selecionado.</p>}<Button disabled={busy} onClick={()=>void generate()} variant="default">Gerar MP4 e relatório</Button></Card>}
 </EditorDialog>}
 <DataTable key={tenant.id} data={jobs} columns={columns} getRowId={job=>job.id} label="Exportações" searchPlaceholder="Buscar exportações…" emptyMessage={<><h2>Nenhuma exportação disponível</h2><p>Somente arquivos cujas imagens você pode reproduzir e exportar aparecem aqui.</p></>}/>
 </>;
}
