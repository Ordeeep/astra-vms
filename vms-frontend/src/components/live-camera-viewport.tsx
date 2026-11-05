"use client";

import {useEffect,useRef,useState,type CSSProperties,type ReactNode} from "react";
import {LoaderCircle,Minus,Plus,RotateCcw,Search,X} from "lucide-react";

type View={scale:number;x:number;y:number};
const original:View={scale:1,x:0,y:0};
const clamp=(value:number,limit:number)=>Math.max(-limit,Math.min(limit,value));
function bounded(view:View):View{
 const scale=Math.max(1,Math.min(8,view.scale)),limit=(scale-1)*50;
 return {scale,x:clamp(view.x,limit),y:clamp(view.y,limit)};
}

// Owns only the local framing: the player stays mounted throughout zoom/pan.
export function LiveCameraViewport({name,position,onClose,children}:{name:string;position:number;onClose:()=>void;children:ReactNode}){
 const [enabled,setEnabled]=useState(false),[view,setView]=useState<View>(original);
 const area=useRef<HTMLDivElement>(null);
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
   <span title={name}>{name}</span>
   <span className="live-stream-change" role="status" aria-live="polite"><LoaderCircle className="animate-spin" aria-hidden="true"/><span>Alterando stream…</span></span>
   <button type="button" className="live-camera-close live-camera-zoom" aria-label={`Zoom digital de ${name}`} aria-pressed={enabled} title={enabled?"Desativar zoom digital":"Ativar zoom digital"} onClick={event=>{event.stopPropagation();if(enabled)disable();else{setView(original);setEnabled(true);}}}><Search aria-hidden="true"/></button>
   <button type="button" className="live-camera-close" aria-label={`Fechar câmera da posição ${position+1}`} title="Fechar câmera" onClick={event=>{event.stopPropagation();onClose();event.currentTarget.closest<HTMLElement>("[data-live-position]")?.focus();}}><X aria-hidden="true"/></button>
  </div>
  <div ref={area} className="video-area live-digital-zoom" data-zoom-enabled={enabled} data-zoomed={enabled&&view.scale>1} style={{"--digital-zoom-scale":view.scale,"--digital-zoom-x":`${view.x}%`,"--digital-zoom-y":`${view.y}%`} as CSSProperties}
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
    if(event.key==="Escape"){event.preventDefault();event.stopPropagation();disable();event.currentTarget.closest(".live-cell")?.querySelector<HTMLButtonElement>(".live-camera-zoom")?.focus();return;}
    if(event.target!==event.currentTarget)return;
    if(!["+","=","-","0","ArrowLeft","ArrowRight","ArrowUp","ArrowDown"].includes(event.key))return;
    event.preventDefault();event.stopPropagation();
    if(event.key==="+"||event.key==="=")zoom(.25);
    else if(event.key==="-")zoom(-.25);
    else if(event.key==="0")setView(original);
    else setView(previous=>bounded({...previous,x:previous.x+(event.key==="ArrowLeft"?5:event.key==="ArrowRight"?-5:0),y:previous.y+(event.key==="ArrowUp"?5:event.key==="ArrowDown"?-5:0)}));
   }}>
   {children}
   {enabled&&<div className="live-digital-zoom-controls" aria-label="Controles de zoom digital" onPointerDown={event=>event.stopPropagation()} onDoubleClick={event=>event.stopPropagation()}>
    <button type="button" aria-label="Diminuir zoom digital" title="Diminuir" disabled={view.scale<=1} onClick={()=>zoom(-.25)}><Minus aria-hidden="true"/></button>
    <output aria-label="Ampliação digital">{view.scale.toLocaleString("pt-BR",{maximumFractionDigits:2})}×</output>
    <button type="button" aria-label="Aumentar zoom digital" title="Aumentar" disabled={view.scale>=8} onClick={()=>zoom(.25)}><Plus aria-hidden="true"/></button>
    <button type="button" aria-label="Restaurar enquadramento digital" title="Restaurar enquadramento" onClick={()=>setView(original)}><RotateCcw aria-hidden="true"/></button>
   </div>}
  </div>
 </>;
}
