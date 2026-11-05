"use client";

import {useEffect,useRef,useState,type FormEvent} from "react";
import {MapPin} from "lucide-react";
import {FormDialog} from "@/components/form-dialog";
import {allPages,type Site} from "@/components/management";
import {Alert,AlertDescription} from "@/components/ui/alert";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Card} from "@/components/ui/card";
import {Checkbox} from "@/components/ui/checkbox";
import {Input} from "@/components/ui/input";
import {Label} from "@/components/ui/label";
import {Spinner} from "@/components/ui/spinner";
import {api,type Tenant} from "@/lib/api";
import {onEnvironmentChange} from "@/lib/events";

export function SitesWorkspace({tenant}:{tenant:Tenant}) {
 const base=`/tenants/${tenant.id}`;
 const [sites,setSites]=useState<Site[]>([]),[caps,setCaps]=useState<Record<string,boolean>>({});
 const [loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const [editor,setEditor]=useState<{site?:Site}|null>(null);
 const editorTrigger=useRef<HTMLElement|null>(null);

 async function load(){
  try{
   const [items,permissions]=await Promise.all([allPages<Site>(base+"/sites"),api<Record<string,boolean>>(base+"/capabilities","GET",undefined,true)]);
   setSites(items);setCaps(permissions);
  }catch(e){setError((e as Error).message);}finally{setLoading(false);}
 }
 useEffect(()=>{void load();return onEnvironmentChange([],()=>void load());},[tenant.id]);

 function open(site?:Site){
  editorTrigger.current=document.activeElement as HTMLElement;setError("");setEditor({site});
 }
 async function save(event:FormEvent<HTMLFormElement>){
  event.preventDefault();if(!editor)return;
  const values=new FormData(event.currentTarget),site=editor.site;
  setBusy(true);setError("");
  try{
   await api(base+"/sites"+(site?`/${site.id}`:""),site?"PATCH":"POST",{
    name:values.get("name"),address:values.get("address"),timezone:values.get("timezone"),
    ...(site?{active:values.get("active")==="on",revision:site.revision}:{})
   },true);
   setEditor(null);await load();
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }

 return <>
  <div className="page-heading"><div><h1>Sites</h1><p>Gerencie os locais físicos, endereços e fusos horários deste tenant.</p></div>{caps["inventory.manage"]&&<Button onClick={()=>open()}>+ Novo site</Button>}</div>
  {error&&!editor&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
  {editor&&<FormDialog returnFocus={editorTrigger.current} title={editor.site?"Editar site":"Novo site"} onClose={()=>{setEditor(null);setError("");}} onSubmit={save} busy={busy}>
   {error&&<Alert role="alert" className="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
   <Label className="field-label">Nome<Input name="name" required maxLength={200} defaultValue={editor.site?.name??""}/></Label>
   <Label className="field-label">Endereço<Input name="address" maxLength={2000} defaultValue={editor.site?.address??""}/></Label>
   <Label className="field-label">Fuso horário<Input name="timezone" list="timezones" required defaultValue={editor.site?.timezone??"America/Sao_Paulo"}/><datalist id="timezones">{["America/Sao_Paulo","America/Manaus","America/Recife","America/Fortaleza","America/Rio_Branco","UTC"].map(value=><option key={value} value={value}/>)}</datalist><small>A agenda utiliza o horário local do site.</small></Label>
   {editor.site&&<Label className="check"><Checkbox name="active" defaultChecked={editor.site.active}/>Cadastro ativo</Label>}
  </FormDialog>}
  {loading?<p className="flex items-center gap-2" role="status"><Spinner aria-hidden="true"/>Carregando sites…</p>:<div className="cards">
   {sites.map(site=><Card className="panel resource-card gap-0 p-6" key={site.id}>
    <div className="section-title"><MapPin className="resource-symbol size-6" aria-hidden="true"/><Badge className="badge" variant={site.active?"outline":"secondary"}>{site.active?"Ativo":"Desativado"}</Badge></div>
    <h2>{site.name}</h2><p>{site.address||"Sem endereço cadastrado"}</p><small>{site.timezone}</small>
    <div className="card-actions">{site.capabilities?.["inventory.manage"]&&<Button onClick={()=>open(site)} variant="outline">Editar site</Button>}</div>
   </Card>)}
   {!sites.length&&<Card className="panel empty gap-0 p-6"><h2>Nenhum site cadastrado</h2><p>Cadastre um local para organizar suas câmeras.</p></Card>}
  </div>}
 </>;
}
