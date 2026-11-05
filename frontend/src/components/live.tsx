"use client";
import {LiveFullscreenControls} from "@/components/live-fullscreen";
import { LiveCameraMenu } from "@/components/live-camera-menu";
import { LiveCameraList } from "@/components/live-camera-list";
import { LiveRecordingPanel } from "@/components/live-recording-panel";
import { Card } from "@/components/ui/card";
import { Empty } from "@/components/ui/empty";
import { LoaderCircle, Plus } from "lucide-react";
import { AlertDescription, Alert } from "@/components/ui/alert";
import {useEffect,useRef,useState} from "react";
import {api,type Tenant} from "@/lib/api";
import {onEnvironmentChange} from "@/lib/events";
import {useLiveView,type LivePosition as Position} from "@/components/live-view-context";
import {LiveCameraViewport} from "@/components/live-camera-viewport";
import {LivePlayer} from "@/components/live-player";
import {createLiveTransitionQueue} from "@/lib/live-transition-queue";
type Rule={min:number;max:number;profile:string;fallback:string[]};
type Policy={rules:Rule[];revision:number;inherited:boolean};
export function LiveWorkspace({tenant}:{tenant:Tenant;globalAdmin?:boolean}){
 const {fullscreen,fullscreenError,maximized,setMaximized,highlightedPosition,layoutSize,cameraListOpen,cameras,positions,setPositions,activePosition,setActivePosition,chooseCamera,moveCamera,dropPosition,setDropPosition,pendingCameraId,setPendingCameraId}=useLiveView();
 const [policy,setPolicy]=useState<Policy|null>(null),[error,setError]=useState("");
 const [transitionQueue]=useState(createLiveTransitionQueue);
 const [recordingCamera,setRecordingCamera]=useState<{id:string;name:string}|null>(null);
 const gesture=useRef<{x:number;y:number;from:number;pointerId:number;element:HTMLElement;moved:boolean}|null>(null);
 const suppressClick=useRef(false);
 const [draggedPosition,setDraggedPosition]=useState<number|null>(null);
 const targetPosition=(x:number,y:number)=>{
  const cell=document.elementFromPoint(x,y)?.closest<HTMLElement>("[data-live-position]");
  return cell&&!cell.hidden?Number(cell.dataset.livePosition):null;
 };
 const clearDrag=()=>{
  const current=gesture.current;gesture.current=null;
  if(current?.element.hasPointerCapture(current.pointerId))current.element.releasePointerCapture(current.pointerId);
  setDraggedPosition(null);setDropPosition(null);
 };
 useEffect(()=>{
  const cancel=(event:KeyboardEvent)=>{if(event.key==="Escape"&&gesture.current){event.preventDefault();clearDrag();}};
  window.addEventListener("keydown",cancel);
  return()=>window.removeEventListener("keydown",cancel);
 },[setDropPosition]);
 useEffect(()=>{clearDrag();},[layoutSize,maximized]);
 useEffect(()=>{
  let active=true;
  async function load(){try{const p=await api<Policy>(`/tenants/${tenant.id}/live-policy`,"GET",undefined,true);if(active){setPolicy(p);setError("");}}catch(e){if(active)setError((e as Error).message);}}
  void load();const unsubscribe=onEnvironmentChange(["CameraStatusChanged"],()=>void load());
  return()=>{active=false;unsubscribe();};
 },[tenant.id]);
 const visiblePositions=positions.slice(0,layoutSize*layoutSize);
 const occupied=visiblePositions.filter(p=>p.cameraId!=="").length;
 const rule=policy?.rules.find(r=>Math.max(1,occupied)>=r.min&&Math.max(1,occupied)<=r.max);
 const focusRule=policy?.rules.find(r=>r.min<=1&&r.max>=1);
 const change=(i:number,value:Partial<Position>)=>setPositions(current=>current.map((p,n)=>n===i?{...p,...value}:p));
 const closeCamera=(i:number)=>{change(i,{cameraId:"",profile:"",fallback:[]});if(maximized===i)setMaximized(null);setPendingCameraId(null);};
 return <section className="live-workspace" data-videowall={fullscreen} aria-label="Visualização ao vivo">
 <LiveFullscreenControls/>
 {fullscreenError&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{fullscreenError}</AlertDescription></Alert>}
 {error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
 <div className="live-stage">
 <div className="live-grid" data-maximized={maximized!==null} style={{gridTemplateColumns:`repeat(${maximized!==null?1:layoutSize},minmax(0,1fr))`,gridTemplateRows:`repeat(${maximized!==null?1:layoutSize},minmax(0,1fr))`}}>
 {/* Keep DOM order stable; CSS places each tile at its current mosaic position. */}
 {visiblePositions.map((p,i)=>({p,i})).sort((a,b)=>a.p.tileId-b.p.tileId).map(({p,i})=>{const camera=cameras.find(c=>c.id===p.cameraId&&c.capabilities.live);const profile=p.profile||rule?.profile||"main";return <LiveCameraMenu key={p.tileId} occupied={!!camera} maximized={maximized===i} onOpen={()=>setActivePosition(i)} onMaximize={()=>setMaximized(maximized===i?null:i)} onClose={()=>closeCamera(i)} onRecording={()=>{if(camera)setRecordingCamera({id:camera.id,name:camera.name});}} profiles={camera?.profiles??[]} profile={p.profile} onProfileChange={value=>change(i,{profile:value,fallback:[]})}><Card className="live-cell gap-0 p-0" style={{gridRow:maximized!==null?1:Math.floor(i/layoutSize)+1,gridColumn:maximized!==null?1:i%layoutSize+1}} data-live-position={i} data-occupied={!!camera} data-draggable={!!camera&&maximized===null} data-dragging={draggedPosition===i} data-active={activePosition===i} data-highlighted={highlightedPosition===i} hidden={maximized!==null&&maximized!==i} data-drop-target={dropPosition===i} role="group" aria-label={`Posição ${i+1} do mosaico`} tabIndex={0} onClickCapture={event=>{if(suppressClick.current){event.preventDefault();event.stopPropagation();return;}setActivePosition(i);}} onFocusCapture={()=>setActivePosition(i)}
  onPointerDown={event=>{
   if(event.button!==0||!event.isPrimary)return;
   suppressClick.current=false;
   if(!camera||maximized!==null||(event.target as HTMLElement).closest("button,input,select,a,[role=menu]"))return;
   gesture.current={x:event.clientX,y:event.clientY,from:i,pointerId:event.pointerId,element:event.currentTarget,moved:false};
   event.currentTarget.setPointerCapture(event.pointerId);
  }}
  onPointerMove={event=>{
   const current=gesture.current;if(!current||current.pointerId!==event.pointerId)return;
   if(!current.moved&&Math.hypot(event.clientX-current.x,event.clientY-current.y)<6)return;
   event.preventDefault();current.moved=true;suppressClick.current=true;
   setPendingCameraId(null);setDraggedPosition(current.from);
   const target=targetPosition(event.clientX,event.clientY);
   setDropPosition(target===current.from?null:target);
  }}
  onPointerUp={event=>{
   const current=gesture.current;if(!current||current.pointerId!==event.pointerId)return;
   if(current.moved){const target=targetPosition(event.clientX,event.clientY);if(target!==null)moveCamera(current.from,target);}
   clearDrag();
  }}
  onPointerCancel={clearDrag} onLostPointerCapture={()=>{if(gesture.current)clearDrag();}} onContextMenu={clearDrag}
  onDoubleClick={event=>{if(!suppressClick.current&&camera&&!(event.target as HTMLElement).closest("button,[role=menu]"))setMaximized(maximized===i?null:i);}}
  onKeyDown={event=>{if(event.target!==event.currentTarget)return;if((event.key==="Enter"||event.key===" ")&&pendingCameraId){event.preventDefault();chooseCamera(pendingCameraId,i);setPendingCameraId(null);}if(event.key==="Escape"&&!fullscreen){setPendingCameraId(null);setMaximized(null);}if(event.key==="Enter"&&!pendingCameraId&&camera){event.preventDefault();setMaximized(maximized===i?null:i);}}}>
  {camera?<LiveCameraViewport key={JSON.stringify([tenant.id,camera.id,camera.sourceVersion,camera.connectionRevision])} name={camera.name} position={i} onClose={()=>closeCamera(i)}>{(policy||p.profile?<LivePlayer key={JSON.stringify([tenant.id,camera.id,camera.sourceVersion,camera.connectionRevision,camera.effectiveActive])} tenant={tenant.id} camera={camera} profile={profile} fallback={p.profile?[]:(rule?.fallback??[])} focusProfile={maximized===i?(p.profile||focusRule?.profile||"main"):undefined} focusFallback={p.profile?[]:(focusRule?.fallback??[])} visible={maximized===null||maximized===i} queue={transitionQueue}/>:<div className="video-state" role="status"><LoaderCircle className="animate-spin" aria-label="Carregando qualidade do mosaico…"/></div>)}</LiveCameraViewport>:<div className="video-area"><Empty className="video-message"><Plus aria-hidden="true"/><p>{p.cameraId?"Sem acesso à câmera":"Arraste uma câmera para cá"}</p></Empty></div>}
 </Card></LiveCameraMenu>})}
 </div>
 <aside id="live-camera-panel" data-collapsed={!cameraListOpen} className="live-camera-panel" aria-label="Visualizações e câmeras">
  <div id="live-camera-list-content" className="live-camera-list-content" hidden={!cameraListOpen}><LiveCameraList/></div>
 </aside>
 </div>
 {recordingCamera&&<LiveRecordingPanel tenantId={tenant.id} camera={recordingCamera} onClose={()=>setRecordingCamera(null)}/>}
 </section>;
}
