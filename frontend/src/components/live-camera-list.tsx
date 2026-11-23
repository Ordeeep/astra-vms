"use client";

import { useRef, useState } from "react";
import { ChevronDown, Joystick, Search, Video } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useLiveView } from "@/components/live-view-context";
import { LiveSavedViews } from "@/components/live-saved-views";
import { LivePTZControls } from "@/components/live-ptz-controls";
import { LiveRecordingExportPanel } from "@/components/exports";

const normalize = (text: string) => text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("pt-BR").trim();

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
  const [collapsedSites, setCollapsedSites] = useState<Set<string>>(() => new Set());
  const search = normalize(query);
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
  const isSiteOpen = (id: string) => !!search || !collapsedSites.has(id);

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
    <div className="live-camera-search">
      <Search aria-hidden="true"/>
      <Input type="search" aria-label="Buscar câmera ou site" placeholder="Buscar câmera" value={query} onChange={event => setQuery(event.target.value)}/>
    </div>
    <ScrollArea className="live-camera-groups" type="auto">{catalogError ? <div className="live-camera-notice" role="alert"><p>{catalogError}</p><Button variant="ghost" size="sm" onClick={retryCatalog}>Tentar novamente</Button></div>
      : loading && !cameras.length ? <p className="live-camera-notice" role="status">Carregando câmeras…</p>
      : !filtered.length ? <p className="live-camera-notice" role="status">{search ? "Nenhuma câmera encontrada." : "Nenhuma câmera disponível."}</p>
      : filtered.map(group => <Collapsible key={group.id} open={isSiteOpen(group.id)} onOpenChange={open => setCollapsedSites(current => {
        const next = new Set(current); if (open) next.delete(group.id); else next.add(group.id); return next;
      })} className="live-camera-site">
        <CollapsibleTrigger className="live-camera-site-trigger" aria-label={`Site ${group.name}`} title={group.name}>
          <ChevronDown className="live-camera-trigger-chevron" aria-hidden="true"/><span>{group.name}</span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <ul className="live-camera-items">
            {group.cameras.map(camera => <li key={camera.id}><Button type="button" variant="ghost" size="sm" className="live-camera-item"
              aria-label={`Arrastar ${camera.name}`} title={camera.effectiveActive ? `${camera.name} · Clique para preencher o mosaico. Para escolher uma posição, arraste ou use Enter na câmera e na posição.` : `${camera.name} · Desativada`}
              aria-pressed={pendingCameraId === camera.id} disabled={!camera.effectiveActive}
              data-in-mosaic={mosaicCameraIds.has(camera.id)} aria-description={mosaicCameraIds.has(camera.id) ? "No mosaico" : undefined}
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
              <Video aria-hidden="true"/><span>{camera.name}</span>
            </Button></li>)}
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
