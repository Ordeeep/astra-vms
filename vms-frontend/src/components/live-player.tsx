"use client";

import {useEffect,useState} from "react";
import {LoaderCircle,VideoOff} from "lucide-react";
import {Button} from "@/components/ui/button";
import type {Camera} from "@/components/management";
import {LiveStream,type LiveStreamRequest} from "@/components/live-stream";
import type {LiveTransitionQueue} from "@/lib/live-transition-queue";

type Entry=LiveStreamRequest&{ready?:boolean;actualProfile?:string;serialized:boolean};
type Model={active:Entry|null;pending:Entry|null;message:string;failed:boolean};
const requestKey=(profile:string,fallback:string[])=>JSON.stringify([profile,fallback]);

function ContinuousVideo({tenant,camera,profile,fallback,queue,visible,overlay=false,underlayReady=false,onAvailability,onDenied}:{
 tenant:string;camera:Camera;profile:string;fallback:string[];queue:LiveTransitionQueue;visible:boolean;
 overlay?:boolean;underlayReady?:boolean;onAvailability?:(ready:boolean,profile?:string)=>void;onDenied:()=>void;
}){
 const [attempt,setAttempt]=useState(0),target=requestKey(profile,fallback),key=JSON.stringify([target,attempt]);
 const [model,setModel]=useState<Model>(()=>({active:null,pending:{key,profile,fallback,serialized:overlay},message:"Conectando…",failed:false}));
 useEffect(()=>{
  setModel(previous=>{
   if(previous.active?.key===key)return {...previous,pending:null,message:"",failed:false};
   if(previous.pending?.key===key)return previous;
   return {active:previous.active,pending:{key,profile,fallback:JSON.parse(target)[1],serialized:overlay||!!previous.active},message:"Conectando…",failed:false};
  });
 },[key,profile,target,overlay]);
 const available=!!model.active?.ready;
 const actualProfile=model.active?.actualProfile;
 useEffect(()=>{onAvailability?.(available,actualProfile);},[available,actualProfile,onAvailability]);
 const ready=(entry:Entry,actualProfile:string)=>setModel(previous=>{
  if(previous.active?.key===entry.key)return {...previous,active:{...previous.active,ready:true,actualProfile},message:"",failed:false};
  if(previous.pending?.key!==entry.key)return previous;
  return {active:{...entry,ready:true,actualProfile},pending:null,message:"",failed:false};
 });
 const waiting=(entry:Entry,message:string)=>setModel(previous=>{
  if(previous.active?.key===entry.key)return {...previous,active:{...previous.active,ready:false},message};
  return previous.pending?.key===entry.key?{...previous,message}:previous;
 });
 const failure=(entry:Entry,message:string,denied:boolean)=>{
  if(denied){onDenied();return;}
  setModel(previous=>{
   if(previous.pending?.key===entry.key)return {...previous,pending:null,message:previous.active?.ready||underlayReady?"Qualidade solicitada indisponível. Mantendo a transmissão atual.":message,failed:true};
   if(previous.active?.key===entry.key)return {...previous,active:null,message,failed:true};
   return previous;
  });
 };
 const showing=available||underlayReady;
 return <div className="live-video-stack" data-overlay={overlay} data-changing={!!model.pending&&showing}>
  {[model.active,model.pending].filter((entry):entry is Entry=>!!entry).map(entry=><LiveStream key={entry.key} tenant={tenant} camera={camera} request={entry} presented={model.active?.key===entry.key&&!!entry.ready} visible={visible&&model.active?.key===entry.key} queue={entry.serialized?queue:undefined} onReady={actual=>ready(entry,actual)} onWaiting={message=>waiting(entry,message)} onFailure={(message,denied)=>failure(entry,message,denied)}/>)}
  {model.message&&(!showing?<div className="video-state" role="status">{model.failed?<Button className="video-failure" onClick={()=>setAttempt(value=>value+1)} variant="ghost" size="icon" aria-label={`Falha na conexão de ${camera.name}. Reconectar`} title={`${model.message} Clique para reconectar.`}><VideoOff aria-hidden="true"/></Button>:<LoaderCircle className="animate-spin" aria-label={model.message}/>}</div>:model.failed?<div className="live-video-notice" role="status"><span>{model.message}</span><button type="button" onClick={()=>setAttempt(value=>value+1)}>Tentar novamente</button></div>:null)}
 </div>;
}

export function LivePlayer({tenant,camera,profile,fallback,focusProfile,focusFallback,visible,queue}:{
 tenant:string;camera:Camera;profile:string;fallback:string[];focusProfile?:string;focusFallback?:string[];visible:boolean;queue:LiveTransitionQueue;
}){
 const [base,setBase]=useState<{ready:boolean;profile?:string}>({ready:false});
 const [focusReady,setFocusReady]=useState(false),[denied,setDenied]=useState(false);
 // Stable callback identities keep availability reports from causing render loops.
 const [baseAvailability]=useState(()=>(ready:boolean,actualProfile?:string)=>setBase(previous=>previous.ready===ready&&previous.profile===actualProfile?previous:{ready,profile:actualProfile}));
 const [focusAvailability]=useState(()=>(ready:boolean)=>setFocusReady(ready));
 const needsFocus=!!focusProfile&&focusProfile!==profile&&focusProfile!==base.profile;
 useEffect(()=>{if(!needsFocus)setFocusReady(false);},[needsFocus]);
 if(!camera.effectiveActive||denied)return <div className="video-state" role="status"><VideoOff aria-label={denied?"Acesso à transmissão encerrado":"Câmera desativada"}/></div>;
 return <>
  <ContinuousVideo tenant={tenant} camera={camera} profile={profile} fallback={fallback} queue={queue} visible={visible&&!(needsFocus&&focusReady)} onAvailability={baseAvailability} onDenied={()=>setDenied(true)}/>
  {needsFocus&&<ContinuousVideo tenant={tenant} camera={camera} profile={focusProfile!} fallback={focusFallback??[]} queue={queue} visible={visible} overlay underlayReady={base.ready} onAvailability={focusAvailability} onDenied={()=>setDenied(true)}/>}
 </>;
}
