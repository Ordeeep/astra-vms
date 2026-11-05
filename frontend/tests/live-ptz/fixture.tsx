import React,{useEffect,useRef,useState} from "react";
import {createRoot} from "react-dom/client";
import {LiveViewProvider,useLiveView} from "@/components/live-view-context";
import {LivePTZControls} from "@/components/live-ptz-controls";
import {LiveCameraList} from "@/components/live-camera-list";
import {setContext} from "@/lib/api";
import {type Camera} from "@/components/management";
type Request={path:string;method:string;body:Record<string,unknown>;context:string};
const requests:Request[]=[];
let delay=0,serial=0,superseded=false,revoked=false;
declare global{interface Window{ptzQA:{requests:Request[];delay:(ms:number)=>void;supersede:()=>void;revoke:()=>void}}}
window.ptzQA={requests,delay:ms=>{delay=ms;},supersede:()=>{superseded=true;},revoke:()=>{revoked=true;window.dispatchEvent(new CustomEvent("vms:change",{detail:{type:"InventoryChanged"}}));}};
const make=(id:string):Camera=>({id,name:id==="a"?"Entrada":"Pátio",siteId:"site",active:true,effectiveActive:true,revision:1,sourceVersion:"source-"+id,connectionRevision:1,connectionType:"hikvision",profiles:[{id:"main",name:"Principal"}],capabilities:{live:true,"ptz.control":!revoked}});
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
window.fetch=async(input,options)=>{
 const path=new URL(String(input),location.origin).pathname,method=options?.method??"GET";
 const body=options?.body?JSON.parse(String(options.body)):{};
 requests.push({path,method,body,context:new Headers(options?.headers).get("X-VMS-Context")??""});
 if(path.endsWith("/cameras"))return json({items:[make("a"),make("b")]});
 if(path.endsWith("/sites"))return json({items:[{id:"site",name:"Lab",active:true}]});
 if(path.endsWith("/ptz"))return json({panTilt:!path.includes("/b/"),zoom:true,home:true,revision:path.includes("/a/")?"source-a/1/site":"source-b/1/site"});
 if(method==="POST"&&path.endsWith("/commands")){const id="command-"+(++serial);if(delay)await new Promise(r=>setTimeout(r,delay));superseded=false;return json({id,expiresInMs:2000},201);}
 if(method==="PUT")return superseded?json({code:"ptz_superseded"},409):json({expiresInMs:2000});
 if(method==="DELETE")return new Response(null,{status:204,headers:{"X-VMS-Contract":"1"}});
 return json({code:"fixture_route_not_implemented"},404);
};
function Controls(){
 const view=useLiveView(),loaded=useRef(false);
 useEffect(()=>{if(!view.loading&&!loaded.current){loaded.current=true;view.chooseCamera("a",0);view.chooseCamera("b",1);view.setActivePosition(0);}},[view.loading]);
 return <><nav style={{display:"flex",gap:8,marginBottom:24}}>
 <button onClick={()=>view.setActivePosition(0)}>Ativar Entrada</button>
 <button onClick={()=>view.setActivePosition(1)}>Ativar Pátio</button>
 <button onClick={()=>view.setMaximized(1)}>Maximizar Pátio</button>
 <button onClick={()=>view.setMaximized(null)}>Restaurar</button>
 <button onClick={()=>view.setPositions(p=>p.map(tile=>({...tile,cameraId:""})))}>Limpar mosaico</button>
 </nav><div style={{width:230,border:"1px solid var(--border)",borderRadius:8}}><LivePTZControls/></div></>;
}
function Fixture(){const[tenant,setTenant]=useState("tenant");
 if(new URLSearchParams(location.search).has("panel"))return <><header style={{height:40}}>Ao vivo</header><main className="content live-content"><LiveViewProvider tenantId={tenant} enabled><section className="live-workspace"><div className="live-stage"><div className="live-grid"/><aside className="live-camera-panel"><div className="live-camera-list-content"><LiveCameraList/></div></aside></div></section></LiveViewProvider></main></>;
 return <main style={{padding:24}}><button onClick={()=>{setContext("context-other");setTenant("other");}}>Trocar tenant</button><LiveViewProvider key={tenant} tenantId={tenant} enabled><Controls/></LiveViewProvider></main>}
setContext("context-tenant");createRoot(document.getElementById("root")!).render(<Fixture/>);
