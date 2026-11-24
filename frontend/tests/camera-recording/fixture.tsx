import React,{useRef,useState} from "react";
import {createRoot} from "react-dom/client";
import {CamerasWorkspace} from "@/components/cameras";
import {StorageWorkspace} from "@/components/storage";
import {type Camera} from "@/components/management";
import {newRecordingConfig,type RecordingConfig} from "@/lib/camera-recording";
import {setContext,type Identity,type Tenant} from "@/lib/api";

const tenant:Tenant={id:"fixture",name:"Laboratório",active:true,revision:1};
const mode=new URL(location.href).searchParams.get("mode");
const bulk=mode==="bulk";
const globalAdmin=!["recording-only","inventory-only","viewer"].includes(mode??"");
const capabilities={"inventory.manage":mode!=="recording-only"&&mode!=="viewer","recording.manage":mode!=="inventory-only"&&mode!=="viewer","camera.delete":bulk,playback:mode==="viewer"};
const identity:Identity={user:{id:"admin",username:"fixture",name:"Fixture",email:"",active:true,globalAdmin,revision:1},tenants:[tenant],csrfToken:"fixture"};
const camera:Camera={id:"camera",name:"Portaria",siteId:"site",active:true,effectiveActive:true,revision:4,sourceVersion:"source",connectionType:"rtsp",profiles:[{id:"main",name:"Principal"},{id:"sub1",name:"Substream"}],capabilities};
let cameras=bulk?Array.from({length:12},(_,index)=>({...camera,id:String(index+1),name:index===0?camera.name:`Bloco ${String(index+1).padStart(2,"0")}`})):[camera];
let recording:RecordingConfig={...newRecordingConfig(),enabled:true,profile:"sub1",revision:3};
let policy={quotaBytes:1073741824,exportBytes:536870912,retentionDays:10,exportHours:24,revision:2};
let storageConfigured=mode!=="pending",quotaConfigured=mode!=="pending",failRecording=false,conflict=false,retentionImpact=false;
const requests:{method:string;path:string;body:any}[]=[];
declare global {interface Window {recordingQA:{requests:typeof requests;failRecording:(value:boolean)=>void;conflict:(value:boolean)=>void;retentionImpact:(value:boolean)=>void;getRecording:()=>RecordingConfig}}}
window.recordingQA={requests,failRecording:value=>{failRecording=value;},conflict:value=>{conflict=value;},retentionImpact:value=>{retentionImpact=value;},getRecording:()=>recording};
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
window.fetch=async(input,options)=>{
 const path=new URL(typeof input==="string"?input:input instanceof URL?input.href:input.url,location.origin).pathname;
 const method=options?.method??"GET",body=options?.body?JSON.parse(String(options.body)):null;
 requests.push({method,path,body});
 if(method==="GET"&&path.endsWith("/sites"))return json({items:[{id:"site",name:"Matriz",timezone:"America/Sao_Paulo",active:true,capabilities},...(bulk?[{id:"destination",name:"Filial",timezone:"UTC",active:false,capabilities}]:[])]});
 if(method==="GET"&&path.endsWith("/cameras"))return json({items:cameras});
 if(method==="GET"&&path.endsWith("/cameras/camera"))return json(cameras[0]);
 if(method==="GET"&&path.endsWith("/capabilities"))return json(capabilities);
 if(method==="GET"&&path.endsWith("/recording"))return failRecording?json({code:"database_unavailable"},503):json(recording);
 if(method==="GET"&&path.endsWith("/recording-status"))return json({items:mode==="inventory-only"?[]:[{cameraId:"camera",code:!recording.enabled?"recording_disabled":!storageConfigured?"storage_not_configured":!quotaConfigured?"storage_quota_pending":"recording",lastWrite:"2026-11-16T15:30:00Z",updatedAt:"2026-11-16T15:30:00Z"}],storageConfigured,quotaConfigured});
 if(method==="PATCH"&&path.endsWith("/configuration")){
  if(conflict)return json({code:"revision_conflict"},412);
  if(retentionImpact&&!body.recording?.confirmImpact)return json({code:"retention_impact_confirmation",eligibleBytes:1048576},409);
  if(body.recording)recording={...body.recording,revision:recording.revision+1};
  if(!body.recordingOnly)cameras=cameras.map(c=>({...c,name:body.name,siteId:body.siteId,active:body.active,revision:c.revision+1,profiles:body.source?.profiles??c.profiles}));
  return json(cameras[0]);
 }
 if(method==="POST"&&path.endsWith("/cameras"))return json({id:"created"},201);
 if(bulk&&method==="POST"&&path.endsWith("/cameras/bulk")){
  if(body.action==="configure"&&body.cameraIds.includes("12"))return json({code:"camera_bulk_rejected",issues:[{cameraId:"12",code:"recording_profile_unavailable"}]},422);
  if(body.action==="delete")cameras=cameras.filter(camera=>!body.cameraIds.includes(camera.id));
  return json({changed:body.cameraIds.length,unchanged:0},body.action==="delete"?202:200);
 }
 if(path.endsWith("/admin/storage")){if(method==="POST")storageConfigured=true;return json({configured:storageConfigured,config:{path:"D:\\Astra\\Acervo",volumeLimit:10737418240,margin:268435456}});}
 if(path.endsWith("/recording-policy")){if(method==="PUT"){policy={...body,revision:policy.revision+1};quotaConfigured=policy.quotaBytes>0;}return json({policy,usage:{recording:1048576}});}
 if(path.endsWith("/quota-jobs"))return json({items:[]});
 if(method==="POST"&&path.endsWith("/source-discovery"))return json({profiles:[],api:{status:"error",code:"camera_api_unavailable"}});
 return json({code:"fixture_route_not_implemented"},404);
};
setContext("fixture-context");
function Fixture(){
 const [page,setPage]=useState("cameras");const guard=useRef<(()=>boolean)|null>(null);
 const navigate=(target:string)=>{if(!guard.current||guard.current())setPage(target);};
 return <main className="astra-interface" style={{maxWidth:1400,margin:"24px auto",padding:20}}>
  <nav aria-label="Fixture"><button onClick={()=>navigate("cameras")}>Câmeras</button>{(globalAdmin||capabilities["recording.manage"])&&<button onClick={()=>navigate("storage")}>Armazenamento</button>}</nav>
  {page==="cameras"?<CamerasWorkspace tenant={tenant} registerCloseGuard={value=>{guard.current=value;}} onStorage={()=>{guard.current=null;setPage("storage");}}/>:<StorageWorkspace tenant={tenant} identity={identity}/>}
 </main>;
}
createRoot(document.getElementById("root")!).render(<Fixture/>);
