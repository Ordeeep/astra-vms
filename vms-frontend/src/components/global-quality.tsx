"use client";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {useRef,useState,type FormEvent} from "react";
import {api} from "@/lib/api";
import {FormDialog} from "@/components/form-dialog";
type Rule={min:number;max:number;profile:string;fallback:string[]};
export function GlobalQuality({onSaved}:{onSaved:()=>Promise<void>}){
 const [rules,setRules]=useState<Rule[]|null>(null),[revision,setRevision]=useState(0),[error,setError]=useState(""),[busy,setBusy]=useState(false);
 const editorTrigger=useRef<HTMLElement|null>(null);
 const [opening,setOpening]=useState(false);
 async function open(){if(opening||busy)return;editorTrigger.current=document.activeElement as HTMLElement;setOpening(true);setError("");try{const p=await api<{rules:Rule[];revision:number}>("/admin/live-policy");setRules(p.rules);setRevision(p.revision);}catch(e){setError((e as Error).message);}finally{setOpening(false);}}
 async function save(e:FormEvent){e.preventDefault();setBusy(true);setError("");try{await api("/admin/live-policy","PUT",{rules,revision});setRules(null);await onSaved();}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 return <section className="section-spaced">{error&&!rules&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}<Button onClick={()=>void open()} disabled={opening} variant="ghost">{opening?"Carregando qualidade…":"Configurar qualidade padrão da instalação"}</Button>{rules&&<FormDialog title="Qualidade padrão da instalação" returnFocus={editorTrigger.current} wide onClose={()=>{setRules(null);setError("");}} onSubmit={save} busy={busy}>{error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}<p>Aplicada aos ambientes que herdam o padrão. Os perfis devem existir nas câmeras ou ter uma alternativa configurada.</p>{rules.map((r,i)=><div className="grant-row" key={i}><Label className="field-label">De<Input type="number" min={1} max={16} value={r.min} onChange={e=>setRules(rules.map((v,n)=>n===i?{...v,min:Number(e.target.value)}:v))}/></Label><Label className="field-label">Até<Input type="number" min={1} max={16} value={r.max} onChange={e=>setRules(rules.map((v,n)=>n===i?{...v,max:Number(e.target.value)}:v))}/></Label><Label className="field-label">Perfil<Input value={r.profile} onChange={e=>setRules(rules.map((v,n)=>n===i?{...v,profile:e.target.value}:v))}/></Label><Label className="field-label">Alternativas em ordem<Input value={r.fallback.join(",")} onChange={e=>setRules(rules.map((v,n)=>n===i?{...v,fallback:e.target.value.split(",").map(s=>s.trim()).filter(Boolean)}:v))}/></Label><Button type="button" onClick={()=>setRules(rules.filter((_,n)=>n!==i))} variant="ghost">Remover faixa</Button></div>)}<Button type="button" onClick={()=>setRules([...rules,{min:1,max:16,profile:"main",fallback:[]}])} variant="outline">Adicionar faixa</Button><small>As faixas precisam cobrir 1 a 16 posições, em ordem e sem sobreposição.</small></FormDialog>}</section>;
}
