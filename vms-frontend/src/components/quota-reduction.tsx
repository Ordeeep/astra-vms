"use client";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import {useEffect,useRef,useState,type FormEvent} from "react";
import {api} from "@/lib/api";
import {EditorDialog} from "@/components/form-dialog";
type Reduction={id:string;targetBytes:number;status:string;code:string;result:{limit:number;usage:number;toFree:number;protected:boolean;applied:boolean}};
const labels:Record<string,string>={queued:"Aguardando",applying:"Liberando espaço",waiting:"Aguardando trechos em uso",completed:"Nova cota aplicada",failed:"Não concluída"};
export function QuotaReduction({tenant,revision,limit,onApplied}:{tenant:string;revision:number;limit:number;onApplied:()=>void}){
 const base=`/tenants/${tenant}`;
 const [open,setOpen]=useState(false),[preview,setPreview]=useState<Reduction|null>(null),[jobs,setJobs]=useState<Reduction[]>([]),[error,setError]=useState(""),[busy,setBusy]=useState(false);
 const key=useRef(""),seen=useRef(new Set<string>()),loaded=useRef(false),refresh=useRef(onApplied);
 const editorTrigger=useRef<HTMLElement|null>(null);
 refresh.current=onApplied;
 useEffect(()=>{let active=true,loading=false;const poll=async()=>{if(loading)return;loading=true;try{const value=await api<{items:Reduction[]}>(base+"/quota-jobs","GET",undefined,true);if(!active)return;setJobs(value.items);for(const j of value.items.filter(j=>j.status==="completed")){if(!seen.current.has(j.id)&&loaded.current)refresh.current();seen.current.add(j.id);}loaded.current=true;}catch(e){if(active)setError((e as Error).message);}finally{loading=false;}};void poll();const timer=setInterval(()=>void poll(),2000);return()=>{active=false;clearInterval(timer);};},[tenant]);
 async function prepare(e:FormEvent<HTMLFormElement>){e.preventDefault();setBusy(true);setError("");try{const f=new FormData(e.currentTarget);const p=await api<Reduction>(base+"/quota-previews","POST",{targetBytes:Math.round(Number(f.get("target"))*1024**3),revision},true);setPreview(p);key.current=crypto.randomUUID();}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 async function confirm(){if(!preview)return;setBusy(true);setError("");try{const j=await api<Reduction>(base+"/quota-jobs","POST",{previewId:preview.id,idempotencyKey:key.current},true);setJobs(current=>[j,...current.filter(v=>v.id!==j.id)]);setPreview(null);setOpen(false);}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 return <Card className="panel export-preview gap-0 p-6"><div className="section-title"><h2>Redução de cota</h2><Button onClick={()=>{editorTrigger.current=document.activeElement as HTMLElement;setOpen(true);setPreview(null);setError("");}} variant="outline">Preparar redução</Button></div><p>Ao reduzir, o histórico mais antigo pode ser descartado. A captura deste ambiente pausa durante a operação; consultas em andamento são preservadas.</p>
 {!open&&error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 {open&&<EditorDialog title="Redução de cota" busy={busy} returnFocus={editorTrigger.current} onClose={()=>{setOpen(false);setPreview(null);setError("");}}><p>Calcule o impacto antes de confirmar. O histórico mais antigo pode ser descartado; consultas em andamento são preservadas.</p>
 {error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 <form onSubmit={prepare} className="form-grid"><Label className="field-label">Nova cota de gravações (GiB)<Input name="target" type="number" min={0} max={limit/1024**3} step={0.001} required disabled={busy} onChange={()=>setPreview(null)} defaultValue={Math.max(0,Math.floor(limit/1024**3*500)/1000)}/></Label><Button disabled={busy} variant="outline">{busy?"Processando…":"Calcular impacto"}</Button></form>
 {preview&&<Alert role="note" className="notice"><AlertDescription><p>Serão liberados pelo menos <strong>{(preview.result.toFree/1024**2).toFixed(1)} MiB</strong> para aplicar a cota de {(preview.targetBytes/1024**3).toFixed(3)} GiB. O uso pode mudar até a confirmação. Arquivos descartados não podem ser recuperados pelo Astra.</p><Button disabled={busy} onClick={()=>void confirm()} variant="default">Confirmar descarte e redução</Button></AlertDescription></Alert>}
 <div className="form-actions"><Button type="button" disabled={busy} onClick={()=>{setOpen(false);setPreview(null);setError("");}} variant="ghost">Cancelar</Button></div></EditorDialog>}
 {jobs.slice(0,5).map(j=><div className="quota-progress" key={j.id}><strong>{labels[j.status]??j.status}</strong><span> · destino {(j.targetBytes/1024**3).toFixed(3)} GiB</span>{["applying","waiting"].includes(j.status)&&<small>Restam {(j.result.toFree/1024**2).toFixed(1)} MiB a liberar. A cota anterior continua vigente.</small>}{j.status==="failed"&&<small>Verifique as permissões e a política atual antes de solicitar outra redução.</small>}</div>)}
 </Card>;
}
