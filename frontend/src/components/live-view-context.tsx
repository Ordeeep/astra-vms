"use client";

import { createContext, useContext, useEffect, useRef, useState, type Dispatch, type ReactNode, type SetStateAction } from "react";
import { allPages, type Camera, type Site } from "@/components/management";
import { ApiError } from "@/lib/api";
import { onEnvironmentChange } from "@/lib/events";
import { allMosaicLayouts, mosaicLayouts, restoreMosaic, savedMosaicLayout, type LiveMosaic, type LiveTile, type MosaicLayout } from "@/lib/live-mosaic";

import {useLiveFullscreen} from "@/lib/use-live-fullscreen";

export type { LivePosition, LiveMosaic } from "@/lib/live-mosaic";
// Local tile identity follows its contents when positions are exchanged, keeping
// each player mounted even when the same camera appears more than once.

type LiveView = {
  fullscreen: boolean;
  fullscreenError: string;
  toggleFullscreen: () => Promise<void>;
  debugEnabled: boolean;
  toggleDebug: () => void;
  tenantId?: string;
  layout: MosaicLayout;
  changeLayout: (id: string) => void;
  cameraListOpen: boolean;
  setCameraListOpen: Dispatch<SetStateAction<boolean>>;
  dropPosition: number | null;
  setDropPosition: Dispatch<SetStateAction<number | null>>;
  draggedCameraId: string | null;
  setDraggedCameraId: Dispatch<SetStateAction<string | null>>;
  pendingCameraId: string | null;
  setPendingCameraId: Dispatch<SetStateAction<string | null>>;
  cameras: Camera[];
  exportCameraIds: ReadonlySet<string>;
  sites: Site[];
  loading: boolean;
  catalogError: string;
  retryCatalog: () => void;
  positions: LiveTile[];
  setPositions: Dispatch<SetStateAction<LiveTile[]>>;
  selected: LiveMosaic | null;
  setSelected: Dispatch<SetStateAction<LiveMosaic | null>>;
  loadMosaic: (mosaic: LiveMosaic) => void;
  highlightedPosition: number | null;
  activePosition: number;
  setActivePosition: (index: number) => void;
  maximized: number | null;
  setMaximized: Dispatch<SetStateAction<number | null>>;
  chooseCamera: (cameraId: string, positionIndex: number) => void;
  fillMosaic: (cameraIds: readonly string[]) => void;
  moveCamera: (from: number, to: number) => void;
};

const LiveViewContext = createContext<LiveView | null>(null);

