import React from "react";
import {createRoot} from "react-dom/client";
import {CamerasWorkspace} from "@/components/cameras";
import {type Camera,type Profile} from "@/components/management";
import {setContext,type Tenant} from "@/lib/api";

const tenant:Tenant={id:"synthetic",name:"Laboratório",active:true,revision:1};
const profiles:Profile[]=[{id:"main",name:"Principal",codec:"h264",width:1920,height:1080,fps:25},{id:"sub1",name:"Substream 1",codec:"h264",width:640,height:360,fps:15}];
const capabilities={"inventory.manage":true};
let cameras:Camera[]=[
 {id:"rtsp",name:"RTSP existente",siteId:"site",active:true,effectiveActive:true,revision:7,sourceVersion:"rtsp-source",connectionType:"rtsp",profiles,capabilities},
 {id:"dahua",name:"Dahua existente",siteId:"site",active:true,effectiveActive:true,revision:9,sourceVersion:"dahua-source",connectionType:"dahua",connection:{host:"192.0.2.40",protocol:"http",httpPort:80,username:"operador",hasPassword:true},profiles,capabilities},
 {id:"restricted",name:"Site restrito",siteId:"restricted-site",active:true,effectiveActive:true,revision:1,sourceVersion:"restricted-source",connectionType:"rtsp",profiles:[profiles[0]],capabilities},
];
type RequestLog={method:string;path:string;body:Record<string,unknown>|null};
const requests:RequestLog[]=[];
let failSave=false,failDiscovery=false,delayDiscovery=0;
declare global{interface Window{cameraFormQA:{requests:RequestLog[];failSave:(value:boolean)=>void;failDiscovery:(value:boolean)=>void;delayDiscovery:(milliseconds:number)=>void}}}
window.cameraFormQA={requests,failSave:value=>{failSave=value;},failDiscovery:value=>{failDiscovery=value;},delayDiscovery:milliseconds=>{delayDiscovery=milliseconds;}};
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
// All requests remain inside this synthetic fixture; no installation is contacted.
window.fetch=async(input,options)=>{
 const path=new URL(typeof input==="string"?input:input instanceof URL?input.href:input.url,location.origin).pathname;
 const method=options?.method??"GET",body=options?.body?JSON.parse(String(options.body)):null;
 requests.push({method,path,body});
 if(method==="GET"&&path.endsWith("/sites"))return json({items:[{id:"site",name:"Matriz",active:true,capabilities},{id:"other-site",name:"Anexo",active:true,capabilities},{id:"forbidden",name:"Sem permissão",active:true,capabilities:{}}]});
 if(method==="GET"&&path.endsWith("/cameras"))return json({items:cameras});
 if(method==="GET"&&path.endsWith("/capabilities"))return json(capabilities);
 if(method==="POST"&&path.endsWith("/source-discovery")){
  const failed=failDiscovery;
  if(delayDiscovery)await new Promise(resolve=>setTimeout(resolve,delayDiscovery));
  return json(failed?{profiles:[],api:{status:"error",code:"camera_auth_failed"}}:{device:{model:"Dispositivo de teste",vendor:body.connectionType==="hikvision"?"Hikvision":"Dahua"},profiles,api:{status:"ok"}});
 }
 if(method==="POST"&&path.endsWith("/source-tests"))return json({video:{codec:"h264",width:1920,height:1080,fps:25}});
 if(method==="POST"&&path.endsWith("/profile-tests"))return json({id:"diagnostic",status:"done",result:{video:{codec:"h264",profile:"main",width:1920,height:1080,fps:25,h264Candidate:true}}});
 if(method==="PATCH"&&path.endsWith("/configuration")){
  if(failSave)return json({code:"revision_conflict"},412);
  const cameraId=path.split("/").at(-2);
  cameras=cameras.map(camera=>camera.id===cameraId?{...camera,name:body.name,siteId:body.siteId,active:body.active,revision:camera.revision+1}:camera);
  return json({ok:true});
 }
 if(method==="POST"&&path.endsWith("/cameras"))return failSave?json({code:"revision_conflict"},412):json({id:"created"},201);
 return json({code:"fixture_route_not_implemented"},404);
};
setContext("fixture-context");
createRoot(document.getElementById("root")!).render(<main className="astra-interface" style={{maxWidth:1200,margin:"24px auto",padding:24}}><CamerasWorkspace tenant={tenant}/></main>);
