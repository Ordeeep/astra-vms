"use client";

import {useEffect,useRef,useState} from "react";

// Fullscreen the document so portaled menus and dialogs remain usable. Only
// presentation changes: tiles and their transports retain their identity.
export function useLiveFullscreen(enabled:boolean){
 const [fullscreen,setFullscreen]=useState(false),[fullscreenError,setError]=useState("");
 const allowed=useRef(enabled);
 const owned=useRef(false),busy=useRef(false),returnFocus=useRef<HTMLElement|null>(null);
 useEffect(()=>{
  const changed=()=>{
   const active=owned.current&&document.fullscreenElement===document.documentElement;
   setFullscreen(active);
   if(!active&&owned.current){
    owned.current=false;
    requestAnimationFrame(()=>{if(returnFocus.current?.isConnected)returnFocus.current.focus();});
   }
  };
  allowed.current=enabled;
  const escape=(event:KeyboardEvent)=>{
   if(event.key==="Escape"&&owned.current&&document.fullscreenElement===document.documentElement)
    void document.exitFullscreen().catch(()=>{});
  };
  document.addEventListener("fullscreenchange",changed);
  window.addEventListener("keydown",escape,true);
  return()=>{
   allowed.current=false;
   document.removeEventListener("fullscreenchange",changed);
   window.removeEventListener("keydown",escape,true);
   if(owned.current&&document.fullscreenElement===document.documentElement)void document.exitFullscreen().catch(()=>{});
   owned.current=false;
  };
 },[]);
 useEffect(()=>{
  allowed.current=enabled;
  if(!enabled&&owned.current&&document.fullscreenElement)void document.exitFullscreen().catch(()=>{});
 },[enabled]);
 async function toggleFullscreen(){
  if(busy.current)return;
  setError("");
  if(!document.fullscreenEnabled){setError("Tela cheia não está disponível neste navegador.");return;}
  busy.current=true;
  try{
   if(owned.current&&document.fullscreenElement)await document.exitFullscreen();
   else if(enabled){
    returnFocus.current=document.activeElement instanceof HTMLElement?document.activeElement:null;
    owned.current=true;
    await document.documentElement.requestFullscreen({navigationUI:"hide"});
    if(!allowed.current&&document.fullscreenElement===document.documentElement)await document.exitFullscreen();
   }
  }catch{
   owned.current=false;setFullscreen(false);
   setError("Não foi possível ativar a tela cheia. Tente novamente pelo botão Videowall.");
  }finally{busy.current=false;}
 }
 return {fullscreen,fullscreenError,toggleFullscreen};
}

