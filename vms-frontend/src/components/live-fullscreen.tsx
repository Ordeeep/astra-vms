"use client";

import {useEffect,useState} from "react";
import {Minimize,Monitor} from "lucide-react";
import {Button} from "@/components/ui/button";
import {LiveHeaderControls} from "@/components/live-header-controls";
import {useLiveView} from "@/components/live-view-context";

export function LiveFullscreenControls(){
 const {fullscreen,maximized,setMaximized,toggleFullscreen}=useLiveView();
 const [awake,setAwake]=useState(true);
 useEffect(()=>{
  if(!fullscreen)return;
  let timer:ReturnType<typeof setTimeout>;
  const wake=()=>{setAwake(true);clearTimeout(timer);timer=setTimeout(()=>setAwake(false),3000);};
  wake();
  window.addEventListener("pointermove",wake);
  window.addEventListener("pointerdown",wake);
  window.addEventListener("keydown",wake);
  return()=>{clearTimeout(timer);window.removeEventListener("pointermove",wake);window.removeEventListener("pointerdown",wake);window.removeEventListener("keydown",wake);};
 },[fullscreen]);
 if(!fullscreen)return null;
 return <div className="live-wall-controls" data-awake={awake} role="toolbar" aria-label="Controles do videowall">
  <span className="live-wall-label"><Monitor aria-hidden="true"/>Videowall</span>
  {maximized!==null&&<Button variant="ghost" size="sm" onClick={()=>setMaximized(null)}>Restaurar mosaico</Button>}
  <LiveHeaderControls wall/>
  <Button variant="ghost" size="sm" onClick={()=>void toggleFullscreen()}><Minimize aria-hidden="true"/>Sair <kbd>Esc</kbd></Button>
 </div>;
}
