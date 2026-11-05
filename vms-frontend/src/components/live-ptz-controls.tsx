"use client";

import {useEffect,useRef,useState,type ButtonHTMLAttributes} from "react";
import {useLiveView} from "@/components/live-view-context";
import {type Camera} from "@/components/management";
import {captureScopedAPI} from "@/lib/api";
import {PTZControlSession,type PTZAction,type PTZResources} from "@/lib/ptz-control";
import { ArrowDownLeft, ArrowDownRight, ArrowUpLeft, ArrowUpRight, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, House, Minus, Plus } from "lucide-react";
import "./live-ptz-controls.css";

const directions = [
  { id: "up-left", label: "Cima e esquerda", icon: ArrowUpLeft, x: -1, y: -1 },
  { id: "up", label: "Cima", icon: ChevronUp, x: 0, y: -1 },
  { id: "up-right", label: "Cima e direita", icon: ArrowUpRight, x: 1, y: -1 },
  { id: "left", label: "Esquerda", icon: ChevronLeft, x: -1, y: 0 },
  { id: "home", label: "Posição inicial", icon: House, x: 0, y: 0 },
  { id: "right", label: "Direita", icon: ChevronRight, x: 1, y: 0 },
  { id: "down-left", label: "Baixo e esquerda", icon: ArrowDownLeft, x: -1, y: 1 },
  { id: "down", label: "Baixo", icon: ChevronDown, x: 0, y: 1 },
  { id: "down-right", label: "Baixo e direita", icon: ArrowDownRight, x: 1, y: 1 },
];

export function LivePTZControls() {
  const {tenantId,cameras,positions,activePosition}=useLiveView();
  const camera=cameras.find(camera=>camera.id===positions[activePosition]?.cameraId);
  const key=[tenantId,camera?.id,camera?.sourceVersion,camera?.connectionRevision,camera?.siteId,camera?.effectiveActive,camera?.capabilities.live,camera?.capabilities["ptz.control"]].join("/");
  return <PTZPanel key={key} tenantId={tenantId} camera={camera}/>;
}

