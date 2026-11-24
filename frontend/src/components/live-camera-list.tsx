"use client";

import { useRef, useState } from "react";
import { Cctv, CctvOff, ChevronDown, ChevronsDownUp, ChevronsUpDown, CircleHelp, Joystick, Search } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useLiveView } from "@/components/live-view-context";
import { LiveSavedViews } from "@/components/live-saved-views";
import { LivePTZControls } from "@/components/live-ptz-controls";
import { LiveRecordingExportPanel } from "@/components/exports";
import type { Camera } from "@/components/management";
import { availabilityLabels, cameraAvailability } from "@/lib/camera-availability";

const normalize = (text: string) => text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("pt-BR").trim();

function cameraIconStatus(camera: Camera, now: number) {
  const {state} = cameraAvailability(camera, now);
  if (state !== "online") return {state, label: availabilityLabels[state]};
  if (camera.recordingActive === undefined) return {state: "unknown", label: "Online · Estado da gravação indisponível"};
  if (camera.recordingActive) return {state: "recording", label: "Online · Gravando"};
  return {state, label: "Online · Sem gravar"};
}

export function LiveCameraList() {
  const { cameras, sites, loading, catalogError, retryCatalog, positions, layout, activePosition, exportCameraIds, chooseCamera, fillMosaic, setDraggedCameraId, setDropPosition, pendingCameraId, setPendingCameraId } = useLiveView();
  const gesture = useRef<{x:number;y:number;cameraId:string;moved:boolean}|null>(null);
  const suppressClick = useRef(false);
  const clearDrag = () => { gesture.current=null;setDraggedCameraId(null);setDropPosition(null); };
  const targetPosition = (x:number,y:number) => {
    const cell=document.elementFromPoint(x,y)?.closest<HTMLElement>("[data-live-position]");
    return cell ? Number(cell.dataset.livePosition) : null;
  };
  const [query, setQuery] = useState("");
  const [openSites, setOpenSites] = useState<Set<string>>(() => new Set());
  const [closedSearchSites, setClosedSearchSites] = useState<Set<string>>(() => new Set());
  const search = normalize(query);
  const now = Date.now();
  const mosaicCameraIds = new Set(positions.slice(0, layout.areas.length).map(position => position.cameraId).filter(Boolean));
  const exportCameraId = positions[activePosition]?.cameraId || (mosaicCameraIds.size === 1 ? mosaicCameraIds.values().next().value : undefined);
  const exportCamera = cameras.find(camera => camera.id === exportCameraId);
  const groups = [...new Set(cameras.map(camera => camera.siteId))].map(id => ({
    id, name: sites.find(site => site.id === id)?.name ?? "Site restrito",
    cameras: cameras.filter(camera => camera.siteId === id),
  })).sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));
  const filtered = groups.map(group => ({ ...group,
    cameras: group.cameras.filter(camera => normalize(`${camera.name} ${group.name}`).includes(search))
      .sort((a, b) => a.name.localeCompare(b.name, "pt-BR")),
  })).filter(group => group.cameras.length);
  const isSiteOpen = (id: string) => search ? !closedSearchSites.has(id) : openSites.has(id);
  const allOpen = filtered.every(group => isSiteOpen(group.id));
  const toggleLabel = allOpen ? "Recolher todos os sites" : "Expandir todos os sites";
  const ToggleIcon = allOpen ? ChevronsDownUp : ChevronsUpDown;

  function setSiteOpen(id: string, open: boolean) {
    const setSites = search ? setClosedSearchSites : setOpenSites;
    setSites(current => {
      const next = new Set(current);
      if (search ? !open : open) next.add(id);
      else next.delete(id);
      return next;
    });
  }

  function toggleAllSites() {
    const siteIds = filtered.map(group => group.id);
    if (search) setClosedSearchSites(new Set(allOpen ? siteIds : []));
    else setOpenSites(new Set(allOpen ? [] : siteIds));
  }

  function fillFromCamera(cameraId: string) {
    const listed = filtered.filter(group => isSiteOpen(group.id))
      .flatMap(group => group.cameras.filter(camera => camera.effectiveActive).map(camera => camera.id));
    const start = listed.indexOf(cameraId);
    if (start >= 0) fillMosaic(listed.slice(start));
  }

  return <Tabs defaultValue="cameras" className="live-camera-browser" aria-label="Visualizações e câmeras ao vivo">
    <TabsList className="live-camera-tabs" aria-label="Conteúdo do painel ao vivo">
      <TabsTrigger value="views">Visualização</TabsTrigger>
      <TabsTrigger value="cameras">Câmeras</TabsTrigger>
    </TabsList>
    <TabsContent value="views" className="live-camera-tab-content">
      <LiveSavedViews/>
    </TabsContent>
    <TabsContent value="cameras" className="live-camera-tab-content live-camera-catalog">
    <div className="live-camera-tools">
      <div className="live-camera-search">
        <Search aria-hidden="true"/>
        <Input type="search" aria-label="Buscar câmera ou site" placeholder="Buscar câmera" value={query} onChange={event => {
          setQuery(event.target.value);
          if (normalize(event.target.value) !== search) setClosedSearchSites(new Set());
        }}/>
      </div>
      {!catalogError && filtered.length > 0 && <Tooltip>
        <TooltipTrigger asChild>
          <Button type="button" variant="outline" size="icon" className="live-camera-toggle-sites" aria-label={toggleLabel} onClick={toggleAllSites}>
            <ToggleIcon aria-hidden="true"/>
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom" sideOffset={4}>{toggleLabel}</TooltipContent>
      </Tooltip>}
    </div>
    <ScrollArea className="live-camera-groups" type="auto">{catalogError ? <div className="live-camera-notice" role="alert"><p>{catalogError}</p><Button variant="ghost" size="sm" onClick={retryCatalog}>Tentar novamente</Button></div>
      : loading && !cameras.length ? <p className="live-camera-notice" role="status">Carregando câmeras…</p>
      : !filtered.length ? <p className="live-camera-notice" role="status">{search ? "Nenhuma câmera encontrada." : "Nenhuma câmera disponível."}</p>
      : filtered.map(group => <Collapsible key={group.id} open={isSiteOpen(group.id)} onOpenChange={open => setSiteOpen(group.id, open)} className="live-camera-site">
        <CollapsibleTrigger className="live-camera-site-trigger" aria-label={`Site ${group.name}`} title={group.name}>
          <ChevronDown className="live-camera-trigger-chevron" aria-hidden="true"/><span>{group.name}</span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <ul className="live-camera-items">
            {group.cameras.map(camera => {
              const status = cameraIconStatus(camera, now);
              const StatusIcon = status.state === "offline" ? CctvOff : Cctv;
              const inMosaic = mosaicCameraIds.has(camera.id);
              const action = "Clique para preencher o mosaico. Para escolher uma posição, arraste ou use Enter na câmera e na posição.";
              const description = [status.label, inMosaic && "No mosaico", camera.effectiveActive && action].filter(Boolean).join(". ");
              return <li key={camera.id}><Tooltip delayDuration={300}><TooltipTrigger asChild><Button type="button" variant="ghost" size="sm" className="live-camera-item"
              aria-label={`Arrastar ${camera.name}`}
              aria-pressed={pendingCameraId === camera.id} disabled={!camera.effectiveActive}
              data-in-mosaic={inMosaic} data-camera-status={status.state} aria-description={description}
              data-draggable={camera.effectiveActive}
              onPointerDown={event => {
                if(event.button!==0)return;
                suppressClick.current=false;
                gesture.current={x:event.clientX,y:event.clientY,cameraId:camera.id,moved:false};
                event.currentTarget.setPointerCapture(event.pointerId);
              }}
              onPointerMove={event => {
                const current=gesture.current;if(!current)return;
                if(!current.moved&&Math.hypot(event.clientX-current.x,event.clientY-current.y)<6)return;
                current.moved=true;suppressClick.current=true;
                setPendingCameraId(null);setDraggedCameraId(current.cameraId);
                setDropPosition(targetPosition(event.clientX,event.clientY));
              }}
              onPointerUp={event => {
                const current=gesture.current;
                if(current?.moved){const target=targetPosition(event.clientX,event.clientY);if(target!==null)chooseCamera(current.cameraId,target);}
                clearDrag();
              }}
              onPointerCancel={clearDrag}
              onLostPointerCapture={clearDrag}
              onClick={event => {
                const dragged = event.detail !== 0 && suppressClick.current;
                suppressClick.current = false;
                if (dragged) return;
                if (event.detail === 0 || ("pointerType" in event.nativeEvent && event.nativeEvent.pointerType === "touch")) {
                  setPendingCameraId(camera.id);
                } else {
                  fillFromCamera(camera.id);
                }
              }}
              onKeyDown={event => { if (event.key === "Escape") {setPendingCameraId(null);clearDrag();} }}>
              <span className="live-camera-status-icon" aria-hidden="true">
                <StatusIcon/>
                {status.state === "unknown" && <CircleHelp className="live-camera-status-mark"/>}
              </span>
              <span>{camera.name}</span>
            </Button></TooltipTrigger><TooltipContent side="left" sideOffset={6}>{status.label}</TooltipContent></Tooltip></li>;
            })}
          </ul>
        </CollapsibleContent>
      </Collapsible>)}</ScrollArea>
    <Collapsible className="live-ptz-panel" defaultOpen={false}>
      <CollapsibleTrigger className="live-camera-site-trigger live-ptz-trigger">
        <Joystick aria-hidden="true"/>
        <span>Controle PTZ</span>
        <ChevronDown className="live-camera-trigger-chevron" aria-hidden="true"/>
      </CollapsibleTrigger>
      <CollapsibleContent className="live-ptz-content" role="region" aria-label="Controle PTZ"><LivePTZControls/></CollapsibleContent>
    </Collapsible>
    <LiveRecordingExportPanel camera={exportCamera} allowed={!!exportCamera && exportCameraIds.has(exportCamera.id)}/>
    </TabsContent>
  </Tabs>;
}
