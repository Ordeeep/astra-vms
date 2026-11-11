"use client";

import {Minimize,Monitor} from "lucide-react";
import {Button} from "@/components/ui/button";
import {LiveHeaderControls} from "@/components/live-header-controls";
import {useLiveView} from "@/components/live-view-context";

export function LiveFullscreenControls(){
 const {fullscreen,maximized,setMaximized,toggleFullscreen}=useLiveView();
 if(!fullscreen)return null;
 return <div className="live-wall-controls" role="toolbar" aria-label="Controles do videowall">
  <span className="live-wall-label"><Monitor aria-hidden="true"/>Videowall</span>
  {maximized!==null&&<Button variant="ghost" size="sm" onClick={()=>setMaximized(null)}>Restaurar mosaico</Button>}
  <LiveHeaderControls wall/>
  <Button variant="ghost" size="sm" onClick={()=>void toggleFullscreen()}><Minimize aria-hidden="true"/>Sair <kbd>Esc</kbd></Button>
 </div>;
}