function PTZPanel({tenantId,camera}:{tenantId?:string;camera?:Camera}) {
  const [speed,setSpeed]=useState(3);
  const [feedback,setFeedback]=useState("");
  const [resources,setResources]=useState<PTZResources|null>(null);
  const [loading,setLoading]=useState(false);
  const [retry,setRetry]=useState(0);
  const [loadError,setLoadError]=useState("");
  const [active,setActive]=useState(false);
  const control=useRef<PTZControlSession|null>(null);
  const inputs=useRef(new Map<string,{generation:number;label:string}>());
  const reason=!camera?"Selecione um quadro com câmera no mosaico.":!camera.effectiveActive?"Esta câmera está desativada.":!camera.capabilities.live||!camera.capabilities["ptz.control"]?"Sem permissão para controlar o PTZ desta câmera.":"";
  useEffect(()=>{
    if(reason||!camera||!tenantId)return;
    let canceled=false;
    const request=captureScopedAPI(),path=`/tenants/${tenantId}/cameras/${camera.id}/ptz`;
    setLoading(true);setLoadError("");setResources(null);setFeedback("");
    void request<PTZResources>(path).then(next=>{
      if(canceled)return;
      setResources(next);
      control.current=new PTZControlSession((method,action,id)=>request(method==="POST"?path+"/commands":path+"/commands/"+id,method,method==="POST"?{...action,revision:next.revision}:undefined),setFeedback,setActive);
    }).catch(error=>{if(!canceled)setLoadError((error as Error).message);}).finally(()=>{if(!canceled)setLoading(false);});
    const end=()=>control.current?.end();
    const visibility=()=>{if(document.hidden)end();};
    window.addEventListener("blur",end);window.addEventListener("pagehide",end);document.addEventListener("visibilitychange",visibility);
    return ()=>{canceled=true;control.current?.dispose();control.current=null;window.removeEventListener("blur",end);window.removeEventListener("pagehide",end);document.removeEventListener("visibilitychange",visibility);};
  },[tenantId,camera?.id,reason,retry]);

  const end=()=>control.current?.end();
  const start=(action:PTZAction,label:string)=>control.current?.start(action,label);
  const beginInput=(key:string,action:PTZAction,label:string)=>{const generation=start(action,label);if(generation!==undefined)inputs.current.set(key,{generation,label});};
  const endInput=(key:string)=>{const input=inputs.current.get(key);if(input){inputs.current.delete(key);control.current?.end(input.generation);}};
  const hold=(action:PTZAction,label:string):ButtonHTMLAttributes<HTMLButtonElement>=>({
    onPointerDown:event=>{if(event.button!==0)return;event.preventDefault();event.currentTarget.focus();event.currentTarget.setPointerCapture(event.pointerId);beginInput(`pointer:${event.pointerId}`,action,label);},
    onPointerUp:event=>endInput(`pointer:${event.pointerId}`),
    onPointerCancel:event=>endInput(`pointer:${event.pointerId}`),
    onLostPointerCapture:event=>endInput(`pointer:${event.pointerId}`),
    onBlur:()=>{for(const [key,input] of inputs.current){if(input.label===label)endInput(key);}},
    onKeyDown:event=>{if(event.key!==" "&&event.key!=="Enter")return;event.preventDefault();if(!event.repeat)beginInput(`key:${label}:${event.key}`,action,label);},
    onKeyUp:event=>{if(event.key===" "||event.key==="Enter"){event.preventDefault();endInput(`key:${label}:${event.key}`);}},
    onContextMenu:event=>event.preventDefault(),
    // Assistive technology can activate without pointer/key events.
    onClick:event=>{if(event.detail===0){const generation=start(action,label);if(generation!==undefined)setTimeout(()=>control.current?.end(generation),200);}},
  });
  const unavailable=!!reason||loading||!resources||!!loadError;
  const nothing=resources&&!resources.panTilt&&!resources.zoom&&!resources.home;
  const status=reason||loadError||(loading?"Consultando recursos PTZ…":nothing?"Esta câmera não oferece controle PTZ.":feedback||"Mantenha pressionado para mover; solte para parar.");
  return <div className="live-ptz-controls" data-controlling={active} role="group" aria-label={camera?`Controle PTZ de ${camera.name}`:"Controle PTZ"}>
    <div className="live-ptz-control-row">
      <label className="live-ptz-column live-ptz-speed" title="Velocidade">
        <span aria-hidden="true">Vel.</span>
        <input type="range" min={1} max={5} step={1} value={speed} disabled={unavailable||(!resources?.panTilt&&!resources?.zoom)} aria-label="Velocidade PTZ" aria-valuetext={`${speed} de 5`}
          onChange={event=>{end();setSpeed(Number(event.target.value));}}/>
        <span className="live-ptz-value" aria-hidden="true">{speed}</span>
      </label>
      <div className="live-ptz-wheel" role="group" aria-label="Direções PTZ">
        <span className="live-ptz-cross live-ptz-cross-vertical" aria-hidden="true"/>
        <span className="live-ptz-cross live-ptz-cross-horizontal" aria-hidden="true"/>
        {directions.map(({id,label,icon:Icon,x,y})=>{
          const diagonal=x!==0&&y!==0;
          const radius=48/148*100/(diagonal?Math.SQRT2:1);
          const home=id==="home";
          return <button key={id} type="button" className="live-ptz-direction" data-diagonal={diagonal} data-home={home}
            style={{left:`${50+x*radius}%`,top:`${50+y*radius}%`}}
            disabled={unavailable||!(home?resources?.home:resources?.panTilt)}
            aria-label={home?label:`Mover para ${label.toLocaleLowerCase("pt-BR")}`} title={label}
            {...(home?{onClick:()=>start({home:true},"Retorno à posição inicial solicitado.")} :hold({pan:x*speed/5,tilt:-y*speed/5},`Movendo para ${label.toLocaleLowerCase("pt-BR")}.`))}>
            <Icon aria-hidden="true"/>
          </button>;
        })}
      </div>
      <div className="live-ptz-column live-ptz-zoom" role="group" aria-label="Zoom PTZ">
        <span>Zoom</span>
        <button type="button" aria-label="Aumentar zoom" title="Aumentar zoom" disabled={unavailable||!resources?.zoom} {...hold({zoom:speed/5},"Aumentando zoom óptico.")}><Plus aria-hidden="true"/></button>
        <button type="button" aria-label="Reduzir zoom" title="Reduzir zoom" disabled={unavailable||!resources?.zoom} {...hold({zoom:-speed/5},"Reduzindo zoom óptico.")}><Minus aria-hidden="true"/></button>
      </div>
    </div>
    <p className="sr-only" role="status" aria-live="polite">{status}</p>
    {!!loadError&&<button type="button" className="live-ptz-retry" onClick={()=>setRetry(value=>value+1)}>Tentar novamente</button>}
  </div>;
}
