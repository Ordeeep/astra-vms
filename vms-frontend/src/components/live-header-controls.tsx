"use client";

import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Button } from "@/components/ui/button";
import { ChevronLeft, ChevronRight, Monitor } from "lucide-react";
import { useLiveView } from "@/components/live-view-context";

export function LiveHeaderControls({wall=false}:{wall?:boolean}) {
  const {layoutSize,changeLayout,cameraListOpen,setCameraListOpen,toggleFullscreen,fullscreen}=useLiveView();
  return <div className="live-header-controls" hidden={fullscreen&&!wall}>
    {!wall&&<Button variant="ghost" size="sm" aria-label="Abrir videowall em tela cheia" title="Videowall em tela cheia" onClick={()=>void toggleFullscreen()}><Monitor aria-hidden="true"/><span className="sr-only">Videowall</span></Button>}
    <NativeSelect className="live-layout-select" aria-label="Layout do mosaico" value={layoutSize} onChange={event=>changeLayout(Number(event.target.value))}>
      {[1,2,3,4].map(size=><NativeSelectOption key={size} value={size}>{size}×{size}</NativeSelectOption>)}
    </NativeSelect>
    <Button variant="ghost" size="icon" aria-label={cameraListOpen?"Recuar listagem":"Exibir listagem"} title={cameraListOpen?"Recuar listagem":"Exibir listagem"} aria-expanded={cameraListOpen} aria-controls="live-camera-list-content" onClick={()=>setCameraListOpen(open=>!open)}>{cameraListOpen?<ChevronRight aria-hidden="true"/>:<ChevronLeft aria-hidden="true"/>}</Button>
  </div>;
}
