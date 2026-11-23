"use client";
import "./operator-dialog.css";
import {ConfirmationDialog,FormDialog} from "@/components/form-dialog";
import {PermissionGroupEditor,PermissionGroupSummary} from "@/components/permission-group-editor";
import {administersInstallation,permissionActions,type Grant} from "@/lib/permission-grants";
import {DataTable,type DataTableColumn} from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { AlertDescription, Alert } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import {NativeSelect,NativeSelectOption} from "@/components/ui/native-select";
import { Checkbox } from "@/components/ui/checkbox";
import { FieldSet, FieldLegend } from "@/components/ui/field";

import {useEffect,useState,useRef,type FormEvent} from "react";
import {api,type Identity,type Operator,type Tenant} from "@/lib/api";
import {type CameraConnection,type ConnectionType} from "@/components/camera-connection-editor";
import {onEnvironmentChange} from "@/lib/events";
import {type CameraAvailability} from "@/lib/camera-availability";

export type Site={id:string;name:string;address:string;timezone:string;active:boolean;revision:number;capabilities?:Record<string,boolean>};
export type Profile={id:string;name:string;url?:string;codec?:string;width?:number;height?:number;fps?:number;enabled?:boolean};
export type CameraDisplayMode="cover"|"4:3"|"contain";
export type Camera={id:string;name:string;siteId:string;active:boolean;displayMode?:CameraDisplayMode;effectiveActive:boolean;revision:number;sourceVersion:string;connectionType?:ConnectionType;connection?:CameraConnection;connectionRevision?:number;profiles:Profile[];capabilities:Record<string,boolean>;availability?:CameraAvailability};
type Group={id:string;name:string;grants:Grant[];revision:number};
type ScopedGroup=Group&{scope:"installation"|"tenant"};
type Member=Operator&{groups:string[];membershipRevision:number};
export async function allPages<T>(path:string):Promise<T[]> {
 const items:T[]=[];let cursor="";
 do {const page=await api<{items:T[];nextCursor?:string}>(path+(cursor?`?cursor=${encodeURIComponent(cursor)}`:""),"GET",undefined,true);items.push(...page.items);cursor=page.nextCursor??"";}while(cursor);
 return items;
}

function validateOperatorPasswords(form:HTMLFormElement){
 const password=form.elements.namedItem("password") as HTMLInputElement|null;
 const confirmation=form.elements.namedItem("confirmPassword") as HTMLInputElement|null;
 if(password&&confirmation)confirmation.setCustomValidity(password.value!==confirmation.value?"As senhas não coincidem.":"");
}

