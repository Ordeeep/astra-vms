import React from "react";
import {createRoot} from "react-dom/client";
import {Administration} from "@/components/management";
import {setContext,type Identity,type Tenant} from "@/lib/api";
const mode=new URL(location.href).searchParams.get("mode");
const tenant:Tenant={id:"tenant",name:"Ambiente de teste",active:true,revision:1};
const user={id:"admin",username:"admin",name:"Admin",email:"",globalAdmin:!mode?.startsWith("delegated"),active:true,revision:1};
const identity:Identity={user,tenants:[tenant],csrfToken:"test"};
let operators=[{...user,id:"operator",username:"joao",name:"João",globalAdmin:false,installationGroups:[] as string[]}];
let groups=[{id:"view",name:"Visualização",grants:[] as {action:string;scope:string;resourceId:string}[],revision:1},{id:"manage",name:"Gestão",grants:[],revision:1}];
let installation=[{id:"installation-administrators",name:"Administradores da instalação",grants:[{action:"installation.manage",scope:"installation",resourceId:"installation"}],revision:1}];
let assigned=["view"];
const writes:unknown[]=[];
const reads:string[]=[];
Object.assign(window,{writes,reads});
const json=(value:unknown,status=200)=>new Response(status===204?null:JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
window.fetch=async(input,options)=>{
 const path=String(input).replace('/api/v1','');const method=options?.method??'GET';
 const body=options?.body?JSON.parse(String(options.body)):undefined;
 if(method==='GET'){
  reads.push(path);
  if(path.endsWith('/sites'))return mode==='groups-catalog-error'?json({code:'database_unavailable'},503):json({items:[{id:'site',name:'Site de teste'}]});
  if(path==='/admin/installation-groups')return json({items:installation});
  if(path==='/admin/operators')return json({items:operators});
  if(path.endsWith('/groups'))return mode==='failed'?json({code:'database_unavailable'},503):json({items:groups});
  if(path.endsWith('/operators'))return json({items:operators.map(u=>({...u,groups:assigned,membershipRevision:3}))});
 }
 writes.push({path,method,body});
 if(path.startsWith('/admin/installation-groups')){const group={...body,id:method==='POST'?'new-installation':path.split('/').at(-1),revision:2};installation=method==='POST'?[...installation,group]:installation.map(g=>g.id===group.id?group:g);return json(group,method==='POST'?201:200);}
 if(path.startsWith('/tenants/tenant/groups')){const group={...body,id:method==='POST'?'new-tenant-'+writes.length:path.split('/').at(-1),revision:method==='POST'?1:body.revision+1};groups=method==='POST'?[...groups,group]:groups.map(g=>g.id===group.id?group:g);return json(group,method==='POST'?201:200);}
 if(method==='DELETE'){operators=operators.filter(u=>!path.endsWith('/'+u.id));return json(null,204);}
 if(mode==='conflict')return json({code:'revision_conflict'},412);
 if(body?.installationGroups)operators=operators.map(u=>path.endsWith('/'+u.id)?{...u,installationGroups:body.installationGroups}:u);
 if(body?.membership)assigned=body.membership.groups;
 if(method==='PUT')assigned=body.groups;
 if(method==='POST'){operators=[...operators,{...user,...body,id:'created'}];return json(operators.at(-1),201);}
 return json(null,204);
};
setContext('test');
createRoot(document.getElementById('root')!).render(<main className="astra-interface" style={{padding:20,fontFamily:"Arial, sans-serif"}}><Administration section={mode?.includes('groups')?'groups':'operators'} identity={identity} tenant={mode?.includes('no-tenant')?null:tenant} onRefresh={async()=>{}}/></main>);
