"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ThemeToggle } from "@/components/theme-toggle";
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Bug, ChevronDown, Monitor } from "lucide-react";
import { useLiveView } from "@/components/live-view-context";
import { allMosaicLayouts, mosaicLayouts, type MosaicLayout } from "@/lib/live-mosaic";

function LayoutPreview({ layout }: { layout: MosaicLayout }) {
  return (
    <img className="live-layout-preview" src={`/mosaic-layouts/${layout.id}.png`}
      width={24} height={20} alt="" draggable={false}/>
  );
}

export function LiveHeaderActions() {
  const { fullscreen, toggleFullscreen, debugEnabled, toggleDebug } = useLiveView();

  return (
    <ButtonGroup aria-label="Tela, debug e tema">
      <Button type="button" variant="outline" size="sm" className="live-wall-button"
        title="Abrir em tela cheia" hidden={fullscreen} onClick={() => void toggleFullscreen()}>
        <Monitor aria-hidden="true"/>
        Vídeo Wall
      </Button>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button type="button" variant={debugEnabled ? "default" : "outline"} size="icon-lg"
            aria-label="Debug do mosaico" aria-pressed={debugEnabled} onClick={toggleDebug}>
            <Bug aria-hidden="true"/>
          </Button>
        </TooltipTrigger>
        <TooltipContent>{debugEnabled ? "Desativar debug do mosaico" : "Ativar debug do mosaico"}</TooltipContent>
      </Tooltip>
      <ThemeToggle variant="outline"/>
    </ButtonGroup>
  );
}

function LayoutPicker({ enabled }: { enabled: boolean }) {
  const { layout, changeLayout } = useLiveView();
  const [open, setOpen] = useState(false);

  return (
    <DropdownMenu modal={false} open={enabled && open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="ghost" size="sm" className="live-layout-more"
          aria-label="Layouts" aria-description={`Layout atual: ${layout.label}`} title="Layouts">
          <LayoutPreview layout={layout}/>
          <ChevronDown className="live-layout-chevron size-3" aria-hidden="true"/>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" collisionPadding={8} className="live-layout-menu min-w-30">
        <ScrollArea type="auto">
          <DropdownMenuRadioGroup value={layout.id} onValueChange={changeLayout}>
            {allMosaicLayouts.map(preset => (
              <DropdownMenuRadioItem key={preset.id} value={preset.id} className="live-layout-option"
                aria-label={`Layout ${preset.label}`} title={preset.shortLabel ? preset.label : undefined}>
                <LayoutPreview layout={preset}/>
                <span>{preset.shortLabel ?? preset.label}</span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </ScrollArea>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function LiveHeaderControls({ wall = false }: { wall?: boolean }) {
  const { layout, changeLayout, fullscreen } = useLiveView();
  const enabled = !fullscreen || wall;
  return (
    <div className="live-header-controls" hidden={!enabled}>
      <TooltipProvider delayDuration={350}>
        <div className="live-layout-buttons" role="group" aria-label="Layouts do mosaico">
          {mosaicLayouts.map(preset => (
            <Tooltip key={preset.id}>
              <TooltipTrigger asChild>
                <Button type="button" variant="ghost" size="icon-sm" className="live-layout-button"
                  aria-label={`Layout ${preset.label}`} aria-pressed={layout.id === preset.id}
                  onClick={() => changeLayout(preset.id)}>
                  <LayoutPreview layout={preset}/>
                </Button>
              </TooltipTrigger>
              <TooltipContent>{preset.label}</TooltipContent>
            </Tooltip>
          ))}
        </div>
      </TooltipProvider>
      <LayoutPicker enabled={enabled}/>
    </div>
  );
}
