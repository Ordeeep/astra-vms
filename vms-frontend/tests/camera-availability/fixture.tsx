import React,{useState} from "react";
import {createRoot} from "react-dom/client";
import {CamerasWorkspace} from "@/components/cameras";
import {type Camera} from "@/components/management";
import {setContext,type Tenant} from "@/lib/api";
import {type CameraAvailability} from "@/lib/camera-availability";

const tenant:Tenant={id:"synthetic",name:"Laboratório",active:true,revision:1};
const stamp=()=>new Date().toISOString(),until=()=>new Date(Date.now()+45_000).toISOString();
const make=(id:string,name:string,availability:CameraAvailability,active=true):Camera=>({id,name,siteId:"site",active,effectiveActive:active,revision:1,sourceVersion:"source",connectionType:"dahua",connectionRevision:1,profiles:[{id:"main",name:"Principal"},{id:"sub1",name:"Substream 1"}],capabilities:{"inventory.manage":true},availability});
let cameras:Camera[]=[
 make("online","Entrada",{state:"online",checkedAt:stamp(),lastVideoAt:stamp(),validUntil:until(),profile:"sub1",warnings:[{profile:"main",available:false,complete:true,code:"authentication_failed",checkedAt:stamp()}]}),
 make("offline","Estacionamento",{state:"offline",code:"video_unavailable",checkedAt:stamp(),validUntil:until(),warnings:[{profile:"main",available:false,complete:true,code:"video_timeout",checkedAt:stamp()}]}),
 make("unknown","Corredor",{state:"unknown",code:"awaiting_check"}),
 make("suspended","Depósito",{state:"suspended",code:"camera_inactive"},false),
];
type RequestLog={method:string;path:string};
const requests:RequestLog[]=[];let fail=false;let delay=0;
declare global{interface Window{availabilityQA:{requests:RequestLog[];setStatus:(id:string,status:CameraAvailability)=>void;fail:(value:boolean)=>void;delay:(milliseconds:number)=>void;expire:()=>void}}}
window.availabilityQA={requests,setStatus:(id,status)=>{cameras=cameras.map(c=>c.id===id?{...c,availability:status}:c);},fail:value=>{fail=value;},delay:milliseconds=>{delay=milliseconds;},expire:()=>{cameras=cameras.map(c=>({...c,availability:{...c.availability!,validUntil:new Date(Date.now()-1000).toISOString()}}));}};
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
window.fetch=async(input,options)=>{
 const url=new URL(typeof input==="string"?input:input instanceof URL?input.href:input.url,location.origin);
 const path=url.pathname,method=options?.method??"GET";requests.push({method,path});
 const old=cameras.map(c=>({...c}));
 if(delay)await new Promise(resolve=>setTimeout(resolve,delay));
 if(fail)return json({code:"database_unavailable"},503);
 if(path.includes("/other/")&&path.endsWith("/cameras"))return json({items:[]});
 if(method==="GET"&&path.endsWith("/sites"))return json({items:[{id:"site",name:"Matriz",active:true,capabilities:{"inventory.manage":true}}]});
 if(method==="GET"&&path.endsWith("/cameras"))return json({items:old});
 if(method==="GET"&&path.endsWith("/capabilities"))return json({"inventory.manage":true});
 return json({code:"fixture_route_not_implemented"},404);
};
function Fixture(){
 const [current,setCurrent]=useState(tenant);
 return <main className="astra-interface" style={{maxWidth:1400,margin:"24px auto",padding:24}}><button type="button" onClick={()=>{setContext(current.id==="synthetic"?"other-context":"fixture-context");setCurrent(current.id==="synthetic"?{...tenant,id:"other"}:tenant);}}>Trocar tenant de teste</button><CamerasWorkspace tenant={current}/></main>;
}
setContext("fixture-context");
createRoot(document.getElementById("root")!).render(<Fixture/>);
