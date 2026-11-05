"use client";

import type { ReactNode } from "react";
import { ContextMenu } from "radix-ui";
import { Check, ChevronRight, Circle, Maximize, Minimize, Video, X } from "lucide-react";

export function LiveCameraMenu({children,occupied,maximized,onOpen,onMaximize,onClose,onRecording,profiles,profile,onProfileChange}:{
  children:ReactNode;occupied:boolean;maximized:boolean;onOpen:()=>void;onMaximize:()=>void;onClose:()=>void;
  onRecording:()=>void;profiles:{id:string;name:string}[];profile:string;onProfileChange:(value:string)=>void;
}) {
  return <ContextMenu.Root onOpenChange={open=>{if(open)onOpen();}}>
    <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
    <ContextMenu.Portal>
      <ContextMenu.Content className="live-context-menu" aria-label="Ações da câmera" collisionPadding={8}>
        {occupied?<>
          <ContextMenu.Item onSelect={onRecording}><Circle aria-hidden="true"/>Gravação</ContextMenu.Item>
          <ContextMenu.Item onSelect={onMaximize}>{maximized?<Minimize aria-hidden="true"/>:<Maximize aria-hidden="true"/>}{maximized?"Restaurar mosaico":"Maximizar câmera"}</ContextMenu.Item>
          <ContextMenu.Sub>
            <ContextMenu.SubTrigger><Video aria-hidden="true"/>Trocar stream<ChevronRight className="live-context-chevron" aria-hidden="true"/></ContextMenu.SubTrigger>
            <ContextMenu.Portal>
              <ContextMenu.SubContent className="live-context-menu" aria-label="Streams da câmera" collisionPadding={8} sideOffset={4}>
                <ContextMenu.RadioGroup value={profile} onValueChange={onProfileChange}>
                  {[{id:"",name:"Automática"},...profiles].map(option=><ContextMenu.RadioItem value={option.id} key={option.id}>
                    {option.name}<ContextMenu.ItemIndicator className="live-context-check"><Check aria-hidden="true"/></ContextMenu.ItemIndicator>
                  </ContextMenu.RadioItem>)}
                </ContextMenu.RadioGroup>
              </ContextMenu.SubContent>
            </ContextMenu.Portal>
          </ContextMenu.Sub>
          <ContextMenu.Item onSelect={onClose}><X aria-hidden="true"/>Fechar câmera</ContextMenu.Item>
        </>:<ContextMenu.Item disabled>Arraste uma câmera para cá</ContextMenu.Item>}
      </ContextMenu.Content>
    </ContextMenu.Portal>
  </ContextMenu.Root>;
}
