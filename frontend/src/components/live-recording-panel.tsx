"use client";

import "./recording-timeline.css";

import {useCallback, useEffect, useRef, useState, type ComponentProps, type CSSProperties} from "react";
import {createPortal} from "react-dom";
import {Calendar, FastForward, Rewind, Minus, Pause, Play, Plus, SkipBack, SkipForward, Undo2, Redo2, X} from "lucide-react";
import {Button} from "@/components/ui/button";
import {RecordingDatePicker} from "@/components/recording-date-picker";
import {RecordingRuler} from "@/components/recording-ruler";
import {RecordingVideo} from "@/components/recording-video";
import {useRecordingAvailability} from "@/components/use-recording-availability";
import {api} from "@/lib/api";
import type {Notification} from "@/lib/events";
import type {RecordingInterval, RecordingSegment} from "@/lib/recording-timeline";
import {DEFAULT_RECORDING_SPAN, MAX_RECORDING_SPAN, MIN_RECORDING_SPAN, initialRecordingWindow, recordingClock, recordingDate, recordingInstant, recordingSeekInstant, recordingTimestamp, zoomRecordingWindow} from "@/lib/recording-navigation";

const RECORDING_SPEEDS = [-8, -4, -2, -1, -.5, .5, 1, 2, 4, 8];

type Tile = {id: string; name: string; tileId: number; host?: HTMLElement};
type RulerProps = ComponentProps<typeof RecordingRuler>;

function RecordingTrack({tiles, tenant, at, playing, speed, ...ruler}: Omit<RulerProps,"intervals"|"name"> & {
  tenant: string; tiles: Tile[]; at: number | null; playing: boolean; speed: number;
}) {
  const camera = tiles[0], base = `/tenants/${tenant}/cameras/${camera.id}/recording-availability`;
  const day = at === null ? null : Math.floor(at / 86400000) * 86400000;
  const availability = useRecordingAvailability(base, camera.id, ruler.view, day);
  const coverage = availability.playback ?? availability;
  const covered = at !== null && coverage.intervals.some(i => i.start <= at && at < i.end);
  const status = coverage.error || (coverage.loading ? "Consultando gravações…" : "Sem gravação neste horário.");
  return <>
    <RecordingRuler {...ruler} name={camera.name} intervals={availability.intervals}/>
    {availability.disconnected && <p role="status">Conexão interrompida. Disponibilidade pode estar desatualizada.</p>}
    {tiles.map(tile => tile.host && createPortal(covered && at !== null
      ? <RecordingVideo tenant={tenant} camera={camera.id} at={at} playing={playing} speed={speed}/>
      : <div className="recording-video-message" role="status">{status}</div>, tile.host, String(tile.tileId)))}
  </>;
}

