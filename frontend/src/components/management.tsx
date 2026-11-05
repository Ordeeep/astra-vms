"use client";
import {FormDialog} from "@/components/form-dialog";
import {PermissionGroupEditor,PermissionGroupSummary} from "@/components/permission-group-editor";
import {type Grant} from "@/lib/permission-grants";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { NativeSelectOption, NativeSelect } from "@/components/ui/native-select";
import { Checkbox } from "@/components/ui/checkbox";
import { FieldSet, FieldLegend } from "@/components/ui/field";

import {useEffect,useState,useRef,type FormEvent} from "react";
import {api,type Identity,type Operator,type Tenant} from "@/lib/api";
import {type CameraConnection,type ConnectionType} from "@/components/camera-connection-editor";
import {onEnvironmentChange} from "@/lib/events";
import {type CameraAvailability} from "@/lib/camera-availability";

export type Site={id:string;name:string;address:string;timezone:string;active:boolean;revision:number;capabilities?:Record<string,boolean>};
export type Profile={id:string;name:string;url?:string;codec?:string;width?:number;height?:number;fps?:number;enabled?:boolean};
export type Camera={id:string;name:string;siteId:string;active:boolean;effectiveActive:boolean;revision:number;sourceVersion:string;connectionType?:ConnectionType;connection?:CameraConnection;connectionRevision?:number;profiles:Profile[];capabilities:Record<string,boolean>;availability?:CameraAvailability};
type Group={id:string;name:string;grants:Grant[];revision:number};
type Member=Operator&{groups:string[];membershipRevision:number};
export async function allPages<T>(path:string):Promise<T[]> {
 const items:T[]=[];let cursor="";
 do {const page=await api<{items:T[];nextCursor?:string}>(path+(cursor?`?cursor=${encodeURIComponent(cursor)}`:""),"GET",undefined,true);items.push(...page.items);cursor=page.nextCursor??"";}while(cursor);
 return items;
}

