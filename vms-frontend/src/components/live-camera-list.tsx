"use client";

import { useRef, useState } from "react";
import { ChevronDown, Search, Video } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useLiveView } from "@/components/live-view-context";
import { LiveSavedViews } from "@/components/live-saved-views";
import { LivePTZControls } from "@/components/live-ptz-controls";

const normalize = (text: string) => text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("pt-BR").trim();

export function LiveCameraList() {
  const { cameras, sites, loading, catalogError, retryCatalog, positions, layoutSize, chooseCamera, setDraggedCameraId, setDropPosition, pendingCameraId, setPendingCameraId } = useLiveView();
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
  const mosaicCameraIds = new Set(positions.slice(0, layoutSize * layoutSize).map(position => position.cameraId));
  const groups = [...new Set(cameras.map(camera => camera.siteId))].map(id => ({
    id, name: sites.find(site => site.id === id)?.name ?? "Site restrito",
    cameras: cameras.filter(camera => camera.siteId === id),
  })).sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));
  const filtered = groups.map(group => ({ ...group,
    cameras: group.cameras.filter(camera => normalize(`${camera.name} ${group.name}`).includes(search))
      .sort((a, b) => a.name.localeCompare(b.name, "pt-BR")),
  })).filter(group => group.cameras.length);

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
    <div className="live-camera-groups">{catalogError ? <div className="live-camera-notice" role="alert"><p>{catalogError}</p><Button variant="ghost" size="sm" onClick={retryCatalog}>Tentar novamente</Button></div>
      : loading && !cameras.length ? <p className="live-camera-notice" role="status">Carregando câmeras…</p>
      : !filtered.length ? <p className="live-camera-notice" role="status">{search ? "Nenhuma câmera encontrada." : "Nenhuma câmera disponível."}</p>
      : filtered.map(group => <Collapsible key={group.id} open={!!search || !collapsedSites.has(group.id)} onOpenChange={open => setCollapsedSites(current => {
        const next = new Set(current); if (open) next.delete(group.id); else next.add(group.id); return next;
      })} className="live-camera-site">
        <CollapsibleTrigger className="live-camera-site-trigger" aria-label={`Site ${group.name}`} title={group.name}>
          <ChevronDown aria-hidden="true"/><span>{group.name}</span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <ul className="live-camera-items">
            {group.cameras.map(camera => <li key={camera.id}><button type="button" className="live-camera-item"
              aria-label={`Arrastar ${camera.name}`} title={camera.effectiveActive ? `Arraste ${camera.name} até uma posição. Pelo teclado, pressione Enter e depois Enter na posição desejada.` : `${camera.name} · Desativada`}
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
              onClick={() => {if(!suppressClick.current)setPendingCameraId(camera.id);suppressClick.current=false;}}
              onKeyDown={event => { if (event.key === "Escape") {setPendingCameraId(null);clearDrag();} }}>
              <Video aria-hidden="true"/><span>{camera.name}</span>
            </button></li>)}
          </ul>
        </CollapsibleContent>
      </Collapsible>)}</div>
    <Collapsible className="live-ptz-panel" defaultOpen={false}>
      <CollapsibleTrigger className="live-camera-site-trigger live-ptz-trigger">
        <ChevronDown aria-hidden="true"/><span>Controle PTZ</span>
      </CollapsibleTrigger>
      <CollapsibleContent className="live-ptz-content" role="region" aria-label="Controle PTZ"><LivePTZControls/></CollapsibleContent>
    </Collapsible>
    </TabsContent>
  </Tabs>;
}
