"use client";
import {useEffect,useRef,useState,type FormEvent} from "react";
import {Alert,AlertDescription} from "@/components/ui/alert";
import {Label} from "@/components/ui/label";
import {Checkbox} from "@/components/ui/checkbox";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Card} from "@/components/ui/card";
import {api,ApiError,type Identity,type Tenant} from "@/lib/api";
import {FormDialog} from "@/components/form-dialog";
import {QuotaReduction} from "@/components/quota-reduction";
import {onEnvironmentChange} from "@/lib/events";
type Policy={quotaBytes:number;exportBytes:number;retentionDays:number;exportHours:number;revision:number};
const gib=1024**3;
export function StorageWorkspace({tenant,identity}:{tenant:Tenant|null;identity:Identity}){
 const base=tenant?`/tenants/${tenant.id}`:"";
 const [policy,setPolicy]=useState<Policy|null>(null),[usage,setUsage]=useState<Record<string,number>>({}),[caps,setCaps]=useState<Record<string,boolean>>({});
 const [storage,setStorage]=useState<{configured:boolean;config:{path:string;volumeLimit:number;margin:number}}|null>(null);
 const [policyOpen,setPolicyOpen]=useState(false),[storageOpen,setStorageOpen]=useState(false);
 const [error,setError]=useState(""),[notice,setNotice]=useState(""),[impact,setImpact]=useState<number|null>(null),[busy,setBusy]=useState(false);
 const [storageError,setStorageError]=useState("");
 const editorTrigger=useRef<HTMLElement|null>(null);
 async function load(){
  if(!tenant)return;
  try{
   const capabilities=await api<Record<string,boolean>>(base+"/capabilities","GET",undefined,true);setCaps(capabilities);
   if(capabilities["recording.manage"]){const result=await api<{policy:Policy;usage:Record<string,number>}>(base+"/recording-policy","GET",undefined,true);setPolicy(result.policy);setUsage(result.usage??{});}
   else setPolicy(null);
  }catch(e){setError((e as Error).message);}
 }
 useEffect(()=>{void load();return onEnvironmentChange(["AuthorizationChanged"],()=>void load());},[tenant?.id]);
 useEffect(()=>{let active=true;if(identity.user.globalAdmin)api<typeof storage>("/admin/storage").then(value=>{if(active)setStorage(value);}).catch(e=>{if(active)setStorageError(e.message);});return()=>{active=false;};},[identity.user.globalAdmin]);
 function failure(e:unknown){if(e instanceof ApiError&&e.code==="retention_impact_confirmation"){setImpact(Number(e.details.eligibleBytes));setError("");}else setError((e as Error).message);}
 async function savePolicy(e:FormEvent<HTMLFormElement>){e.preventDefault();if(!policy)return;const f=new FormData(e.currentTarget);setBusy(true);setError("");try{await api(base+"/recording-policy","PUT",{quotaBytes:Math.round(Number(f.get("quota"))*gib),exportBytes:Math.round(Number(f.get("exports"))*gib),retentionDays:Number(f.get("retention")),exportHours:Number(f.get("hours")),revision:policy.revision,confirmImpact:f.get("confirmImpact")==="on"},true);setPolicyOpen(false);setNotice("Política de armazenamento aplicada.");await load();}catch(e){failure(e);}finally{setBusy(false);}}
 async function saveStorage(e:FormEvent<HTMLFormElement>){e.preventDefault();const f=new FormData(e.currentTarget);setBusy(true);setError("");try{await api("/admin/storage","POST",{path:f.get("path"),volumeLimit:Math.round(Number(f.get("capacity"))*gib),margin:Math.round(Number(f.get("margin"))*1024**2)});setStorage(await api<typeof storage>("/admin/storage"));setStorageOpen(false);setNotice("Destino local configurado. Defina agora a cota deste ambiente.");await load();}catch(e){failure(e);}finally{setBusy(false);}}

 const dialogError=error&&<Alert role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>;
 const impactNotice=impact!==null&&<Alert role="note"><AlertDescription><p>A nova retenção torna {(impact/1024**2).toFixed(1)} MiB do acervo elegíveis para descarte.</p><Label className="check"><Checkbox name="confirmImpact" required/>Confirmo a aplicação ao histórico existente.</Label></AlertDescription></Alert>;
 return <>
 <div className="page-heading"><div><span className="eyebrow">SISTEMA</span><h1>Armazenamento</h1><p>Política do ambiente e destino das gravações no servidor.</p></div></div>
 {!policyOpen&&!storageOpen&&dialogError}{notice&&<Alert role="status"><AlertDescription>{notice}</AlertDescription></Alert>}
 <section className="storage-section" aria-labelledby="tenant-storage-title">
  <div className="section-title"><h2 id="tenant-storage-title">Política do ambiente atual{tenant?` · ${tenant.name}`:""}</h2>{policy&&caps["recording.manage"]&&<Button variant="outline" onClick={()=>{editorTrigger.current=document.activeElement as HTMLElement;setImpact(null);setError("");setPolicyOpen(true);}}>Cota e retenção</Button>}</div>
  {!tenant&&<p>Selecione um ambiente para consultar sua política.</p>}
  {tenant&&!caps["recording.manage"]&&<p>Você não tem permissão para gerenciar a política de armazenamento deste ambiente.</p>}
 {policy&&<div className="stats-grid"><Card className="panel metric gap-3 p-6"><span>Gravações do ambiente</span><strong>{((usage.recording??0)/gib).toFixed(3)} <small>/ {(policy.quotaBytes/gib).toFixed(2)} GiB</small></strong></Card><Card className="panel metric gap-3 p-6"><span>Retenção padrão</span><strong>{policy.retentionDays} <small>dias</small></strong></Card><Card className="panel metric gap-3 p-6"><span>Orçamento de exportação</span><strong>{(policy.exportBytes/gib).toFixed(2)} <small>GiB</small></strong></Card></div>}
 {policyOpen&&policy&&<FormDialog title="Política deste ambiente" returnFocus={editorTrigger.current} onClose={()=>{setPolicyOpen(false);setError("");setImpact(null);}} onSubmit={savePolicy} busy={busy}>{dialogError}<div className="form-grid"><Label className="field-label">Cota máxima de gravações (GiB)<Input name="quota" type="number" min={0} step={0.001} defaultValue={policy.quotaBytes/gib} required/></Label><Label className="field-label">Orçamento separado de exportações (GiB)<Input name="exports" type="number" min={0} step={0.001} defaultValue={policy.exportBytes/gib} required/></Label><Label className="field-label">Retenção padrão (dias)<Input name="retention" type="number" min={1} max={3650} defaultValue={policy.retentionDays} required/></Label><Label className="field-label">Limpar exportações após (horas)<Input name="hours" type="number" min={1} max={720} defaultValue={policy.exportHours} required/></Label></div><Alert role="note" className="notice"><AlertDescription>A cota limita o uso e não reserva espaço físico. O acervo mais antigo pode ser sobrescrito antes do prazo quando faltar capacidade.</AlertDescription></Alert>{impactNotice}</FormDialog>}

 {tenant&&policy&&caps["recording.manage"]&&<div inert={policyOpen||storageOpen||busy}><QuotaReduction tenant={tenant.id} revision={policy.revision} limit={policy.quotaBytes} onApplied={()=>void load()}/></div>}
 </section>
 {identity.user.globalAdmin&&<section className="storage-section" aria-labelledby="server-storage-title"><h2 id="server-storage-title">Armazenamento do servidor</h2><p>Destino e limites físicos compartilhados pelos ambientes desta instalação.</p>
 {storageError&&<Alert role="alert" variant="destructive"><AlertDescription>{storageError}</AlertDescription></Alert>}
 {storage&&!storage.configured&&<Alert role="note" className="notice"><AlertDescription><p>Configure o destino local para armazenar as gravações.</p><Button onClick={()=>{if(busy||policyOpen)return;editorTrigger.current=document.activeElement as HTMLElement;setError("");setStorageOpen(true);}} variant="outline">Configurar armazenamento</Button></AlertDescription></Alert>}
 {storageOpen&&storage&&!storage.configured&&<FormDialog title="Destino local das gravações" returnFocus={editorTrigger.current} onSubmit={saveStorage} onClose={()=>{setStorageOpen(false);setError("");}} busy={busy}>{dialogError}<p>Escolha uma pasta local no servidor: NTFS no Windows ou ext4 no Ubuntu. O orçamento é compartilhado pelos ambientes.</p><Label className="field-label">Pasta no servidor<Input name="path" required placeholder="/srv/astra/archive ou D:\Astra\Acervo"/></Label><div className="form-grid"><Label className="field-label">Orçamento do volume (GiB)<Input name="capacity" type="number" min={0.01} step={0.01} defaultValue={1} required/></Label><Label className="field-label">Espaço livre de segurança (MiB)<Input name="margin" type="number" min={0} defaultValue={256} required/></Label></div></FormDialog>}
 {storage?.configured&&<Alert role="note" className="notice"><AlertDescription>Destino local: <strong>{storage.config.path}</strong> · orçamento {(storage.config.volumeLimit/gib).toFixed(2)} GiB · margem livre {(storage.config.margin/1024**2).toFixed(0)} MiB.</AlertDescription></Alert>}

 </section>}
 </>;
}
