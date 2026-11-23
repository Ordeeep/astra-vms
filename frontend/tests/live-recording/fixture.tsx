import {createRoot} from "react-dom/client";
import {useState} from "react";
import {LiveViewProvider, useLiveView} from "@/components/live-view-context";
import {LiveWorkspace} from "@/components/live";
import {LiveHeaderActions, LiveHeaderControls} from "@/components/live-header-controls";
import {TooltipProvider} from "@/components/ui/tooltip";
import {RecordingExportProvider} from "@/components/exports";
import {setContext} from "@/lib/api";

const cameras=Array.from({length:2},(_,i)=>({id:`cam${i+1}`,name:i===0?"Portaria principal · acesso de funcionários":"Estacionamento",siteId:"site",active:true,effectiveActive:true,revision:1,sourceVersion:"1",profiles:[{id:"main",name:"Principal"}],capabilities:{live:true,playback:true}}));
const blocks=[['2026-11-16T12:00:00Z','2026-11-16T13:00:00Z'],['2026-11-17T12:00:00Z','2026-11-17T12:00:36Z'],['2026-11-17T16:00:00Z','2026-11-17T16:00:12Z'],['2026-11-18T02:59:50Z','2026-11-18T03:00:14Z']].map(([start,end])=>({start:Date.parse(start),end:Date.parse(end)}));
const requests:{path:string;method:string;body:any;context:string|null}[]=[];
const sessions=new Map<string,{at:number;start:number;end:number;next:number|null}>();
const liveSessions=new Map<string,any>();
let serial=0,deny=false,monthFailure=false,hold=false;
const exportPreviews=new Map<string,{cameraId:string;start:string;end:string}>();
const exportTasks=new Map<string,{id:string;cameraName:string;author:string;createdAt:string;start:string;end:string;status:string;code?:string;frames:number;total:number;hasGaps:boolean;canDownload:boolean;downloadExpired:boolean}>();
let exportEmpty=false;
const releases:(()=>void)[]=[];
const qa={requests,sessions,liveSessions,deny:(value:boolean)=>{deny=value;},failMonth:(value:boolean)=>{monthFailure=value;},hold:(value:boolean)=>{hold=value;},release:()=>{hold=false;releases.splice(0).forEach(resolve=>resolve());}};
Object.assign(qa,{
 setExportResult:(status:string,frames:number,downloadExpired=false)=>{
  const task=exportTasks.get('export-job')!;
  Object.assign(task,{status,frames,canDownload:status==='completed'&&!downloadExpired,downloadExpired});
 },
 setExportFailure:(id:string,code?:string)=>{
  Object.assign(exportTasks.get(id)!,{status:'failed',code});
 },
 setExportEmpty:(value:boolean)=>{exportEmpty=value;},
});
Object.assign(qa, {grow: () => { blocks[1].end += 10000; window.dispatchEvent(new CustomEvent('vms:change', {detail:{type:'ArchiveChanged',cameraId:'cam1',payload:{start:new Date(blocks[1].start).toISOString(),end:new Date(blocks[1].end).toISOString()}}})); }});
Object.assign(window,{recordingQA:qa});
const response=(value:unknown,status=200)=>new Response(status===204?null:JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
window.fetch=async(input,options)=>{
 const url=new URL(typeof input==='string'?input:input instanceof URL?input.href:input.url,location.origin),path=url.pathname,method=options?.method??'GET',body=options?.body&&path.startsWith('/api/v1/')?JSON.parse(String(options.body)):null;
 requests.push({path:path+url.search,method,body,context:new Headers(options?.headers).get('X-VMS-Context')});
 if(path.endsWith('/recording-availability')){
  if(deny)return response({code:'forbidden'},403);
  if(url.searchParams.has('month'))return monthFailure?response({code:'database_unavailable'},503):response({timezone:'America/Sao_Paulo',days:['2026-11-16','2026-11-17','2026-11-18'].filter(d=>d.startsWith(url.searchParams.get('month')!))});
  const direction=url.searchParams.get('direction');
  if(direction){const at=Date.parse(url.searchParams.get('at')!);const candidates=blocks.filter(b=>direction==='next'?b.start>at:b.end<=at);const interval=direction==='next'?candidates[0]:candidates.at(-1);return response({timezone:'America/Sao_Paulo',interval:interval?{start:new Date(interval.start).toISOString(),end:new Date(interval.end).toISOString()}:null});}
  if(url.searchParams.has('start')){const start=Date.parse(url.searchParams.get('start')!),end=Date.parse(url.searchParams.get('end')!);return response({timezone:'America/Sao_Paulo',intervals:blocks.filter(b=>b.end>start&&b.start<end).map(b=>({start:new Date(b.start).toISOString(),end:new Date(b.end).toISOString()}))});}
  return response({timezone:'America/Sao_Paulo'});
 }
 if(path.endsWith('/playback-sessions')&&method==='POST'){
  if(deny)return response({code:'forbidden'},403);
  const at=Date.parse(body.start),block=blocks.find(b=>b.start<=at&&b.end>at);
  if(!block)return response({code:'recording_gap',nextStart:new Date(blocks.find(b=>b.start>at)?.start??blocks[0].start).toISOString()},409);
  const reverse=Date.parse(body.end)-at<9000;
  const start=reverse?at:block.start+Math.floor((at-block.start)/12000)*12000,end=Math.min(block.end,start+12000);
  const id=`play${++serial}`;sessions.set(id,{at,start,end,next:end>=block.end?(blocks.find(b=>b.start>end)?.start??null):null});
  if(hold)await new Promise<void>(resolve=>releases.push(resolve));
  return response({id,manifest:`/fixture-media/manifest.m3u8?session=${id}`},201);
 }
 if(path.includes('/playback-sessions/')){const id=path.split('/').at(-1)!;if(method==='DELETE'){sessions.delete(id);return response(null,204);}return deny?response({code:'forbidden'},403):response(null,204);}
 if(path.includes('/fixture-media/')&&url.searchParams.get('format')==='json'){
  const session=sessions.get(url.searchParams.get('session')!)!;
  return response({offset:(session.at-session.start)/1000,nextStart:session.next?new Date(session.next).toISOString():null,mapping:[{mediaStart:0,start:new Date(session.start).toISOString(),end:new Date(session.end).toISOString()}]});
 }
 if(path.endsWith('/recording-cameras'))return response({items:cameras.map(camera=>({id:camera.id,name:camera.name,capabilities:{playback:true,export:camera.id==='cam2'}}))});
 if(path.endsWith('/export-previews')&&method==='POST'){
  if(exportEmpty)return response({code:'export_has_no_images'},422);
  const id=`export-preview-${exportPreviews.size+1}`;
  exportPreviews.set(id,body);
  return response({id,revision:1,gaps:[{}]},201);
 }
 if(path.endsWith('/exports')&&method==='POST'){
  const {cameraId,start,end}=exportPreviews.get(body.previewId)!;
  const id=exportTasks.size?`export-job-${exportTasks.size+1}`:'export-job';
  exportTasks.set(id,{id,cameraName:cameras.find(camera=>camera.id===cameraId)!.name,author:'Operadora QA',createdAt:new Date().toISOString(),start,end,status:'queued',frames:0,total:100,hasGaps:true,canDownload:false,downloadExpired:false});
  return response({id,status:'queued'},202);
 }
 const activeExports=()=>[...exportTasks.values()].filter(task=>task.status==='queued'||task.status==='running');
 if(path.endsWith('/export-tasks')&&method==='GET')return response({items:[...exportTasks.values()].reverse(),nextCursor:'',hasActive:activeExports().length>0});
 if(path.endsWith('/exports/cancel')&&method==='POST'){
  const active=activeExports();
  active.forEach(task=>{task.status='cancelled';});
  return response({cancelled:active.length});
 }
 if(path.includes('/exports/')&&path.endsWith('/cancel')&&method==='POST'){
  const task=activeExports().find(item=>item.id===path.split('/').at(-2));
  if(!task)return response({code:'export_finished'},409);
  task.status='cancelled';
  return response(null,204);
 }
 if(path.endsWith('/cameras'))return response({items:cameras});
 if(path.endsWith('/sites'))return response({items:[{id:'site',name:'Matriz',active:true,timezone:'America/Sao_Paulo'}]});
 if(path.endsWith('/mosaics'))return response({items:[]});
 if(path.endsWith('/live-policy'))return response({rules:[{min:1,max:16,profile:'main',fallback:[]}]});
 if(path.endsWith('/live-sessions')&&method==='POST'){const id=`live${++serial}`,value={id,cameraId:body.cameraId,profile:'main',signaling:`/signal/${id}`};liveSessions.set(id,value);return response(value,201);}
 if(path.includes('/live-sessions/')){if(method==='DELETE')liveSessions.delete(path.split('/').at(-1)!);return response(null,204);}
 if(path.startsWith('/signal/'))return new Response(path.split('/').at(-1));
 throw Error(`Fixture route ${method} ${path}`);
};
class Peer {
 iceGatheringState='complete';connectionState='connected';localDescription:any=null;ontrack:any=null;onconnectionstatechange:any=null;
 stream?:MediaStream;timer?:ReturnType<typeof setInterval>;frames=0;
 addTransceiver(){return {setCodecPreferences(){}};}
 async createOffer(){return {type:'offer',sdp:'fixture'};}
 async setLocalDescription(value:any){this.localDescription=value;}
 async setRemoteDescription(){
  const canvas=document.createElement('canvas');canvas.width=320;canvas.height=180;const ctx=canvas.getContext('2d')!;
  const draw=()=>{this.frames++;ctx.fillStyle='#124454';ctx.fillRect(0,0,320,180);ctx.fillStyle='#fff';ctx.font='18px sans-serif';ctx.fillText(`Ao vivo · ${this.frames}`,20,90);};
  draw();this.stream=canvas.captureStream(10);this.timer=setInterval(draw,100);this.ontrack?.({streams:[this.stream],track:this.stream.getVideoTracks()[0]});
 }
 async getStats(){return new Map([['inbound',{type:'inbound-rtp',kind:'video',packetsReceived:this.frames,framesDecoded:this.frames,bytesReceived:this.frames*100}]]);}
 close(){clearInterval(this.timer);this.stream?.getTracks().forEach(t=>t.stop());}
}
Object.assign(window,{RTCPeerConnection:Peer});
function Controls(){const view=useLiveView();return <nav className="fixture-controls">
 <button onClick={()=>{view.changeLayout("2x2");view.setPositions([0,1,2,3].map(tileId=>({tileId,cameraId:tileId<2?'cam1':tileId===2?'cam2':'',profile:''})));}}>Mosaico repetido</button>
 <button onClick={()=>view.changeLayout("1x1")}>Layout 1</button>
 <button onClick={()=>view.setMaximized(view.maximized===0?null:0)}>Maximizar</button>
 <button onClick={()=>void view.toggleFullscreen()}>Video Wall</button>
 </nav>;}
function Fixture(){const [show,setShow]=useState(true);return <TooltipProvider><main className="astra-interface"><button onClick={()=>setShow(v=>!v)}>Alternar tela</button><LiveViewProvider tenantId="fixture" enabled={show}><RecordingExportProvider tenantId="fixture"><Controls/>{show&&<><header><LiveHeaderControls/><LiveHeaderActions/></header><LiveWorkspace tenant={{id:'fixture',name:'Fixture',active:true,revision:1}}/></>}</RecordingExportProvider></LiveViewProvider></main></TooltipProvider>;}
setContext('fixture-context');
createRoot(document.getElementById('root')!).render(<Fixture/>);
