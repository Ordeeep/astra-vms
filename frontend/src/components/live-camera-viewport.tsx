"use client";

import {useEffect,useRef,useState,type CSSProperties,type MouseEvent,type ReactNode} from "react";
import {Film,LoaderCircle,Search,Timeline,Video} from "lucide-react";
import {Slider} from "@/components/ui/slider";
import {Button} from "@/components/ui/button";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";

type View={scale:number;x:number;y:number};
const original:View={scale:1,x:0,y:0};
const clamp=(value:number,limit:number)=>Math.max(-limit,Math.min(limit,value));
function bounded(view:View):View{
 const scale=Math.max(1,Math.min(8,view.scale)),limit=(scale-1)*50;
 return {scale,x:clamp(view.x,limit),y:clamp(view.y,limit)};
}

// Owns only the local framing: the player stays mounted throughout zoom/pan.
export function LiveCameraViewport({name,debugCameraId,showShortcuts,playback,onTogglePlayback,children}:{
 name:string;debugCameraId?:string;showShortcuts:boolean;children:ReactNode;
 playback?:{timelineVisible:boolean;onToggleTimeline:()=>void};onTogglePlayback:()=>void;
}){
 const [enabled,setEnabled]=useState(false),[view,setView]=useState<View>(original);
 const displayId=debugCameraId&&/^[\da-f]{48}$/i.test(debugCameraId)?debugCameraId.slice(0,8):debugCameraId;
 const area=useRef<HTMLDivElement>(null);
 const zoomTrigger=useRef<HTMLButtonElement>(null);
 const drag=useRef<{id:number;x:number;y:number;view:View}|null>(null);
 const zoom=(delta:number,anchor={x:0,y:0})=>setView(previous=>{
  const scale=Math.max(1,Math.min(8,previous.scale+delta)),ratio=scale/previous.scale;
  // Keep the image point under the cursor at the same viewport position.
  return bounded({scale,x:anchor.x-(anchor.x-previous.x)*ratio,y:anchor.y-(anchor.y-previous.y)*ratio});
 });
 const clearDrag=()=>{
  const current=drag.current;drag.current=null;
  if(current&&area.current?.hasPointerCapture(current.id))area.current.releasePointerCapture(current.id);
 };
 const disable=()=>{clearDrag();setEnabled(false);setView(original);};
 const toggleZoom=(event:MouseEvent<HTMLButtonElement>)=>{
  event.stopPropagation();
  zoomTrigger.current=event.currentTarget;
  if(enabled)disable();
  else setEnabled(true);
 };
 const zoomHint=enabled?"Desativar zoom digital":"Ativar zoom digital";
 const timelineHint=playback?.timelineVisible?"Ocultar linha do tempo":"Mostrar linha do tempo";
 useEffect(()=>{
  const element=area.current;
  if(!enabled||!element)return;
  const wheel=(event:WheelEvent)=>{
   // Leave browser zoom shortcuts available.
   if(event.ctrlKey||event.metaKey)return;
   event.preventDefault();event.stopPropagation();
   const rect=element.getBoundingClientRect();
   if(event.deltaY&&rect.width&&rect.height)zoom(event.deltaY<0?.25:-.25,{
    x:(event.clientX-rect.left)/rect.width*100-50,
    y:(event.clientY-rect.top)/rect.height*100-50,
   });
  };
  element.addEventListener("wheel",wheel,{passive:false});
  return()=>element.removeEventListener("wheel",wheel);
 },[enabled]);
 return <>
  <div className="live-camera-title">
   {debugCameraId ? (
    <Tooltip>
     <TooltipTrigger asChild>
      <span className="live-camera-debug-name" tabIndex={0}>[{displayId}] {name}</span>
     </TooltipTrigger>
     <TooltipContent side="bottom" className="break-all">{`[${debugCameraId}] ${name}`}</TooltipContent>
    </Tooltip>
   ) : <span title={name}>{name}</span>}
   <span className="live-stream-change" role="status" aria-live="polite"><LoaderCircle className="animate-spin" aria-hidden="true"/><span>Alterando stream…</span></span>
  </div>
  <div ref={area} className="video-area live-digital-zoom" data-zoom-enabled={enabled} data-zoomed={enabled&&view.scale>1} style={{"--digital-zoom-scale":view.scale,"--digital-zoom-x":`${view.x}cqw`,"--digital-zoom-y":`${view.y}cqh`} as CSSProperties}
   tabIndex={enabled?0:undefined} role={enabled?"group":undefined} aria-label={enabled?`Zoom digital de ${name}. Use mais e menos para ampliar, setas para mover e Escape para sair.`:undefined}
   onPointerDown={event=>{
    if(!enabled||(event.target as HTMLElement).closest("button"))return;
    event.stopPropagation();
    if(event.button!==0||!event.isPrimary)return;
    event.preventDefault();event.currentTarget.focus();
    drag.current={id:event.pointerId,x:event.clientX,y:event.clientY,view};
    event.currentTarget.setPointerCapture(event.pointerId);
   }}
   onPointerMove={event=>{
    const current=drag.current;if(!current||current.id!==event.pointerId)return;
    event.stopPropagation();
    const rect=event.currentTarget.getBoundingClientRect();
    if(rect.width&&rect.height)setView(bounded({...current.view,x:current.view.x+(event.clientX-current.x)/rect.width*100,y:current.view.y+(event.clientY-current.y)/rect.height*100}));
   }}
   onPointerUp={clearDrag} onPointerCancel={clearDrag} onLostPointerCapture={clearDrag}
   onDoubleClick={event=>{if(enabled)event.stopPropagation();}}
   onKeyDown={event=>{
    if(!enabled)return;
    if(event.key==="Escape"){
     event.preventDefault();event.stopPropagation();disable();
     const trigger=showShortcuts?zoomTrigger.current:area.current?.closest<HTMLElement>("[data-live-position]");
     trigger?.focus();
     return;
    }
    if(event.target!==event.currentTarget)return;
    if(!["+","=","-","0","ArrowLeft","ArrowRight","ArrowUp","ArrowDown"].includes(event.key))return;
    event.preventDefault();event.stopPropagation();
    if(event.key==="+"||event.key==="=")zoom(.25);
    else if(event.key==="-")zoom(-.25);
    else if(event.key==="0")setView(original);
    else setView(previous=>bounded({...previous,x:previous.x+(event.key==="ArrowLeft"?5:event.key==="ArrowRight"?-5:0),y:previous.y+(event.key==="ArrowUp"?5:event.key==="ArrowDown"?-5:0)}));
   }}>
   {children}
   {(showShortcuts||enabled)&&
    <Tooltip>
     <TooltipTrigger asChild>
      <Button type="button" variant="ghost" size="icon-xs" className="live-camera-shortcut live-zoom-shortcut"
       aria-label={`Lupa de ${name}`} aria-pressed={enabled} onClick={toggleZoom}>
       <Search className="size-5" strokeWidth={2.5} aria-hidden="true"/>
      </Button>
     </TooltipTrigger>
     <TooltipContent side="right" sideOffset={6}>{zoomHint}</TooltipContent>
    </Tooltip>}
   {showShortcuts&&<>
    <Tooltip>
     <TooltipTrigger asChild>
      <Button type="button" variant="ghost" size="icon-xs" className="live-camera-shortcut live-recording-shortcut"
       aria-label={playback?`Voltar ao vivo em ${name}`:`Abrir gravações de ${name}`} onClick={onTogglePlayback}>
       {playback?<Video className="size-5" aria-hidden="true"/>:<Film className="size-5" aria-hidden="true"/>}
      </Button>
     </TooltipTrigger>
     <TooltipContent side="left" sideOffset={6}>{playback?"Voltar ao vivo":"Abrir gravações"}</TooltipContent>
    </Tooltip>
    {playback&&<Tooltip>
     <TooltipTrigger asChild>
      <Button type="button" variant="ghost" size="icon-xs" className="live-camera-shortcut live-timeline-shortcut"
       aria-label={timelineHint} aria-expanded={playback.timelineVisible} aria-controls="live-recording-timeline" onClick={playback.onToggleTimeline}>
       <Timeline className="size-5" aria-hidden="true"/>
      </Button>
     </TooltipTrigger>
     <TooltipContent side="left" sideOffset={6}>{timelineHint}</TooltipContent>
    </Tooltip>}
   </>}
   {enabled&&<div className="live-digital-zoom-controls" role="group" aria-label="Controles de zoom digital" onPointerDown={event=>event.stopPropagation()} onDoubleClick={event=>event.stopPropagation()}>
    <Slider orientation="vertical" min={1} max={8} step={0.25} value={[view.scale]} aria-label="Zoom digital" onValueChange={([scale])=>zoom(scale-view.scale)}/>
   </div>}
  </div>
 </>;
}
