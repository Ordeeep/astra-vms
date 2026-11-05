import React from "react";
import {createRoot} from "react-dom/client";
import {CamerasWorkspace} from "@/components/cameras";
import {setContext,type Tenant} from "@/lib/api";

const tenant:Tenant={id:"synthetic",name:"Laboratório",active:true,revision:1};
const params=new URL(location.href).searchParams;
let cameras=[{id:"camera",name:"Entrada",siteId:"site",active:true,effectiveActive:true,revision:1,sourceVersion:"source",connectionType:"rtsp",profiles:[{id:"main",name:"Principal"}],capabilities:{"inventory.manage":true,"camera.delete":!params.has("no-delete")}}];
const requests:{method:string;path:string;body:unknown}[]=[];
setContext("fixture-context");
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
// Completely local fixture: no reads or mutations reach the installation.
window.fetch=async(input,options)=>{
 const url=new URL(typeof input==="string"?input:input instanceof URL?input.href:input.url,location.origin);
 const path=url.pathname;const method=options?.method??"GET";
 requests.push({method,path,body:options?.body?JSON.parse(String(options.body)):null});
 document.querySelector("#requests")!.textContent=JSON.stringify(requests);
 if(method==="GET"&&path.endsWith("/sites"))return json({items:[{id:"site",name:"Matriz",active:true,capabilities:{"inventory.manage":true}}]});
 if(method==="GET"&&path.endsWith("/cameras"))return json({items:cameras});
 if(method==="GET"&&path.endsWith("/capabilities"))return json({"inventory.manage":true});
 if(method==="DELETE"&&path.endsWith("/cameras/camera")){
  await new Promise(resolve=>setTimeout(resolve,150));
  if(params.has("fail"))return json({code:"database_unavailable"},503);
  if(params.has("conflict"))return json({code:"revision_conflict"},412);
  cameras=[];return json({code:"camera_deleted"},202);
 }
 return json({code:"fixture_route_not_implemented"},404);
};
createRoot(document.getElementById("root")!).render(<main className="astra-interface" style={{maxWidth:1100,margin:"24px auto",padding:24}}><CamerasWorkspace tenant={tenant}/><pre id="requests" hidden/></main>);