export function LiveRecordingPanel({tenantId, camera, tiles, timelineVisible, onClose}: {
  tenantId: string; camera: {id: string; name: string}; tiles: Tile[]; timelineVisible: boolean; onClose: () => void;
}) {
  const base = `/tenants/${tenantId}/cameras/${camera.id}/recording-availability`;
  const [nameWidth, setNameWidth] = useState(190);
  useEffect(() => { try { const saved = Number(localStorage.getItem("astra.recording-name-width")); if (saved >= 64 && saved <= 600) setNameWidth(saved); } catch {} }, []);
  const resizeName = (value: number) => { setNameWidth(value); try { localStorage.setItem("astra.recording-name-width", String(value)); } catch {} };
  const [zone, setZone] = useState(""), [metaError, setMetaError] = useState("");
  const [view, setView] = useState<RecordingInterval | null>(null), [revision, setRevision] = useState(0);
  const [toast, setToast] = useState<{message: string; retry?: () => void} | null>(null);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), 8000);
    return () => clearTimeout(timer);
  }, [toast]);
  const [position, setPosition] = useState<number | null>(null), [preview, setPreview] = useState<number | null>(null);
  const [playing, setPlaying] = useState(false), [speed, setSpeed] = useState(1), [follow, setFollow] = useState(true);
  const navigation = useRef(0), initialized = useRef(false);
  const at = preview ?? position;
  const center = view ? (view.start + view.end) / 2 : Date.now(), date = zone ? recordingDate(center, zone) : "";
  const seek = useCallback((value: number) => {
    navigation.current++; initialized.current = true;
    setPosition(recordingSeekInstant(value)); setPreview(null);
  }, []);
  useEffect(() => {
    let active = true;
    const version = navigation.current;
    const now = Date.now();
    const query = new URLSearchParams({direction: "previous", at: new Date(now).toISOString()});
    async function load() {
      try {
        const value = await api<{timezone: string; interval: RecordingSegment | null}>(`${base}?${query}`, "GET", undefined, true);
        if (!active) return;
        setZone(value.timezone); setMetaError("");
        if (!initialized.current && navigation.current === version) {
          const instant = value.interval ? Math.ceil(recordingTimestamp(value.interval.end)) - 1 : now;
          initialized.current = true; setPosition(instant);
          const window = initialRecordingWindow(value.timezone, instant);
          setView(window);
        }
      } catch (error) { if (active) setMetaError((error as Error).message); }
    }
    void load();
    return () => { active = false; };
  }, [base, revision]);
  useEffect(() => {
    const receive = (event: Event) => {
      const value = (event as CustomEvent<Notification>).detail;
      if (["AuthorizationChanged", "ResyncRequired"].includes(value.type)) {
        navigation.current++; initialized.current = false; setPlaying(false); setPosition(null); setPreview(null); setView(null);
      }
      if (["AuthorizationChanged","ResyncRequired","Connected","InventoryChanged"].includes(value.type)) setRevision(v => v + 1);
    };
    window.addEventListener("vms:change", receive);
    return () => { window.removeEventListener("vms:change", receive); navigation.current++; };
  }, []);
  const scrubbing = preview !== null;
  useEffect(() => {
    if (!playing || scrubbing) return;
    let last = performance.now();
    const timer = setInterval(() => {
      const now = performance.now(), elapsed = now - last;
      last = now;
      setPosition(p => p === null ? null : p + elapsed * speed);
    }, 100);
    return () => clearInterval(timer);
  }, [playing, speed, scrubbing]);
  useEffect(() => {
    if (!follow || scrubbing || position === null) return;
    setView(current => {
      if (!current) return current;
      const span = current.end - current.start;
      return position < current.start || position > current.end - span * .06 ? {start: position - span * .3, end: position + span * .7} : current;
    });
  }, [position, follow, scrubbing]);
  const navigate = (window: RecordingInterval) => { setFollow(false); setView(window); };
  const neighbour = async (direction: "previous" | "next") => {
    const current = ++navigation.current;
    try {
      const query = new URLSearchParams({direction, at: new Date(position ?? center).toISOString()});
      const value = await api<{interval: RecordingSegment | null}>(`${base}?${query}`, "GET", undefined, true);
      if (current !== navigation.current) return;
      if (!value.interval) { setToast({message: direction === "next" ? "Nenhuma gravação seguinte disponível." : "Nenhuma gravação anterior disponível."}); return; }
      const at = direction === "previous" && speed < 0 ? Math.floor(recordingTimestamp(value.interval.end)) - 1 : recordingSeekInstant(value.interval.start);
      seek(at); setFollow(true);
      setView(current => current ? {start: at - (current.end - current.start) * .3, end: at + (current.end - current.start) * .7} : current);
    } catch (error) { if (current === navigation.current) setToast({message: (error as Error).message, retry: () => void neighbour(direction)}); }
  };
  const pending = !zone || !view;
  useEffect(() => {
    setToast(metaError ? {message: metaError, retry: () => setRevision(v => v + 1)} : null);
  }, [metaError]);
  const groups = new Map<string, Tile[]>();
  for (const tile of tiles) {
    const existing = groups.get(tile.id);
    if (existing) existing.push(tile); else groups.set(tile.id, [tile]);
  }
  const resizeOffset = useRef<number | null>(null);
  const [hoverFraction, setHoverFraction] = useState<number | null>(null);
  const tracks = useRef<HTMLDivElement>(null);
  const trackCount = groups.size, showTracks = !!view && !!zone;
  useEffect(() => {
    const container = tracks.current;
    if (!container || !showTracks || !timelineVisible) return;
    const ruler = container.querySelector(".recording-ruler");
    const lastBand = Array.from(container.querySelectorAll(".recording-band")).at(-1);
    if (!ruler || !lastBand) return;
    const measure = () => container.style.setProperty("--recording-marker-bottom",
      `${ruler.getBoundingClientRect().bottom - lastBand.getBoundingClientRect().bottom}px`);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    return () => observer.disconnect();
  }, [trackCount, showTracks, timelineVisible, tiles]);
  const zoom = (factor: number) => {
    if (!view) return;
    const anchor = position !== null && position >= view.start && position <= view.end ? (position - view.start) / (view.end - view.start) : .5;
    navigate(zoomRecordingWindow(view, factor, anchor));
  };
  const zoomValue = view ? Math.log(MAX_RECORDING_SPAN / (view.end - view.start)) / Math.log(MAX_RECORDING_SPAN / MIN_RECORDING_SPAN) * 100 : 0;
  const zoomSpan = view ? (view.end - view.start) / 60000 : 0;
  const zoomAmount = Number((zoomSpan >= 60 ? zoomSpan / 60 : zoomSpan).toFixed(1));
  const zoomUnit = zoomSpan >= 60 ? (zoomAmount === 1 ? "hora" : "horas") : (zoomAmount === 1 ? "minuto" : "minutos");
  return <section className={timelineVisible ? "live-recording-panel recording-panel" : "contents"} style={{"--recording-name-width": `${nameWidth}px`} as CSSProperties} aria-label="Gravações do mosaico">
    {toast && <div className="recording-toast" role="alert"><p>{toast.message}</p>
      {toast.retry && <Button variant="secondary" onClick={() => { setToast({...toast}); toast.retry?.(); }}>Tentar novamente</Button>}
      <Button variant="ghost" size="icon" aria-label="Dispensar notificação" onClick={() => setToast(null)}><X aria-hidden="true"/></Button>
    </div>}
    <div id="live-recording-timeline" hidden={!timelineVisible}>
      <div className="recording-controls" role="group" aria-label="Controles de reprodução de gravações">
        {view && zone && <RecordingDatePicker base={base} date={date} zone={zone} revision={revision} onSelect={value => {
          const at = recordingInstant(value, "00:00:00", zone);
          if (at === null) { setToast({message: "Este horário não existe no fuso do site. Escolha outro horário."}); return; }
          navigate({start: at, end: at + DEFAULT_RECORDING_SPAN});
        }}>
          <output className="recording-clock-value" aria-label="Horário da reprodução">
            <span className="recording-clock-time">{at !== null ? recordingClock(at, zone) : "--:--:--"}</span>
            <span className="recording-clock-date"><Calendar size={14} aria-hidden="true"/>{(at !== null ? recordingDate(at, zone) : date).split("-").reverse().join(":")}</span>
            {position === null && <span className="sr-only">Aguardando horário</span>}
          </output>
        </RecordingDatePicker>}
        <div className="recording-playback-controls">
        <Button variant="ghost" size="icon" aria-label="Gravação anterior" title="Gravação anterior" disabled={pending} onClick={() => void neighbour("previous")}><SkipBack aria-hidden="true"/></Button>
        <Button variant="ghost" size="icon" aria-label="Diminuir velocidade da gravação" title="Diminuir velocidade / retroceder" disabled={speed === RECORDING_SPEEDS[0]} onClick={() => setSpeed(value => RECORDING_SPEEDS[Math.max(0, RECORDING_SPEEDS.indexOf(value) - 1)])}><Rewind aria-hidden="true"/></Button>
        <Button variant="ghost" size="icon" aria-label="Voltar 10 segundos" title="Voltar 10 segundos" disabled={position === null} onClick={() => position !== null && seek(position - 10000)}><Undo2 aria-hidden="true"/></Button>
        <Button variant="default" size="icon" aria-label={playing ? "Pausar gravação" : "Reproduzir gravação"} title={playing ? "Pausar" : "Reproduzir"} disabled={position === null} onClick={() => setPlaying(value => !value)}>{playing ? <Pause aria-hidden="true"/> : <Play aria-hidden="true"/>}</Button>
        <Button variant="ghost" size="icon" aria-label="Avançar 10 segundos" title="Avançar 10 segundos" disabled={position === null} onClick={() => position !== null && seek(position + 10000)}><Redo2 aria-hidden="true"/></Button>
        <Button variant="ghost" size="icon" aria-label="Aumentar velocidade da gravação" title="Aumentar velocidade / avançar" disabled={speed === RECORDING_SPEEDS.at(-1)} onClick={() => setSpeed(value => RECORDING_SPEEDS[Math.min(RECORDING_SPEEDS.length - 1, RECORDING_SPEEDS.indexOf(value) + 1)])}><FastForward aria-hidden="true"/></Button>
        <Button variant="ghost" size="icon" aria-label="Próxima gravação" title="Próxima gravação" disabled={pending} onClick={() => void neighbour("next")}><SkipForward aria-hidden="true"/></Button>
        <output className="recording-speed" aria-label="Velocidade da gravação" aria-live="polite">{String(speed).replace(".", ",")}×</output>
        </div>
        <div className="recording-navigation-controls">
          <Button variant="ghost" size="icon" aria-label="Diminuir zoom da linha do tempo" disabled={!view || view.end - view.start >= MAX_RECORDING_SPAN} onClick={() => zoom(2)}><Minus aria-hidden="true"/></Button>
          <input className="recording-zoom-slider" type="range" min={0} max={100} step="any" value={zoomValue} disabled={!view} aria-label="Zoom da linha do tempo" aria-valuetext={view ? `Janela de ${zoomAmount} ${zoomUnit}` : "Carregando"} onChange={event => {
            if (view) zoom((MAX_RECORDING_SPAN * (MIN_RECORDING_SPAN / MAX_RECORDING_SPAN) ** (Number(event.target.value) / 100)) / (view.end - view.start));
          }}/>
          <Button variant="ghost" size="icon" aria-label="Aumentar zoom da linha do tempo" disabled={!view || view.end - view.start <= MIN_RECORDING_SPAN} onClick={() => zoom(.5)}><Plus aria-hidden="true"/></Button>
          <Button variant="ghost" size="icon" aria-label="Fechar painel de gravações" title="Fechar painel de gravações" onClick={onClose}><X aria-hidden="true"/></Button>
        </div>
      </div>
      {showTracks && trackCount > 0 && <div className="recording-name-divider" role="separator" tabIndex={0}
        aria-label="Largura do nome da câmera" aria-orientation="vertical" aria-valuemin={131} aria-valuemax={600} aria-valuenow={Math.max(131, nameWidth)}
        onPointerDown={event => {
          if (event.button !== 0 || !event.isPrimary) return;
          event.preventDefault();
          resizeOffset.current = event.clientX - event.currentTarget.getBoundingClientRect().left;
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={event => {
          if (resizeOffset.current === null || !event.currentTarget.hasPointerCapture(event.pointerId)) return;
          const panel = event.currentTarget.parentElement!.getBoundingClientRect();
          resizeName(Math.round(Math.max(131, Math.min(600, panel.width * .45, event.clientX - panel.left - resizeOffset.current))));
        }}
        onLostPointerCapture={() => { resizeOffset.current = null; }}
        onKeyDown={event => {
          const current = event.currentTarget.offsetLeft;
          const widths: Record<string, number> = {Home: 131, End: 600, ArrowLeft: current - 16, ArrowRight: current + 16};
          if (Object.hasOwn(widths, event.key)) { event.preventDefault(); resizeName(Math.max(131, Math.min(600, widths[event.key]))); }
        }}/>}
      <div ref={tracks}>
      {view && zone && Array.from(groups.values(), (group, index) => <RecordingTrack key={group[0].id} tenant={tenantId} tiles={group} at={at} playing={playing && !scrubbing} speed={speed} zone={zone} view={view} position={at} onPreview={setPreview} onCancelPreview={() => setPreview(null)} onNavigate={navigate} onSeek={seek} header={index === 0} hoverFraction={hoverFraction} onHover={setHoverFraction}/>)}
      </div>
    </div>
  </section>;
}