export function Administration({identity,tenant,onRefresh,section="operators"}:{identity:Identity;tenant:Tenant|null;onRefresh:()=>Promise<void>;section?:"operators"|"groups"|"tenants"}){
 const tenantID=tenant?.id??"",base=`/tenants/${tenantID}`,admin=identity.user.globalAdmin;
 const [groups,setGroups]=useState<Group[]>([]),[members,setMembers]=useState<Member[]>([]),[operators,setOperators]=useState<Operator[]>([]),[sites,setSites]=useState<Site[]>([]);
 const tab=section;
 const [groupScope,setGroupScope]=useState<ScopedGroup["scope"]>("tenant");
 const installationScope=groupScope==="installation";
 const groupBase=installationScope?"/admin/installation-groups":base+"/groups";
 const [installationGroups,setInstallationGroups]=useState<Group[]>([]),[assignedInstallation,setAssignedInstallation]=useState<string[]>([]);
 const globalSection=section==="tenants";
 const available=globalSection?admin:admin||!!tenant;
 const [error,setError]=useState(""),[busy,setBusy]=useState(false),[editor,setEditor]=useState<{type:string;value?:Tenant|Operator|Group|Member}|null>(null);
 const [operatorToDelete,setOperatorToDelete]=useState<Operator|null>(null);
 const editorTrigger=useRef<HTMLElement|null>(null);
 const heading=useRef<HTMLHeadingElement>(null);
 const [permissionSummary,setPermissionSummary]=useState("");
 const [catalogLoading,setCatalogLoading]=useState(true),[catalogError,setCatalogError]=useState("");
 async function loadCatalog(){
  setCatalogLoading(true);setCatalogError("");
  try {setSites(await allPages<Site>(base+"/sites"));}
  catch(e){setCatalogError("Não foi possível carregar os sites. "+(e as Error).message);}
  finally{setCatalogLoading(false);}
 }
 const [grants,setGrants]=useState<Grant[]>([]),[assigned,setAssigned]=useState<string[]>([]),[membershipRevision,setMembershipRevision]=useState(0),[initialAssigned,setInitialAssigned]=useState<string[]>([]);
 const [accessReady,setAccessReady]=useState(false);
 async function load(){setAccessReady(false);if(section==="tenants")return;try{
  if(admin){
   const [loadedOperators,loadedInstallation]=await Promise.all([section==="operators"?allPages<Operator>("/admin/operators"):Promise.resolve([]),allPages<Group>("/admin/installation-groups")]);
   setOperators(loadedOperators.sort((a,b)=>a.username.localeCompare(b.username)));setInstallationGroups(loadedInstallation);
  }
  if(tenant){
   const [loadedGroups,loadedMembers]=await Promise.all([allPages<Group>(base+"/groups"),section==="operators"?allPages<Member>(base+"/operators"):Promise.resolve([]),section==="groups"?loadCatalog():Promise.resolve()]);
   setGroups(loadedGroups);setMembers(loadedMembers);if(!admin&&section==="operators")setOperators(loadedMembers);
  }
  setAccessReady(true);
 }catch(e){setError((e as Error).message);}}
 useEffect(()=>{void load();return onEnvironmentChange([],()=>void load());},[tenantID,admin,section]);
 function edit(type:string,value?:Tenant|Operator|Group|Member){if(type==="operator"&&!accessReady)return;if(type==="group")setGroupScope((value as ScopedGroup|undefined)?.scope??(tenant?"tenant":"installation"));editorTrigger.current=document.activeElement as HTMLElement;setError("");setPermissionSummary("");setEditor({type,value});setGrants(type==="group"&&value?(value as Group).grants:[]);const member=members.find(m=>m.id===value?.id);setAssigned(member?.groups??[]);setInitialAssigned(member?.groups??[]);setMembershipRevision(member?.membershipRevision??0);setAssignedInstallation((value as Operator|undefined)?.installationGroups??[]);}
 async function save(e:FormEvent<HTMLFormElement>){
  e.preventDefault();if(!editor || (editor.type==="group" && !installationScope && !administersInstallation(grants) && (catalogLoading || catalogError)))return;
  if(editor.type==="operator"&&!accessReady)return;
  if(editor.type==="group"&&(installationScope?!admin:!tenant))return;
  if(editor.type==="operator"&&admin){validateOperatorPasswords(e.currentTarget);if(!e.currentTarget.reportValidity())return;}
  const f=new FormData(e.currentTarget),v=editor.value;setBusy(true);setError("");
  try{
   if(editor.type==="group")await api(groupBase+(v?`/${v.id}`:""),v?"PUT":"POST",{name:f.get("name"),grants,...(v?{revision:v.revision}:{})},!installationScope);
   if(editor.type==="operator"){
    const membership=tenant&&(assigned.length!==initialAssigned.length||assigned.some(id=>!initialAssigned.includes(id)))?{tenantId:tenantID,groups:assigned,revision:membershipRevision}:undefined;
    if(admin)await api("/admin/operators"+(v?`/${v.id}`:""),v?"PATCH":"POST",{username:f.get("username"),name:f.get("name"),email:f.get("email"),installationGroups:assignedInstallation,...(f.get("password")?{password:f.get("password")}:{}),...(v?{active:f.get("active")==="on",revision:v.revision}:{}),membership});
    else if(v&&membership)await api(base+`/operators/${v.id}`,"PUT",{groups:assigned,revision:membershipRevision},true);
   }
   if(editor.type==="tenant")await api("/admin/tenants"+(v?`/${v.id}`:""),v?"PATCH":"POST",{name:f.get("name"),...(v?{active:f.get("active")==="on",revision:v.revision}:{})});
   await load();setEditor(null);if(editor.type==="group"||editor.type==="tenant"||(editor.type==="operator"&&v?.id===identity.user.id))await onRefresh();
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }
 async function deleteOperator(operator:Operator){
  if(busy||!admin||operator.id===identity.user.id)return;
  setBusy(true);setError("");
  try{await api(`/admin/operators/${operator.id}`,"DELETE",{revision:operator.revision});setOperatorToDelete(null);setEditor(null);await load();}
  catch(e){setError((e as Error).message);}finally{setBusy(false);}
 }
 const listedGroups:ScopedGroup[]=[...groups.map(g=>({...g,scope:"tenant" as const})),...(admin?installationGroups.map(g=>({...g,scope:"installation" as const})):[])];
 const groupKey=(g:ScopedGroup)=>`${g.scope}:${g.id}`;
 const groupColumns:DataTableColumn<ScopedGroup>[]=[
  {id:"name",header:"Nome",accessor:g=>g.name,cell:g=><strong>{g.name}</strong>,hideable:false},
  {id:"scope",header:"Escopo",accessor:g=>g.scope==="installation"||administersInstallation(g.grants)?"Instalação":`Ambiente: ${tenant?.name??""}`,filter:"select"},
  {id:"grants",header:"Concessões",accessor:g=>administersInstallation(g.grants)?permissionActions.length+1:g.grants.length,filter:"number",cell:g=>administersInstallation(g.grants)?"Acesso completo":`${g.grants.length} concessões`},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:g=><Button data-group-edit={groupKey(g)} onClick={()=>edit("group",g)} variant="ghost">Editar</Button>},
 ];
 const memberGroups=(member:Member)=>{
  const names=member.groups.map(id=>groups.find(g=>g.id===id)?.name).filter((name):name is string=>!!name);
  return names.length?names:["Sem concessões"];
 };
 const operatorGroups=(u:Operator)=>memberGroups(members.find(m=>m.id===u.id)??{...u,groups:[],membershipRevision:0});
 const operatorColumns:DataTableColumn<Operator>[]=[
  {id:"username",header:"Operador",accessor:u=>u.username,cell:u=><strong>{u.username}</strong>,hideable:false},
  ...(tenant?[{id:"groups",header:"Grupos neste ambiente",accessor:operatorGroups,cell:(u:Operator)=>operatorGroups(u).join(", "),filter:"select" as const}]:[]),
  {id:"active",header:"Estado",accessor:u=>u.active?"Ativo":"Desativado",filter:"select"},
  ...(admin?[{id:"installationGroups",header:"Grupos da instalação",accessor:(u:Operator)=>(u.installationGroups??[]).map(id=>installationGroups.find(g=>g.id===id)?.name??id),cell:(u:Operator)=>(u.installationGroups??[]).map(id=>installationGroups.find(g=>g.id===id)?.name??id).join(", ")||"Sem grupos",filter:"select" as const}]:[]),
 ];
 const tenantColumns:DataTableColumn<Tenant>[]=[
  {id:"name",header:"Ambiente",accessor:t=>t.name,cell:t=><strong>{t.name}</strong>,hideable:false},
  {id:"active",header:"Estado",accessor:t=>t.active?"Ativo":"Desativado",filter:"select"},
  {id:"actions",header:"Ações",filter:false,sortable:false,hideable:false,cell:t=><Button onClick={()=>edit("tenant",t)} variant="ghost">Editar</Button>},
 ];
 const title=section==="operators"?"Operadores":section==="groups"?"Grupos de permissões":"Ambientes";
 const description=section==="operators"?"Gerencie os operadores e seus grupos de permissões em cada ambiente.":section==="groups"?(admin?"Gerencie os grupos da instalação e do ambiente selecionado. Vincule os grupos no cadastro do operador.":"Gerencie os grupos do ambiente selecionado. Vincule os grupos no cadastro do operador."):"Crie ambientes e gerencie seus nomes e estados de ativação.";
 const content=<>
 {error&&!editor&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 {editor&&<FormDialog footerActions={editor.type==="operator"&&admin&&editor.value&&editor.value.id!==identity.user.id?<Button type="button" variant="destructive" disabled={busy} onClick={()=>{setError("");setOperatorToDelete(editor.value as Operator);}}>Excluir</Button>:undefined} returnFocus={editorTrigger.current} fallbackFocus={editor.type==="group"?()=>document.querySelector<HTMLElement>(editor.value?`[data-group-edit="${CSS.escape(groupKey({...editor.value,scope:groupScope} as ScopedGroup))}"]`:"[data-group-create]"):()=>heading.current} wide={editor.type==="group"||editor.type==="operator"&&admin} className={editor.type==="group"?"permission-group-dialog":editor.type==="operator"?"operator-dialog":""} saveLabel={editor.type==="group"?"Salvar grupo":"Salvar"} saveDisabled={editor.type==="group"?(!installationScope&&!administersInstallation(grants)&&(catalogLoading||!!catalogError)):editor.type==="operator"&&!accessReady} footerNote={editor.type==="group"?<PermissionGroupSummary text={permissionSummary}/>:undefined} title={editor.type==="group"?(editor.value?"Editar grupo de permissões":"Novo grupo de permissões"):editor.type==="tenant"?"Ambiente":"Operador"} onClose={()=>{setEditor(null);setError("");}} onSubmit={save} busy={busy}>
  {error&&!operatorToDelete&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
  {["group","tenant"].includes(editor.type)&&<Label className="field-label">Nome<Input name="name" required defaultValue={(editor.value as Tenant|Group)?.name??""}/></Label>}
  {editor.type==="tenant"&&editor.value&&<Label className="check"><Checkbox name="active" defaultChecked={(editor.value as Tenant).active}/>Ativo</Label>}
  {editor.type==="group"&&<Label className="field-label">Escopo<NativeSelect aria-label="Escopo" value={groupScope} disabled>
   {!installationScope&&tenant&&<NativeSelectOption value="tenant">{administersInstallation(grants)?"Instalação: todos os ambientes":`Ambiente: ${tenant.name}`}</NativeSelectOption>}
   {installationScope&&<NativeSelectOption value="installation">Instalação</NativeSelectOption>}
  </NativeSelect></Label>}
  {editor.type==="group"&&<PermissionGroupEditor tenantId={tenantID} sites={sites} grants={grants} onChange={setGrants} onSummaryChange={setPermissionSummary} canAdministerInstallation={admin} loading={!!tenant&&catalogLoading} error={tenant?catalogError:undefined} onRetry={()=>void loadCatalog()}/>}
  {editor.type==="operator"&&<div className={admin?"operator-editor-layout":undefined}><div className="operator-account-fields">
  {editor.type==="operator"&&admin&&<>
   <Label className="field-label">Usuário<Input name="username" placeholder="Ex.: joao.silva" required maxLength={128} autoComplete="off" defaultValue={(editor.value as Operator)?.username??""}/></Label>
   <Label className="field-label">Nome<Input name="name" placeholder="Nome completo" required maxLength={128} autoComplete="off" defaultValue={(editor.value as Operator)?.name??""}/></Label>
   <Label className="field-label">E-mail<Input name="email" placeholder="Ex.: joao@empresa.com.br" type="email" maxLength={254} autoComplete="off" defaultValue={(editor.value as Operator)?.email??""}/></Label>
   <Label className="field-label">{editor.value?"Nova senha":"Senha inicial"}<Input name="password" placeholder={editor.value?"Deixe em branco para manter a senha":"Crie uma senha"} type="password" required={!editor.value} maxLength={1024} autoComplete="new-password" aria-describedby={editor.value?"operator-password-hint":undefined} onInput={event=>validateOperatorPasswords(event.currentTarget.form!)}/></Label>
   <Label className="field-label">Confirmar senha<Input name="confirmPassword" placeholder="Repita a senha" type="password" required={!editor.value} maxLength={1024} autoComplete="new-password" onInput={event=>validateOperatorPasswords(event.currentTarget.form!)}/></Label>
   {editor.value&&<p id="operator-password-hint" className="text-sm text-muted-foreground">Deixe os campos de senha em branco para manter a senha atual.</p>}
  </>}
  {editor.type==="operator"&&admin&&editor.value&&<Label className="check"><Checkbox name="active" defaultChecked={(editor.value as Tenant).active}/>Ativo</Label>}
  {editor.type==="operator"&&!admin&&<p>Operador: {(editor.value as Operator)?.username}</p>}
  </div>
  <div className="operator-permission-groups">
  {admin&&<FieldSet className="operator-groups"><FieldLegend>Grupos da instalação</FieldLegend>
   {installationGroups.map(g=><Label className="check" key={g.id}><Checkbox checked={assignedInstallation.includes(g.id)} onCheckedChange={checked=>setAssignedInstallation(checked===true?[...assignedInstallation,g.id]:assignedInstallation.filter(id=>id!==g.id))}/>{g.name}</Label>)}
   {!installationGroups.length&&<p>Nenhum grupo da instalação cadastrado.</p>}
  </FieldSet>}
  <FieldSet className="operator-groups"><FieldLegend>Grupos do ambiente</FieldLegend>
   {tenant?<>
    {!accessReady?<p role="status">Os grupos não estão disponíveis. <Button type="button" variant="outline" onClick={()=>void load()}>Recarregar</Button></p>:<>
     {groups.map(g=><Label className="check" key={g.id}><Checkbox checked={assigned.includes(g.id)} onCheckedChange={checked=>setAssigned(checked===true?[...assigned,g.id]:assigned.filter(id=>id!==g.id))}/>{g.name}<small>{administersInstallation(g.grants)?"Acesso completo":`${g.grants.length} concessões`}</small></Label>)}
     {!groups.length&&<p>Crie um grupo em Grupos de permissões com escopo neste ambiente para conceder acesso.</p>}
    </>}
   </>:<p>Selecione um ambiente para gerenciar os grupos deste operador.</p>}
  </FieldSet>
  </div>
  </div>}
 </FormDialog>}
 {operatorToDelete&&<ConfirmationDialog title="Excluir operador" fallbackFocus={()=>heading.current}
  description={`As sessões e os vínculos de “${operatorToDelete.username}” serão removidos de todos os ambientes. O histórico será preservado.`}
  busy={busy} confirmLabel={busy?"Excluindo…":"Excluir operador"} onCancel={()=>{setOperatorToDelete(null);setError("");}} onConfirm={()=>void deleteOperator(operatorToDelete)}>
  {error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 </ConfirmationDialog>}
 {tab==="groups"&&<DataTable key={`${tenantID}:groups`} data={listedGroups} columns={groupColumns} getRowId={groupKey} label="Grupos de permissões" searchPlaceholder="Buscar grupos…" emptyMessage="Nenhum grupo de permissões cadastrado."/>}
 {tab==="operators"&&<DataTable key={`${tenantID}:operators`} data={operators} columns={operatorColumns} getRowId={u=>u.id} onRowClick={u=>edit("operator",u)} rowLabel={u=>`Editar operador ${u.username}`} label="Operadores" searchPlaceholder="Buscar operadores…" emptyMessage="Nenhum operador cadastrado."/>}
 {tab==="tenants"&&<DataTable key={`${tenantID}:tenants`} data={identity.tenants} columns={tenantColumns} getRowId={t=>t.id} label="Ambientes" searchPlaceholder="Buscar ambientes…" emptyMessage="Nenhum ambiente disponível."/>}
 </>;
 return <><div className="page-heading"><div><h1 ref={heading} tabIndex={-1}>{title}</h1><p>{description}</p></div>{available&&(section!=="operators"||admin)&&<Button data-group-create={section==="groups"?"":undefined} onClick={()=>edit(({groups:"group",operators:"operator",tenants:"tenant"} as Record<string,string>)[tab])} variant="default">+ {({groups:"Novo grupo",operators:"Novo operador",tenants:"Novo ambiente"} as Record<string,string>)[tab]}</Button>}</div>
 {!available?<p role="status">{globalSection?"Somente administradores da instalação podem gerenciar ambientes.":"Selecione um ambiente ativo para continuar."}</p>:content}</>;
}
