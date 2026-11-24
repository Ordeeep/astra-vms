"use client";

import {useEffect,useState} from "react";
import {LoaderCircle,VideoOff} from "lucide-react";
import {Button} from "@/components/ui/button";
import type {Camera} from "@/components/management";
import {LiveStream,type LiveStreamRequest} from "@/components/live-stream";
import {LiveDebugMetrics} from "@/components/live-debug-metrics";
import type {LiveTransitionQueue} from "@/lib/live-transition-queue";

type Entry=LiveStreamRequest&{ready?:boolean;actualProfile?:string;serialized:boolean};
type Model={active:Entry|null;pending:Entry|null;message:string;failed:boolean};
const reconnectDelayMs=5000;
const requestKey=(profile:string,fallback:string[])=>JSON.stringify([profile,fallback]);

function ContinuousVideo({tenant,camera,profile,fallback,queue,visible,debugEnabled,onDenied}:{
 tenant:string;camera:Camera;profile:string;fallback:string[];queue:LiveTransitionQueue;visible:boolean;
 debugEnabled:boolean;
 onDenied:()=>void;
}){
 const [attempt,setAttempt]=useState(0),target=requestKey(profile,fallback),key=JSON.stringify([target,attempt]);
 const [model,setModel]=useState<Model>(()=>({active:null,pending:{key,profile,fallback,serialized:false},message:"Conectando…",failed:false}));
 useEffect(()=>{
  setModel(previous=>{
   if(previous.active?.key===key||(previous.active?.ready&&previous.active.actualProfile===profile))return {...previous,pending:null,message:"",failed:false};
   if(previous.pending?.key===key)return previous;
   return {active:previous.active,pending:{key,profile,fallback:JSON.parse(target)[1],serialized:!!previous.active},message:"Conectando…",failed:false};
  });
 },[key,profile,target]);
 const available=!!model.active?.ready;
 const waitingToReconnect=model.failed&&!available&&!model.pending;
 useEffect(()=>{
  if(!waitingToReconnect)return;
  const timer=setTimeout(()=>setAttempt(value=>value+1),reconnectDelayMs);
  return()=>clearTimeout(timer);
 },[waitingToReconnect,key]);
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
   if(previous.pending?.key===entry.key)return {...previous,pending:null,message:previous.active?.ready?"Qualidade solicitada indisponível. Mantendo a transmissão atual.":message,failed:true};
   if(previous.active?.key===entry.key)return {...previous,active:null,message,failed:true};
   return previous;
  });
 };
 const showing=available;
 return <div className="live-video-stack" data-display-mode={camera.displayMode??"contain"} data-changing={!!model.pending&&showing}>
  {[model.active,model.pending].filter((entry):entry is Entry=>!!entry).map(entry=><LiveStream key={entry.key} tenant={tenant} camera={camera} request={entry} presented={model.active?.key===entry.key&&!!entry.ready} visible={visible&&model.active?.key===entry.key} debugEnabled={debugEnabled} queue={entry.serialized?queue:undefined} onReady={actual=>ready(entry,actual)} onWaiting={message=>waiting(entry,message)} onFailure={(message,denied)=>failure(entry,message,denied)}/>)}
  {debugEnabled&&visible&&!showing&&<LiveDebugMetrics/>}
  {model.message&&(!showing?<div className="video-state" role="status">{model.failed?<Button className="video-failure" onClick={()=>setAttempt(value=>value+1)} variant="ghost" size="icon" aria-label={`Falha na conexão de ${camera.name}. Reconectar`} title={`${model.message} Clique para reconectar.`}><VideoOff aria-hidden="true"/></Button>:<LoaderCircle className="animate-spin" aria-label={model.message}/>}</div>:model.failed?<div className="live-video-notice" role="status"><span>{model.message}</span><button type="button" onClick={()=>setAttempt(value=>value+1)}>Tentar novamente</button></div>:null)}
 </div>;
}

export function LivePlayer({tenant,camera,profile,fallback,focusProfile,focusFallback,visible,debugEnabled,queue}:{
 tenant:string;camera:Camera;profile:string;fallback:string[];focusProfile?:string;focusFallback?:string[];visible:boolean;queue:LiveTransitionQueue;
 debugEnabled:boolean;
}){
 const [denied,setDenied]=useState(false);
 const needsFocus=!!focusProfile&&focusProfile!==profile;
 if(!camera.effectiveActive||denied)return <>
  <div className="video-state" role="status"><VideoOff aria-label={denied?"Acesso à transmissão encerrado":"Câmera desativada"}/></div>
  {debugEnabled&&visible&&<LiveDebugMetrics/>}
 </>;
 // Maximization is a profile transition owned by the same player. Promotion
 // unmounts the previous stream instead of keeping a hidden base connection.
 return <ContinuousVideo tenant={tenant} camera={camera} profile={needsFocus?focusProfile!:profile} fallback={needsFocus?(focusFallback??[]):fallback} queue={queue} visible={visible} debugEnabled={debugEnabled} onDenied={()=>setDenied(true)}/>;
}