// Workspace keys this provider by viewing context, so a tenant switch discards
// both the camera catalog and the local composition before a new request starts.
export function LiveViewProvider({ tenantId, enabled, children }: {
  tenantId?: string; enabled: boolean; children: ReactNode;
}) {
  const fullscreenState=useLiveFullscreen(enabled);
  const [debugEnabled, setDebugEnabled] = useState(false);
  useEffect(() => {
    if (!enabled) setDebugEnabled(false);
  }, [enabled]);
  const [layout, setLayout] = useState(mosaicLayouts[1]);
  const [cameraListOpen, setCameraListOpen] = useState(true);
  const [dropPosition, setDropPosition] = useState<number | null>(null);
  const [draggedCameraId, setDraggedCameraId] = useState<string | null>(null);
  const [pendingCameraId, setPendingCameraId] = useState<string | null>(null);
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [exportCameraIds, setExportCameraIds] = useState<ReadonlySet<string>>(() => new Set());
  const [sites, setSites] = useState<Site[]>([]);
  const [loading, setLoading] = useState(true);
  const [catalogError, setCatalogError] = useState("");
  const [retry, setRetry] = useState(0);
  const [positions, setPositions] = useState<LiveTile[]>(() => Array.from({ length: 4 }, (_, tileId) => ({ tileId, cameraId: "", profile: "" })));
  const nextTileId = useRef(4);
  const [selected, setSelected] = useState<LiveMosaic | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [maximized, setMaximized] = useState<number | null>(null);
  const [highlightedPosition, setHighlightedPosition] = useState<number|null>(null);
  const highlightTimer = useRef<ReturnType<typeof setTimeout>|null>(null);
  useEffect(()=>()=>{if(highlightTimer.current)clearTimeout(highlightTimer.current);},[]);
  function setActivePosition(index:number) {
    setActiveIndex(index);setHighlightedPosition(index);
    if(highlightTimer.current)clearTimeout(highlightTimer.current);
    highlightTimer.current=setTimeout(()=>setHighlightedPosition(null),2000);
  }
  const activePosition = maximized ?? Math.min(activeIndex, layout.areas.length - 1);

  useEffect(() => {
    if (!enabled || !tenantId) return;
    const base = `/tenants/${tenantId}`;
    let request: AbortController | undefined;
    let ready = false;

    async function load(background = false) {
      if (background && request) return;
      request?.abort();
      const controller = new AbortController();
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(10_000)]);
      request = controller;
      if (!background) setLoading(true);
      try {
        const [nextCameras, nextSites] = await Promise.all([
          allPages<Camera>(base + "/cameras", signal),
          background ? undefined : allPages<Site>(base + "/sites", signal),
        ]);
        if (signal.aborted) return;
        const liveCameras = nextCameras.filter(camera => camera.capabilities.live);
        const visible = new Set(liveCameras.map(camera => camera.id));
        setCameras(liveCameras);
        setPositions(current => current.some(position => position.cameraId && !visible.has(position.cameraId))
          ? current.map(position => position.cameraId && !visible.has(position.cameraId) ? {tileId: position.tileId, cameraId: "", profile: ""} : position)
          : current);
        if (nextSites) setSites(nextSites);
        setCatalogError("");
        ready = true;
      } catch (error) {
        if (controller.signal.aborted) return;
        const accessLost = error instanceof ApiError && [401, 403, 404].includes(error.status);
        if (background && !accessLost) {
          setCameras(current => current.map(camera => ({...camera, availability: undefined, recordingActive: undefined})));
          return;
        }
        ready = false;
        setCameras([]); setSites([]);
        setCatalogError((error as Error).message);
      } finally {
        if (request === controller) {
          request = undefined;
          if (!background) setLoading(false);
        }
      }
    }

    const refreshStatus = () => { if (ready && !document.hidden) void load(true); };
    void load();
    const unsubscribe = onEnvironmentChange(["CameraStatusChanged", "InventoryChanged", "CaptureStateChanged"], notification => {
      if (notification.type === "CaptureStateChanged") refreshStatus();
      else void load();
    });
    document.addEventListener("visibilitychange", refreshStatus);
    const timer = window.setInterval(refreshStatus, 5000);
    return () => {
      request?.abort();
      unsubscribe();
      document.removeEventListener("visibilitychange", refreshStatus);
      window.clearInterval(timer);
    };
  }, [tenantId, enabled, retry]);

  useEffect(() => {
    if (!enabled || !tenantId) return;
    let active = true, generation = 0;
    async function load() {
      const version = ++generation;
      try {
        const catalog = await allPages<Pick<Camera,"id"|"capabilities">>(`/tenants/${tenantId}/recording-cameras`);
        if (active && version === generation) setExportCameraIds(new Set(catalog.filter(camera => camera.capabilities.export).map(camera => camera.id)));
      } catch {
        if (active && version === generation) setExportCameraIds(new Set());
      }
    }
    void load();
    const unsubscribe = onEnvironmentChange(["InventoryChanged"], () => void load());
    return () => { active = false; unsubscribe(); };
  }, [tenantId, enabled, retry]);

  function changeLayout(id: string) {
    const next = allMosaicLayouts.find(candidate => candidate.id === id);
    if (!next) return;
    const count = next.areas.length;
    setPositions(current => current.length >= count ? current : Array.from({length:count},(_,index)=>current[index]??{tileId:nextTileId.current++,cameraId:"",profile:""}));
    setLayout(next);
    setActiveIndex(current=>Math.min(current,count-1));
    setMaximized(null);setDropPosition(null);setPendingCameraId(null);
    setHighlightedPosition(current => current !== null && current >= count ? null : current);
  }

  function loadMosaic(mosaic: LiveMosaic) {
    setPositions(current => restoreMosaic(mosaic, current, () => nextTileId.current++));
    setLayout(savedMosaicLayout(mosaic));
    setSelected(mosaic);
    setActiveIndex(0);
    setMaximized(null);setDropPosition(null);setPendingCameraId(null);setDraggedCameraId(null);
    setHighlightedPosition(null);
    if (highlightTimer.current) clearTimeout(highlightTimer.current);
  }

  function chooseCamera(cameraId: string, positionIndex: number) {
    if (!cameras.some(camera => camera.id === cameraId && camera.effectiveActive)) return;
    setActivePosition(positionIndex);
    setPositions(current => current.map((position, index) => index === positionIndex
      ? { ...position, cameraId, profile: "", fallback: [] } : position));
  }

  function fillMosaic(cameraIds: readonly string[]) {
    if (!cameraIds.length) return;
    const visibleCameraIds = cameraIds.slice(0, layout.areas.length);
    setPositions(current => {
      const next = visibleCameraIds.map(cameraId =>
        current.find(tile => tile.cameraId === cameraId) ?? { cameraId, profile: "" });
      return restoreMosaic({ layout: layout.id, positions: next }, current, () => nextTileId.current++);
    });
    setMaximized(null);
    setPendingCameraId(null);
    setActivePosition(0);
  }

  function moveCamera(from: number, to: number) {
    const count = layout.areas.length;
    if (maximized !== null || !Number.isInteger(from) || !Number.isInteger(to)
      || from === to || from < 0 || to < 0 || from >= count || to >= count
      || !positions[from]?.cameraId) return;
    setPositions(current => {
      if (!current[from]?.cameraId || !current[to]) return current;
      const next = [...current];
      [next[from], next[to]] = [next[to], next[from]];
      return next;
    });
    setActivePosition(to);
    setPendingCameraId(null);
  }

  return <LiveViewContext.Provider value={{
    ...fullscreenState, tenantId, layout, changeLayout, cameraListOpen, setCameraListOpen,
    debugEnabled: enabled && debugEnabled, toggleDebug: () => setDebugEnabled(value => !value),
    cameras, exportCameraIds, sites, loading, catalogError, retryCatalog: () => setRetry(value => value + 1),
    highlightedPosition, positions, setPositions, selected, setSelected, loadMosaic, activePosition, setActivePosition,
    dropPosition, setDropPosition, maximized, setMaximized, chooseCamera, fillMosaic, moveCamera, draggedCameraId, setDraggedCameraId, pendingCameraId, setPendingCameraId,
  }}>{children}</LiveViewContext.Provider>;
}

export function useLiveView() {
  const value = useContext(LiveViewContext);
  if (!value) throw new Error("useLiveView requires LiveViewProvider");
  return value;
}