export function Administration({identity,tenant,onRefresh,section="access"}:{identity:Identity;tenant:Tenant|null;onRefresh:()=>Promise<void>;section?:"access"|"operators"|"groups"|"tenants"}){
 const tenantID=tenant?.id??"",base=`/tenants/${tenantID}`,admin=identity.user.globalAdmin;
 const [groups,setGroups]=useState<Group[]>([]),[members,setMembers]=useState<Member[]>([]),[operators,setOperators]=useState<Operator[]>([]),[sites,setSites]=useState<Site[]>([]);
 const tab=section==="access"?"members":section;
 const globalSection=section==="operators"||section==="tenants";
 const available=globalSection?admin:!!tenant;
 const [error,setError]=useState(""),[busy,setBusy]=useState(false),[editor,setEditor]=useState<{type:string;value?:Tenant|Operator|Group|Member}|null>(null);
 const editorTrigger=useRef<HTMLElement|null>(null);
 const [permissionSummary,setPermissionSummary]=useState("");
 const [catalogLoading,setCatalogLoading]=useState(true),[catalogError,setCatalogError]=useState("");
 async function loadCatalog(){
  setCatalogLoading(true);setCatalogError("");
  try {setSites(await allPages<Site>(base+"/sites"));}
  catch(e){setCatalogError("Não foi possível carregar os sites. "+(e as Error).message);}
  finally{setCatalogLoading(false);}
 }
 const [grants,setGrants]=useState<Grant[]>([]),[assigned,setAssigned]=useState<string[]>([]),[target,setTarget]=useState("");
 async function load(){if(section==="tenants")return;try{
  if(admin&&section!=="groups")setOperators((await allPages<Operator>("/admin/operators")).sort((a,b)=>a.username.localeCompare(b.username)));
  if(tenant&&section!=="operators"){
   const [loadedGroups]=await Promise.all([allPages<Group>(base+"/groups"),section==="groups"?loadCatalog():Promise.resolve()]);
   setGroups(loadedGroups);
   if(section==="access")setMembers(await allPages<Member>(base+"/operators"));
  }
 }catch(e){setError((e as Error).message);}}
 useEffect(()=>{void load();return onEnvironmentChange([],()=>void load());},[tenantID,admin,section]);
 function edit(type:string,value?:Tenant|Operator|Group|Member){editorTrigger.current=document.activeElement as HTMLElement;setError("");setPermissionSummary("");setEditor({type,value});setGrants(type==="group"&&value?(value as Group).grants:[]);setAssigned(type==="member"&&value?(value as Member).groups:[]);setTarget(value?.id??"");}
 async function save(e:FormEvent<HTMLFormElement>){
  e.preventDefault();if(!editor || (editor.type==="group" && (catalogLoading || catalogError)))return;const f=new FormData(e.currentTarget),v=editor.value;setBusy(true);setError("");
  try{
   if(editor.type==="group")await api(base+"/groups"+(v?`/${v.id}`:""),v?"PUT":"POST",{name:f.get("name"),grants,...(v?{revision:v.revision}:{})},true);
   if(editor.type==="member"){const m=members.find(m=>m.id===target);await api(base+`/operators/${target}`,"PUT",{groups:assigned,revision:m?.membershipRevision??0},true);}
   if(editor.type==="operator")await api("/admin/operators"+(v?`/${v.id}`:""),v?"PATCH":"POST",v?{active:f.get("active")==="on",globalAdmin:f.get("global")==="on",revision:v.revision}:{username:f.get("username"),password:f.get("password"),globalAdmin:f.get("global")==="on"});
   if(editor.type==="tenant")await api("/admin/tenants"+(v?`/${v.id}`:""),v?"PATCH":"POST",{name:f.get("name"),...(v?{active:f.get("active")==="on",revision:v.revision}:{})});
   await load();setEditor(null);if(editor.type==="tenant")await onRefresh();
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }
 const groupColumns:DataTableColumn<Group>[]=[
  {id:"name",header:"Nome",accessor:g=>g.name,cell:g=><strong>{g.name}</strong>,hideable:false},
  {id:"grants",header:"Concessões",accessor:g=>g.grants.length,filter:"number",cell:g=>`${g.grants.length} concessões`},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:g=><Button data-group-edit={g.id} onClick={()=>edit("group",g)} variant="ghost">Editar</Button>},
 ];
 const memberGroups=(member:Member)=>{
  const names=member.groups.map(id=>groups.find(g=>g.id===id)?.name).filter((name):name is string=>!!name);
  return names.length?names:["Sem concessões"];
 };
 const memberColumns:DataTableColumn<Member>[]=[
  {id:"username",header:"Operador",accessor:m=>m.username,cell:m=><strong>{m.username}</strong>,hideable:false},
  {id:"groups",header:"Grupos",accessor:memberGroups,filter:"select",cell:m=>memberGroups(m).join(", ")},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:m=><Button onClick={()=>edit("member",m)} variant="ghost">Alterar grupos</Button>},
 ];
 const operatorColumns:DataTableColumn<Operator>[]=[
  {id:"username",header:"Operador",accessor:u=>u.username,cell:u=><strong>{u.username}</strong>,hideable:false},
  {id:"active",header:"Estado",accessor:u=>u.active?"Ativo":"Desativado",filter:"select"},
  {id:"administration",header:"Administração",accessor:u=>u.globalAdmin?"Administração global":"Sem administração global",filter:"select"},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:u=><Button onClick={()=>edit("operator",u)} variant="ghost">Editar</Button>},
 ];
 const tenantColumns:DataTableColumn<Tenant>[]=[
  {id:"name",header:"Ambiente",accessor:t=>t.name,cell:t=><strong>{t.name}</strong>,hideable:false},
  {id:"active",header:"Estado",accessor:t=>t.active?"Ativo":"Desativado",filter:"select"},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:t=><Button onClick={()=>edit("tenant",t)} variant="ghost">Editar</Button>},
 ];
 const title=section==="operators"?"Operadores":section==="groups"?"Grupos":section==="tenants"?"Ambientes":"Acessos";
 const description=section==="operators"?"Crie usuários e gerencie as contas de acesso à instalação.":section==="groups"?"Crie grupos de permissões para os operadores deste ambiente.":section==="tenants"?"Crie ambientes e gerencie seus nomes e estados de ativação.":"Gerencie os grupos e acessos dos operadores neste ambiente.";
 const content=<>
 {error&&!editor&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 {editor&&<FormDialog returnFocus={editorTrigger.current} fallbackFocus={editor.type==="group"?()=>document.querySelector<HTMLElement>(editor.value?`[data-group-edit="${CSS.escape(editor.value.id)}"]`:"[data-group-create]"):undefined} wide={editor.type==="group"} className={editor.type==="group"?"permission-group-dialog":""} saveLabel={editor.type==="group"?"Salvar grupo":"Salvar"} saveDisabled={editor.type==="group"&&(catalogLoading||!!catalogError)} footerNote={editor.type==="group"?<PermissionGroupSummary text={permissionSummary}/>:undefined} title={editor.type==="group"?(editor.value?"Editar grupo de permissões":"Novo grupo de permissões"):editor.type==="member"?"Grupos do operador":editor.type==="tenant"?"Ambiente":"Operador"} onClose={()=>{setEditor(null);setError("");}} onSubmit={save} busy={busy}>
  {error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
  {["group","tenant"].includes(editor.type)&&<Label className="field-label">Nome<Input name="name" required defaultValue={(editor.value as Tenant|Group)?.name??""}/></Label>}
  {editor.type==="operator"&&<>{!editor.value&&<><Label className="field-label">Usuário<Input name="username" required autoComplete="off"/></Label><Label className="field-label">Senha inicial<Input name="password" type="password" required autoComplete="new-password"/></Label></>}<Label className="check"><Checkbox name="global" defaultChecked={(editor.value as Operator)?.globalAdmin??false}/>Administrador da instalação</Label><Alert role="note" className="notice"><AlertDescription>Administrar a instalação não concede acesso a vídeos. Use os grupos de cada ambiente.</AlertDescription></Alert></>}
  {["operator","tenant"].includes(editor.type)&&editor.value&&<Label className="check"><Checkbox name="active" defaultChecked={(editor.value as Tenant).active}/>Ativo</Label>}
  {editor.type==="group"&&<PermissionGroupEditor tenantId={tenantID} sites={sites} grants={grants} onChange={setGrants} onSummaryChange={setPermissionSummary} loading={catalogLoading} error={catalogError} onRetry={()=>void loadCatalog()}/>}
  {editor.type==="member"&&<><Label className="field-label">Operador<NativeSelect required value={target} disabled={!!editor.value} onChange={e=>{setTarget(e.target.value);setAssigned(members.find(m=>m.id===e.target.value)?.groups??[]);}} className="w-full"><NativeSelectOption value="">Selecione</NativeSelectOption>{(admin?operators:members).map(u=><NativeSelectOption key={u.id} value={u.id}>{u.username}</NativeSelectOption>)}</NativeSelect></Label><FieldSet><FieldLegend>Grupos neste ambiente</FieldLegend>{groups.map(g=><Label className="check" key={g.id}><Checkbox checked={assigned.includes(g.id)} onCheckedChange={checked=>setAssigned((checked===true)?[...assigned,g.id]:assigned.filter(id=>id!==g.id))}/>{g.name}<small>{g.grants.length} concessões</small></Label>)}{!groups.length&&<p>Crie um grupo antes de conceder acesso a vídeos.</p>}</FieldSet></>}
 </FormDialog>}
 {tab==="groups"&&<DataTable key={`${tenantID}:groups`} data={groups} columns={groupColumns} getRowId={g=>g.id} label="Grupos de permissões" searchPlaceholder="Buscar grupos…" emptyMessage="Nenhum grupo de permissões cadastrado."/>}
 {tab==="members"&&<DataTable key={`${tenantID}:members`} data={members} columns={memberColumns} getRowId={m=>m.id} label="Acessos" searchPlaceholder="Buscar operadores ou grupos…" emptyMessage="Nenhum operador com acesso neste ambiente."/>}
 {tab==="operators"&&<DataTable key={`${tenantID}:operators`} data={operators} columns={operatorColumns} getRowId={u=>u.id} label="Operadores" searchPlaceholder="Buscar operadores…" emptyMessage="Nenhum operador cadastrado."/>}
 {tab==="tenants"&&<DataTable key={`${tenantID}:tenants`} data={identity.tenants} columns={tenantColumns} getRowId={t=>t.id} label="Ambientes" searchPlaceholder="Buscar ambientes…" emptyMessage="Nenhum ambiente disponível."/>}
 </>;
 return <><div className="page-heading"><div><span className="eyebrow">ADMINISTRAÇÃO</span><h1>{title}</h1><p>{description}</p></div>{available&&<Button data-group-create={section==="groups"?"":undefined} onClick={()=>edit(({groups:"group",members:"member",operators:"operator",tenants:"tenant"} as Record<string,string>)[tab])} variant="default">+ {({groups:"Novo grupo",members:"Conceder acesso",operators:"Novo operador",tenants:"Novo ambiente"} as Record<string,string>)[tab]}</Button>}</div>
 {!available?<p role="status">{globalSection?`Somente administradores da instalação podem gerenciar ${section==="tenants"?"ambientes":"operadores"}.`:"Selecione um ambiente ativo para continuar."}</p>:content}</>;
}
