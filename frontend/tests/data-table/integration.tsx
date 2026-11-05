import React,{useEffect,useState} from "react";
import {CamerasWorkspace} from "@/components/cameras";
import {SitesWorkspace} from "@/components/sites";
import {Administration} from "@/components/management";
import {ExportsWorkspace} from "@/components/exports";
import {RecordingWorkspace} from "@/components/recording";
import {PlaybackWorkspace} from "@/components/playback";
import {OperationsWorkspace} from "@/components/operations";
import {setContext,type Identity,type Tenant} from "@/lib/api";

const tenant:Tenant={id:"synthetic-tenant",name:"Empresa sintética",active:true,revision:1};
const identity:Identity={csrfToken:"synthetic",tenants:[tenant],user:{id:"synthetic-operator",username:"operador.sintetico",name:"Operador sintético",email:"synthetic@example.invalid",globalAdmin:true,active:true,revision:1}};
const caps={"inventory.manage":true,"recording.manage":true,"permissions.manage":true,"audit.view":true,playback:true,export:true,live:true};
const sites=Array.from({length:23},(_,index)=>({id:`site-${index}`,name:`Site ${String(index+1).padStart(2,"0")}`,address:`Endereço sintético ${index+1}`,timezone:"America/Sao_Paulo",active:index%4!==0,revision:1,capabilities:caps}));
const cameras=Array.from({length:37},(_,index)=>({id:`camera-${index}`,name:`Câmera ${String(index+1).padStart(2,"0")}`,siteId:sites[index%sites.length].id,active:true,effectiveActive:index%3!==0,revision:1,sourceVersion:"synthetic-source",connectionType:index%2?"rtsp":"dahua",profiles:[{id:"main",name:"Principal",codec:"h264"},{id:"sub",name:"Secundário",codec:"h264"}],capabilities:caps}));
const instant=(index:number)=>new Date(Date.UTC(2026,9,31,12,index)).toISOString();
const statuses=cameras.map((camera,index)=>({cameraId:camera.id,name:camera.name,code:index%3?"recording":"connecting",lastFrame:instant(index),lastWrite:instant(index),updatedAt:instant(index)}));
const groups=Array.from({length:14},(_,index)=>({id:`group-${index}`,name:`Grupo ${String(index+1).padStart(2,"0")}`,revision:1,grants:[{action:"live",scope:"tenant",resourceId:tenant.id}]}));
const operators=Array.from({length:17},(_,index)=>({...identity.user,id:`operator-${index}`,username:`operador.${String(index+1).padStart(2,"0")}`,globalAdmin:index===0,active:index%3!==0,groups:[groups[index%groups.length].id],membershipRevision:1}));
const jobs=Array.from({length:28},(_,index)=>({id:`export-${index}`,requestedBy:identity.user.id,status:["completed","running","queued","failed"][index%4],code:"",createdAt:instant(index),expires:"2099-12-31T23:59:00Z",result:{frames:25,total:100,bytes:1024}}));
const events=Array.from({length:31},(_,index)=>({id:`event-${index}`,name:cameras[index%cameras.length].name,code:index%2?"recording":"connecting",occurredAt:instant(index)}));
const audit=Array.from({length:31},(_,index)=>({id:`audit-${index}`,actor:operators[index%operators.length].username,action:index%2?"live.started":"operator.login",resourceId:`camera-${index}`,occurredAt:instant(index),revision:1,outcome:index%5?"succeeded":"failed",reason:""}));
const segments=Array.from({length:237},(_,index)=>({id:`segment-${index}`,cameraId:cameras[0].id,siteId:sites[0].id,sourceVersion:"synthetic-source",profile:index%2?"main":"sub",start:new Date(Date.now()-3600_000+index*10000).toISOString(),end:new Date(Date.now()-3600_000+index*10000+9000).toISOString(),bytes:4096}));
const page=(items:unknown[],url:URL,limit=12)=>{const offset=Number(url.searchParams.get("cursor")||0);return {items:items.slice(offset,offset+limit),nextCursor:offset+limit<items.length?String(offset+limit):""};};
const requests:string[]=[];
Object.assign(window,{syntheticTableRequests:requests});
setContext("synthetic-context");
// Every API response is produced in-memory. Unknown routes fail visibly; none reach the installation.
window.fetch=async(input,options)=>{
 const url=new URL(typeof input==="string"?input:input instanceof URL?input.href:input.url,location.origin);
 requests.push(`${options?.method??"GET"} ${url.pathname}${url.search}`);
 const route=url.pathname.replace(/^\/api\/v1/,"");
 let result:unknown;
 if(route.endsWith("/recording-policy"))result={policy:{quotaBytes:100*1024**3,exportBytes:10*1024**3,retentionDays:30,exportHours:24,revision:1},usage:{recordingBytes:1024**3,exportBytes:0}};
 else if(route==="/admin/storage")result={configured:true,config:{path:"synthetic-storage",volumeLimit:500*1024**3,margin:1024**3}};
 else if(route.endsWith("/capabilities"))result=caps;
 else if(route.endsWith("/sites"))result=page(sites,url);
 else if(route.endsWith("/cameras")||route.endsWith("/recording-cameras"))result=page(cameras,url);
 else if(route.endsWith("/groups"))result=page(groups,url);
 else if(route.endsWith("/operators"))result=page(operators,url);
 else if(route.endsWith("/exports"))result=page(jobs,url);
 else if(route.endsWith("/recording-status"))result={items:statuses};
 else if(route.endsWith("/diagnostics"))result={database:"available",workerAvailable:true,storageConfigured:true,usage:{},cameraCount:37,activeCameras:25,observedAt:instant(0),...page(statuses,url)};
 else if(route.endsWith("/recording-events"))result=page(events,url);
 else if(route.endsWith("/audit"))result=page(audit.filter(item=>!url.searchParams.get("action")||item.action===url.searchParams.get("action")),url);
 else if(route.endsWith("/timeline"))result={items:segments};
 else return new Response(JSON.stringify({code:"synthetic_route_not_implemented",route}),{status:404,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
 return new Response(JSON.stringify(result),{headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
};
export function IntegrationFixture(){
 const [view,setView]=useState(location.hash.slice(1)||"cameras");
 useEffect(()=>{const change=()=>setView(location.hash.slice(1)||"cameras");window.addEventListener("hashchange",change);return()=>window.removeEventListener("hashchange",change);},[]);
 return <main className="astra-interface" style={{maxWidth:1280,margin:"24px auto",padding:20}}>
  <p style={{padding:12,background:"#eef8fa",marginBottom:20}}>Integração sintética: componentes reais, API exclusivamente simulada.</p>
  <nav aria-label="Telas da fixture" style={{display:"flex",gap:16,flexWrap:"wrap",marginBottom:24}}>{["cameras","sites","groups","operators","tenants","management","exports","recording","playback","operations"].map(name=><a key={name} href={`#${name}`} style={{textDecoration:"underline",fontWeight:view===name?700:400}}>{name}</a>)}</nav>
  <div key={view}>{view==="cameras"?<CamerasWorkspace tenant={tenant}/>:view==="sites"?<SitesWorkspace tenant={tenant}/>:(view==="management"||view==="operators"||view==="groups"||view==="tenants")?<Administration section={view==="management"?"access":view} tenant={tenant} identity={identity} onRefresh={async()=>{}}/>:view==="exports"?<ExportsWorkspace tenant={tenant}/>:view==="recording"?<RecordingWorkspace tenant={tenant} identity={identity}/>:view==="playback"?<PlaybackWorkspace tenant={tenant}/>:<OperationsWorkspace tenant={tenant} identity={identity}/>}</div>
 </main>;
}
