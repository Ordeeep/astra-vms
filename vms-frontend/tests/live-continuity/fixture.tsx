import {createRoot} from "react-dom/client";
import {useState} from "react";
import {LiveWorkspace} from "../../src/components/live";
import {LiveHeaderControls} from "../../src/components/live-header-controls";
import {LiveViewProvider, useLiveView} from "../../src/components/live-view-context";
import "../../src/app/workspace.css";

// Real React reconciliation and HTML video decoding with a simulated API.
// #native uses actual local WebRTC peers. No credentials/backend are required.
const cameras=Array.from({length:5},(_,i)=>({id:`cam${i+1}`,name:`Câmera ${i+1}`,siteId:"site",active:true,effectiveActive:true,revision:1,sourceVersion:"1",connectionRevision:1,capabilities:{live:true},profiles:[{id:"main",name:"Principal"},{id:"sub2",name:"Substream 2"}]}));
const sessions=new Map<string,{id:string;cameraId:string;profile:string;signaling:string}>();
const mosaics=new Map<string,unknown[]>();
const events:{type:string;id?:string;cameraId?:string;profile?:string}[]=[];
const held=new Set<string>(),failures=new Set<string>(),signalFailures=new Set<string>(),unavailableProfiles=new Set<string>();
const requests:{profile:string;cameraId:string}[]=[];
const delayed=new Set<string>(),postResolvers:(()=>void)[]=[];
const peers=new Set<Peer>();
const BrowserPeer=window.RTCPeerConnection;
const nativeMode=location.hash==="#native";
const nativeServers=new Map<string,NativeSender>();
let serial=0;
const qa={events,sessions,held,failures,signalFailures,unavailableProfiles,requests,delayed,cameras,maxSessions:0,release:()=>{held.clear();for(const peer of peers)peer.deliver();},flushPosts:()=>{delayed.clear();postResolvers.splice(0).forEach(resolve=>resolve());},deny:false};
Object.assign(window,{qa});
const response=(data:unknown,status=200)=>new Response(status===204?null:JSON.stringify(data),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
window.fetch=async(input,init)=>{
 const path=String(input),method=init?.method??"GET";
 if(path.endsWith("/mosaics")){
  const items=mosaics.get(path)??[];
  if(method==="GET")return response({items});
  const saved={...JSON.parse(String(init?.body)),id:`view-${items.length+1}`,ownerId:"test",revision:1,editable:true};
  mosaics.set(path,[...items,saved]);return response(saved,201);
 }
 if(path.endsWith("/cameras"))return response({items:cameras});
 if(path.endsWith("/sites"))return response({items:[{id:"site",name:"Local",active:true}]});
 if(path.endsWith("/live-policy"))return response({revision:1,inherited:true,rules:[{min:1,max:2,profile:"main",fallback:[]},{min:3,max:4,profile:"sub2",fallback:["main"]},{min:5,max:16,profile:"sub3",fallback:["sub2","main"]}]});
 if(path.endsWith("/live-sessions")&&method==="POST"){
  const body=JSON.parse(String(init?.body));
  requests.push({...body});
  if(qa.deny)return response({code:"forbidden"},403);
  const selected=[body.profile,...(body.fallback??[])].find(profile=>!unavailableProfiles.has(profile));
  if(!selected)return response({code:"profile_unavailable"},422);
  body.profile=selected;
  if(failures.has(body.profile))return response({code:"media_unavailable"},503);
  const id=`s${++serial}`,session={id,cameraId:body.cameraId,profile:body.profile,signaling:`/signal/${id}`};
  sessions.set(id,session);qa.maxSessions=Math.max(qa.maxSessions,sessions.size);events.push({type:"open",...session});
  if(delayed.has(body.profile))await new Promise<void>(resolve=>postResolvers.push(resolve));
  return response(session,201);
 }
 if(path.includes("/live-sessions/")){
  const id=path.split("/").at(-1)!;
  if(method==="DELETE"){events.push({type:"delete",id});nativeServers.get(id)?.close();nativeServers.delete(id);sessions.delete(id);return response(null,204);}
  if(qa.deny)return response({code:"forbidden"},403);
  return response({});
 }
 if(path.startsWith("/signal/")){
  const id=path.split("/").at(-1)!;
  if(signalFailures.has(sessions.get(id)?.profile??""))return response({code:"media_unavailable"},503);
  if(!nativeMode)return new Response(id);
  const server=new NativeSender();server.session=sessions.get(id);nativeServers.set(id,server);
  return new Response(await server.answer(String(init?.body)));
 }
 throw new Error(`Unexpected fixture request: ${method} ${path}`);
};
class Peer {
 iceGatheringState="complete";connectionState="connected";localDescription:RTCSessionDescriptionInit|null=null;
 ontrack:((event:{streams:MediaStream[];track:MediaStreamTrack})=>void)|null=null;onconnectionstatechange:(()=>void)|null=null;
 session?:{id:string;profile:string};stream?:MediaStream;clock?:ReturnType<typeof setInterval>;packets=0;closed=false;
 constructor(){peers.add(this);}
 addTransceiver(){return {setCodecPreferences(){}};}
 async createOffer(){return {type:"offer" as const,sdp:"fixture"};}
 async setLocalDescription(value:RTCSessionDescriptionInit){this.localDescription=value;}
 async setRemoteDescription(value:RTCSessionDescriptionInit){this.session=sessions.get(value.sdp!);this.deliver();}
 deliver(){
  if(this.closed||this.stream||!this.session||held.has(this.session.profile))return;
  const canvas=document.createElement("canvas");canvas.width=320;canvas.height=180;
  const context=canvas.getContext("2d")!;
  const draw=()=>{this.packets++;context.fillStyle=this.session!.profile==="main"?"#12586b":"#23452e";context.fillRect(0,0,320,180);context.fillStyle="#fff";context.font="20px sans-serif";context.fillText(`${this.session!.id} ${this.session!.profile} ${this.packets}`,15,90);};
  draw();this.stream=canvas.captureStream(10);this.clock=setInterval(draw,100);
  this.ontrack?.({streams:[this.stream],track:this.stream.getVideoTracks()[0]});
 }
 async getStats(){return new Map([["inbound",{type:"inbound-rtp",kind:"video",packetsReceived:this.packets,bytesReceived:this.packets*100,framesDecoded:this.packets}]]);}
 close(){if(this.closed)return;this.closed=true;if(this.clock)clearInterval(this.clock);this.stream?.getTracks().forEach(track=>track.stop());peers.delete(this);events.push({type:"close",id:this.session?.id});}
}
class NativeSender extends Peer {
 native=new BrowserPeer({iceServers:[]});sender?:RTCRtpSender;
 async answer(sdp:string){
  await this.native.setRemoteDescription({type:"offer",sdp});
  const transceiver=this.native.getTransceivers()[0];transceiver.direction="sendonly";this.sender=transceiver.sender;
  this.deliver();await this.native.setLocalDescription(await this.native.createAnswer());
  if(this.native.iceGatheringState!=="complete")await new Promise<void>(resolve=>this.native.addEventListener("icegatheringstatechange",()=>{if(this.native.iceGatheringState==="complete")resolve();}));
  return this.native.localDescription!.sdp;
 }
 deliver(){if(!this.sender||this.closed)return;super.deliver();if(this.stream)void this.sender.replaceTrack(this.stream.getVideoTracks()[0]).catch(()=>{});}
 close(){this.native.close();super.close();}
}
Object.assign(window,{RTCPeerConnection:nativeMode?BrowserPeer:Peer});

function Controls(){
 const view=useLiveView();
 return <nav aria-label="Controles de teste">
  <button onClick={()=>{view.changeLayout(3);view.setPositions(cameras.map((c,tileId)=>({tileId,cameraId:c.id,profile:""})));}}>Cinco câmeras</button>
  <button onClick={()=>view.setPositions(cameras.slice(0,4).map((c,tileId)=>({tileId,cameraId:c.id,profile:""})))}>Quatro câmeras</button>
  <button onClick={()=>view.setPositions(cameras.slice(0,2).map((c,tileId)=>({tileId,cameraId:c.id,profile:""})))}>Duas câmeras</button>
  <button onClick={()=>view.setPositions(cameras.slice(0,3).map((c,tileId)=>({tileId,cameraId:c.id,profile:""})))}>Três câmeras</button>
  <button onClick={()=>view.setMaximized(view.maximized===0?null:0)}>Alternar maximização</button>
  <button onClick={()=>view.setPositions(p=>p.map((v,i)=>i===0?{...v,profile:"main"}:v))}>Principal na primeira</button>
  <button onClick={()=>view.setPositions(p=>p.map((v,i)=>i===0?{...v,profile:"sub2"}:v))}>Substream na primeira</button>
  <button onClick={()=>view.setPositions(p=>p.map((v,i)=>i===0?{...v,profile:""}:v))}>Automática na primeira</button>
  <button onClick={()=>view.setPositions(p=>p.map((v,i)=>i===0?{...v,cameraId:"cam5"}:v))}>Substituir primeira</button>
  <button onClick={()=>view.setPositions(p=>p.map(v=>({...v})))}>Atualizar catálogo</button>
 </nav>;
}
function Fixture(){const [tenant,setTenant]=useState("test");return <><button onClick={()=>setTenant(t=>t==="test"?"other":"test")}>Trocar tenant</button><LiveViewProvider key={tenant} tenantId={tenant} enabled><Controls/><header><LiveHeaderControls/></header><LiveWorkspace tenant={{id:tenant,name:tenant,active:true,revision:1}}/></LiveViewProvider></>;}
document.body.innerHTML='<div id="fixture"></div>';
document.body.style.cssText="margin:0;background:#111;color:white;font-family:sans-serif";
const style=document.createElement("style");style.textContent=".live-workspace{height:85vh}.live-grid{width:100%}.live-cell{display:flex;flex-direction:column}.video-area{min-height:180px}button{margin:4px} .video-state{background:#000}";document.head.append(style);
createRoot(document.getElementById("fixture")!).render(<Fixture/>);
