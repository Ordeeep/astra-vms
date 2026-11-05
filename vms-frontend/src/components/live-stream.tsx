"use client";

import {useEffect, useRef} from "react";
import type {Camera} from "@/components/management";
import {api, ApiError, signalMedia} from "@/lib/api";
import {compatibleReceiverCodecs, liveVideoCodecs} from "@/lib/video-compatibility";
import type {LiveTransitionQueue} from "@/lib/live-transition-queue";

type Session = {id:string;profile:string;fallback:boolean;signaling:string;delivery?:"native"|"compatible"};
export type LiveStreamRequest = {key:string;profile:string;fallback:string[]};

// One mounted instance owns a requested profile and its authorized fallback
// chain. A quality transition creates a sibling without interrupting this one.
export function LiveStream({tenant,camera,request,presented,visible,queue,onReady,onWaiting,onFailure}:{
 tenant:string;camera:Camera;request:LiveStreamRequest;presented:boolean;visible:boolean;queue?:LiveTransitionQueue;
 onReady:(profile:string)=>void;onWaiting:(message:string)=>void;onFailure:(message:string,denied:boolean)=>void;
}) {
 const video=useRef<HTMLVideoElement>(null);
 const releasePreparation=useRef<(()=>void)|undefined>(undefined);
 const handlers=useRef({onReady,onWaiting,onFailure,visible});
 handlers.current={onReady,onWaiting,onFailure,visible};
 const {profile}=request,fallbackKey=JSON.stringify(request.fallback);
 useEffect(()=>{
  let active=true,run=0,retried=false,pc:RTCPeerConnection|undefined,session:Session|undefined,abort:AbortController|undefined;
  const candidates=[...new Set<string>([profile,...JSON.parse(fallbackKey)])];
  let candidate=0;
  let timer:ReturnType<typeof setInterval>|undefined,watchdog:ReturnType<typeof setInterval>|undefined,startup:ReturnType<typeof setTimeout>|undefined;
  let release:(()=>void)|undefined,frameCallback:number|undefined;
  const closeTransport=()=>{
   run++;
   if(timer)clearInterval(timer);if(watchdog)clearInterval(watchdog);if(startup)clearTimeout(startup);
   timer=undefined;watchdog=undefined;startup=undefined;
   abort?.abort();release?.();release=undefined;releasePreparation.current=undefined;
   if(pc){pc.onconnectionstatechange=null;pc.ontrack=null;pc.close();pc=undefined;}
   const element=video.current;
   if(element){if(frameCallback!==undefined)element.cancelVideoFrameCallback?.(frameCallback);element.onerror=null;element.pause();element.srcObject=null;}
   frameCallback=undefined;
   const previous=session;session=undefined;
   if(previous)void api(`/tenants/${tenant}/live-sessions/${previous.id}`,"DELETE",undefined,true).catch(()=>{});
  };
  const fail=(message:string,canAdapt:boolean,denied=false,canFallback=true)=>{
   if(!active)return;
   closeTransport();
   if(!denied&&canAdapt&&!retried){retried=true;handlers.current.onWaiting("Adaptando vídeo ao navegador…");void open(true);return;}
   if(!denied&&canFallback&&candidate+1<candidates.length){
    candidate++;retried=false;handlers.current.onWaiting("Tentando outro perfil de vídeo…");void open();return;
   }
   handlers.current.onFailure(message,denied);
  };
  async function open(forceCompatibility=false){
   const generation=++run,current=()=>active&&generation===run;
   const controller=new AbortController();abort=controller;
   let currentSession:Session|undefined;
   try{
    if(queue){
     handlers.current.onWaiting("Preparando qualidade…");release=await queue.acquire(controller.signal);
     if(!current()){release();release=undefined;return;}
     releasePreparation.current=()=>{release?.();release=undefined;};
    }
    startup=setTimeout(()=>{if(current())fail("Não foi possível preparar o vídeo no prazo esperado.",false);},18000);
    const connection=new RTCPeerConnection({iceServers:[]});pc=connection;
    currentSession=await api<Session>(`/tenants/${tenant}/live-sessions`,"POST",{cameraId:camera.id,profile:candidates[candidate],fallback:candidates.slice(candidate+1),codecs:liveVideoCodecs(),forceCompatibility},true);
    if(!current()){void api(`/tenants/${tenant}/live-sessions/${currentSession.id}`,"DELETE",undefined,true).catch(()=>{});return;}
    session=currentSession;
    // The backend can skip unconfigured profiles before signaling. Continue
    // after the actual profile on failure instead of revisiting skipped ones.
    candidate=Math.max(candidate,candidates.indexOf(session.profile));
    let renewing=false,ready=false,frames=0,lastFrame=performance.now(),lastStats=0,packets=-1,checking=false,wasVisible=handlers.current.visible;
    const readyFrame=()=>{
     if(!current()||ready)return;
     const element=video.current;
     if(!element||element.readyState<2||!element.videoWidth||element.paused)return;
     ready=true;lastFrame=performance.now();if(startup)clearTimeout(startup);startup=undefined;
     handlers.current.onReady(currentSession!.profile);
    };
    const renew=async()=>{
     if(renewing||!current())return;renewing=true;
     try{await api(`/tenants/${tenant}/live-sessions/${currentSession!.id}`,"PUT",{},true);}
     catch(error){if(current())fail("Transmissão encerrada. Verifique o acesso ou a conexão.",false,error instanceof ApiError&&[401,403].includes(error.status),false);}
     finally{renewing=false;}
    };
    timer=setInterval(()=>void renew(),400);void renew();
    connection.ontrack=e=>{
     if(!current()||!video.current)return;
     const element=video.current;
     element.srcObject=e.streams[0]??new MediaStream([e.track]);
     element.onerror=()=>{if(current())fail("Não foi possível reproduzir o vídeo.",[3,4].includes(element.error?.code??0));};
     if(element.requestVideoFrameCallback)frameCallback=element.requestVideoFrameCallback(()=>readyFrame());
     void element.play().catch(error=>{if(current())fail("Não foi possível iniciar o vídeo.",error?.name==="NotSupportedError");});
    };
    connection.onconnectionstatechange=()=>{if(current()&&connection.connectionState==="failed")fail("Transmissão encerrada.",true);};
    const transceiver=connection.addTransceiver("video",{direction:"recvonly"});
    const codecs=compatibleReceiverCodecs(forceCompatibility||session.delivery==="compatible");
    if(codecs.length&&typeof transceiver.setCodecPreferences==="function"){
     try{transceiver.setCodecPreferences(codecs);}catch{/* Keep the browser's SDP defaults when preference hints are rejected. */}
    }
    await connection.setLocalDescription(await connection.createOffer());
    await new Promise<void>((resolve,reject)=>{
     if(connection.iceGatheringState==="complete"){resolve();return;}
     const timeout=setTimeout(()=>reject(new Error("A negociação de rede excedeu o prazo.")),6000);
     connection.addEventListener("icegatheringstatechange",()=>{if(connection.iceGatheringState==="complete"){clearTimeout(timeout);resolve();}});
     controller.signal.addEventListener("abort",()=>{clearTimeout(timeout);reject(new DOMException("Encerrado","AbortError"));},{once:true});
    });
    if(!current())return;
    const answer=await signalMedia(session.signaling,connection.localDescription!.sdp,controller.signal);
    if(!current())return;
    await connection.setRemoteDescription({type:"answer",sdp:answer});
    if(!current())return;
    lastFrame=performance.now();
    watchdog=setInterval(()=>{
     if(!current())return;
     const element=video.current,quality=element?.getVideoPlaybackQuality?.(),count=quality?.totalVideoFrames??0,now=performance.now();
     if(count>frames||(!quality&&element&&element.readyState>=2&&!element.paused)){
      frames=count;lastFrame=now;readyFrame();
     }
     const shown=handlers.current.visible&&!document.hidden;
     if(shown&&!wasVisible)lastFrame=now;wasVisible=shown;
     // Browsers may suspend presentation of hidden videos. Incoming RTP, rather
     // than presented frames, determines whether a background stream is alive.
     if(!shown&&!checking&&now-lastStats>=1000){
      checking=true;lastStats=now;
      void connection.getStats().then(stats=>{
       if(!current())return;
       let received=0;
       stats.forEach(report=>{if(report.type==="inbound-rtp"&&(report.kind==="video"||report.mediaType==="video"))received+=report.packetsReceived??0;});
       if(received>packets){packets=received;lastFrame=performance.now();}
      }).catch(()=>{}).finally(()=>{checking=false;});
     }
     if(now-lastFrame>=12000)fail("Sem novos quadros. Verifique a câmera ou a conexão.",true);
    },250);
   }catch(error){
    if(!current())return;
    const denied=error instanceof ApiError&&[401,403].includes(error.status);
    const mediaFailure=!(error instanceof ApiError)||[400,406,415,422,500,502,503,504].includes(error.status);
    fail((error as Error).message,!denied&&!!currentSession&&(!(error instanceof ApiError)||[400,406,415,422,500,502].includes(error.status)),denied,mediaFailure);
   }
  }
  void open();
  return()=>{active=false;closeTransport();};
 },[tenant,camera.id,camera.sourceVersion,camera.connectionRevision,profile,fallbackKey,queue]);
 // Wait for promotion to commit and the old stream's cleanup before allowing
 // another preparation to consume capacity.
 useEffect(()=>{if(presented){releasePreparation.current?.();releasePreparation.current=undefined;}},[presented]);
 return <video ref={video} className="live-stream-video" data-presented={presented} autoPlay muted playsInline aria-hidden={!presented} aria-label={`Ao vivo: ${camera.name}`}/>;
}
