"use client";

import "./recording-timeline.css";

import {useCallback, useEffect, useRef, useState, type CSSProperties} from "react";
import {createPortal} from "react-dom";
import {ChevronDown, ChevronUp, FastForward, Rewind, LoaderCircle, Minus, Pause, Play, Plus, SkipBack, SkipForward, Undo2, Redo2, X} from "lucide-react";
import {Button} from "@/components/ui/button";
import {RecordingDatePicker} from "@/components/recording-date-picker";
import {RecordingRuler} from "@/components/recording-ruler";
import {RecordingVideo, type RecordingRequest, type RecordingVideoState} from "@/components/recording-video";
import {api} from "@/lib/api";
import type {Notification} from "@/lib/events";
import type {RecordingInterval, RecordingSegment} from "@/lib/recording-timeline";
import {DEFAULT_RECORDING_SPAN, MAX_RECORDING_SPAN, MIN_RECORDING_SPAN, initialRecordingWindow, recordingClock, recordingDate, recordingInstant, recordingSeekInstant, recordingTimestamp, zoomRecordingWindow} from "@/lib/recording-navigation";

const RECORDING_SPEEDS = [-8, -4, -2, -1, -.5, .5, 1, 2, 4, 8];

type Availability = {timezone: string; intervals: RecordingSegment[]};
const numericIntervals = (value: Availability) => value.intervals.map(interval => ({start: recordingTimestamp(interval.start), end: recordingTimestamp(interval.end)}));

export function LiveRecordingPanel({tenantId, camera, host, onClose}: {
  tenantId: string; camera: {id: string; name: string}; host: HTMLElement | null; onClose: () => void;
}) {
  const base = `/tenants/${tenantId}/cameras/${camera.id}/recording-availability`;
  const [nameWidth, setNameWidth] = useState(190);
  useEffect(() => { try { const saved = Number(localStorage.getItem("astra.recording-name-width")); if (saved >= 64 && saved <= 600) setNameWidth(saved); } catch {} }, []);
  const resizeName = (value: number) => { setNameWidth(value); try { localStorage.setItem("astra.recording-name-width", String(value)); } catch {} };
  const [zone, setZone] = useState(""), [metaError, setMetaError] = useState("");
  const [view, setView] = useState<RecordingInterval | null>(null), [revision, setRevision] = useState(0), [compact, setCompact] = useState(false);
  const [availability, setAvailability] = useState({key: "", intervals: [] as RecordingInterval[], error: "", loading: true});
  const [request, setRequest] = useState<RecordingRequest | null>(null), [position, setPosition] = useState<number | null>(null);
  const [playing, setPlaying] = useState(false), [speed, setSpeed] = useState(1), [follow, setFollow] = useState(true);
  const [state, setState] = useState<RecordingVideoState>({phase: "ready", message: "Escolha um horário na linha do tempo."});
  const serial = useRef(0), navigation = useRef(0), selected = useRef(false), playable = useRef(false), live = useRef({playing, request, follow, position, speed});
  live.current = {playing, request, follow, position, speed};
  const key = `${base}/${view?.start}/${view?.end}/${revision}`;

  useEffect(() => {
    let active = true;
    void api<{timezone: string}>(base, "GET", undefined, true).then(value => {
      if (active) { setZone(value.timezone); setMetaError(""); setView(current => current ?? initialRecordingWindow(value.timezone)); }
    }).catch(error => {
      if (active) { serial.current++; navigation.current++; setMetaError((error as Error).message); setZone(""); setRequest(null); setPlaying(false); }
    });
    return () => { active = false; };
  }, [base, revision]);
  useEffect(() => {
    const receive = (event: Event) => {
      const value = (event as CustomEvent<Notification>).detail;
      if (["AuthorizationChanged", "ResyncRequired"].includes(value.type)) {
        serial.current++; navigation.current++; setRequest(null); setPlaying(false); setPosition(null); selected.current = false; playable.current = false;
        setAvailability({key: "", intervals: [], error: "", loading: true});
        setState({phase: "ready", message: "Acesso atualizado. Escolha novamente um horário."});
      }
      if (["AuthorizationChanged", "ResyncRequired", "Connected", "InventoryChanged", "ArchiveChanged", "RecordingChanged"].includes(value.type)) setRevision(value => value + 1);
    };
    window.addEventListener("vms:change", receive);
    return () => { window.removeEventListener("vms:change", receive); serial.current++; navigation.current++; };
  }, []);
  useEffect(() => {
    if (!view || !zone) return;
    let active = true, busy = false;
    const query = new URLSearchParams({start: new Date(view.start).toISOString(), end: new Date(view.end).toISOString()});
    const refresh = async () => {
      if (busy) return; busy = true;
      try {
        const value = await api<Availability>(`${base}?${query}`, "GET", undefined, true);
        if (active) setAvailability({key, intervals: numericIntervals(value), error: "", loading: false});
      } catch (error) { if (active) setAvailability({key, intervals: [], error: (error as Error).message, loading: false}); }
      finally { busy = false; }
    };
    const delay = setTimeout(() => void refresh(), 140), timer = setInterval(() => void refresh(), 30_000);
    return () => { active = false; clearTimeout(delay); clearInterval(timer); };
  }, [base, key, view, zone]);
  const center = view ? (view.start + view.end) / 2 : Date.now(), date = zone ? recordingDate(center, zone) : "";

  const navigate = (window: RecordingInterval) => { setFollow(false); setView(window); };
  const seek = useCallback((at: number, forcePlaying?: boolean) => {
    at = recordingSeekInstant(at);
    if (!Number.isFinite(at)) return;
    navigation.current++;
    const play = forcePlaying ?? (!selected.current || live.current.playing);
    selected.current = true; setPosition(at); setPlaying(play);
    setState({phase: "loading", message: "Preparando gravação…"});
    setRequest({at, serial: ++serial.current});
  }, []);
  const onPosition = useCallback((at: number) => {
    setPosition(at);
    if (live.current.follow) setView(current => {
      if (!current) return current;
      const span = current.end - current.start;
      return at < current.start || at > current.end - span * .06 ? {start: at - span * .3, end: at + span * .7} : current;
    });
  }, []);
  const onState = useCallback((value: RecordingVideoState) => {
    if (value.phase === "ready") playable.current = true;
    if (["error", "gap"].includes(value.phase) && !playable.current) selected.current = false;
    setState(value); if (value.phase === "error" || value.phase === "gap") setPlaying(false);
  }, []);
  const finish = useCallback(async (at: number, next: number | undefined, finishedSerial: number) => {
    if (serial.current !== finishedSerial) return;
    if (next !== undefined) { setPlaying(false); setState({phase: "gap", message: "Sem gravação neste período.", next}); return; }
    try {
      const query = new URLSearchParams({start: new Date(at).toISOString(), end: new Date(at + 2000).toISOString()});
      const value = await api<Availability>(`${base}?${query}`, "GET", undefined, true);
      if (serial.current !== finishedSerial) return;
      const following = numericIntervals(value).find(interval => interval.start <= at + .001 && interval.end > at + 1);
      if (following) { seek(at, live.current.playing); return; }
      setPlaying(false); setState({phase: "ended", message: "Fim da gravação disponível. Você pode escolher outro horário ou voltar ao vivo."});
    } catch (error) { if (serial.current === finishedSerial) { setPlaying(false); setState({phase: "error", message: (error as Error).message}); } }
  }, [base, seek]);
  const neighbour = async (direction: "previous" | "next", resume?: boolean) => {
    const current = ++navigation.current;
    try {
      const query = new URLSearchParams({direction, at: new Date(live.current.position ?? center).toISOString()});
      const value = await api<{interval: RecordingSegment | null}>(`${base}?${query}`, "GET", undefined, true);
      if (current !== navigation.current) return;
      if (!value.interval) { setState(previous => ({...previous, message: direction === "next" ? "Nenhuma gravação seguinte disponível." : "Nenhuma gravação anterior disponível."})); return; }
      const at = direction === "previous" && live.current.speed < 0 ? Math.floor(recordingTimestamp(value.interval.end)) - 1 : recordingSeekInstant(value.interval.start);
      seek(at, resume); setFollow(true);
      setView(current => current ? {start: at - (current.end - current.start) * .3, end: at + (current.end - current.start) * .7} : current);
    } catch (error) { if (current === navigation.current) setState(previous => ({...previous, message: (error as Error).message})); }
  };
  const pending = !zone || !view, current = availability.key === key, loading = pending || !current || availability.loading;
  const error = metaError || (current ? availability.error : "");
  const intervals = current && !error ? availability.intervals : [];
  const canPlay = !!request && !["gap", "ended"].includes(state.phase);
  const zoom = (factor: number) => {
    if (!view) return;
    const anchor = position !== null && position >= view.start && position <= view.end ? (position - view.start) / (view.end - view.start) : .5;
    navigate(zoomRecordingWindow(view, factor, anchor));
  };
  const video = host && createPortal(<>
    <RecordingVideo tenant={tenantId} camera={camera.id} request={request} playing={playing} speed={speed} onPosition={onPosition} onState={onState} onEnded={finish}/>
    {request && ["gap", "ended", "error"].includes(state.phase) && <div className="recording-video-message" role={state.phase === "error" ? "alert" : "status"}><p>{state.message}</p>
      {state.phase === "error" ? <Button variant="secondary" onClick={() => seek(position ?? request.at, true)}>Tentar novamente</Button> : <Button variant="secondary" onClick={() => void neighbour(speed < 0 ? "previous" : "next", true)}>{speed < 0 ? "Ir para a gravação anterior" : "Ir para a próxima gravação"}</Button>}
    </div>}
  </>, host);

  return <section className="live-recording-panel recording-panel" data-compact={compact} style={{"--recording-name-width": `${nameWidth}px`} as CSSProperties} aria-label="Gravações da câmera">
    {video}
    <header className="recording-toolbar">
      {compact && <strong className="recording-compact-name" title={camera.name}>{camera.name}</strong>}
    <div className="recording-panel-status" role={error || state.phase === "error" ? "alert" : "status"}>
      {error ? <>{error}<Button variant="ghost" onClick={() => setRevision(value => value + 1)}>Tentar consultar novamente</Button></> : loading ? <><LoaderCircle className="animate-spin" aria-hidden="true"/>Consultando gravações…</> : !request && !intervals.length ? "Nenhuma gravação disponível para sua conta neste período. Escolha outra data ou horário." : state.message || (intervals.length ? "Faixas azuis: gravações disponíveis · Arraste para navegar; clique para reproduzir." : "Nenhuma gravação disponível para sua conta neste período.")}
    </div>
      <div className="recording-toolbar-end"><Button variant="ghost" size="icon" aria-label={compact ? "Expandir barra de gravações" : "Recolher barra de gravações"} onClick={() => setCompact(value => !value)}>{compact ? <ChevronUp aria-hidden="true"/> : <ChevronDown aria-hidden="true"/>}</Button><Button variant="ghost" size="icon" aria-label="Fechar painel de gravações" onClick={onClose}><X aria-hidden="true"/></Button></div>
    </header>
    <div className="recording-controls" role="group" aria-label="Controles de reprodução de gravações">
      <div className="recording-playback-controls">
      <Button variant="ghost" size="icon" aria-label="Gravação anterior" title="Gravação anterior" disabled={pending} onClick={() => void neighbour("previous")}><SkipBack aria-hidden="true"/></Button>
      <Button variant="ghost" size="icon" aria-label="Diminuir velocidade da gravação" title="Diminuir velocidade / retroceder" disabled={speed === RECORDING_SPEEDS[0]} onClick={() => setSpeed(value => RECORDING_SPEEDS[Math.max(0, RECORDING_SPEEDS.indexOf(value) - 1)])}><Rewind aria-hidden="true"/></Button>
      <Button variant="ghost" size="icon" aria-label="Voltar 10 segundos" title="Voltar 10 segundos" disabled={position === null} onClick={() => position !== null && seek(position - 10000)}><Undo2 aria-hidden="true"/></Button>
      <Button variant="default" size="icon" aria-label={playing ? "Pausar gravação" : "Reproduzir gravação"} title={playing ? "Pausar" : "Reproduzir"} disabled={!canPlay} onClick={() => {
        if (state.phase === "error" && position !== null) seek(position, true);
        else setPlaying(value => !value);
      }}>{playing ? <Pause aria-hidden="true"/> : <Play aria-hidden="true"/>}</Button>
      <Button variant="ghost" size="icon" aria-label="Avançar 10 segundos" title="Avançar 10 segundos" disabled={position === null} onClick={() => position !== null && seek(position + 10000)}><Redo2 aria-hidden="true"/></Button>
      <Button variant="ghost" size="icon" aria-label="Aumentar velocidade da gravação" title="Aumentar velocidade / avançar" disabled={speed === RECORDING_SPEEDS.at(-1)} onClick={() => setSpeed(value => RECORDING_SPEEDS[Math.min(RECORDING_SPEEDS.length - 1, RECORDING_SPEEDS.indexOf(value) + 1)])}><FastForward aria-hidden="true"/></Button>
      <Button variant="ghost" size="icon" aria-label="Próxima gravação" title="Próxima gravação" disabled={pending} onClick={() => void neighbour("next")}><SkipForward aria-hidden="true"/></Button>
      <output className="recording-speed" aria-label="Velocidade da gravação" aria-live="polite">{String(speed).replace(".", ",")}×</output>
      </div>
      {date && <RecordingDatePicker base={base} date={date} revision={revision} onSelect={value => {
        if (!view) return;
        const at = recordingInstant(value, "00:00:00", zone);
        if (at === null) { setState(previous => ({...previous, message: "Este horário não existe no fuso do site. Escolha outro horário."})); return; }
        navigate({start: at, end: at + DEFAULT_RECORDING_SPAN});
      }}>
      <output className="recording-clock-value" aria-label="Horário da reprodução">
        <span className="recording-clock-time">{position !== null && zone ? recordingClock(position, zone) : "--:--:--"}</span>
        <span className="recording-clock-date">{(position !== null && zone ? recordingDate(position, zone) : date).split("-").reverse().join(":") || "--:--:----"}</span>
        {position === null && <span className="sr-only">Aguardando horário</span>}
      </output>
      </RecordingDatePicker>}
      {!compact && <div className="recording-navigation-controls">
        <Button variant="ghost" size="icon" aria-label="Diminuir zoom da linha do tempo" disabled={!view || view.end - view.start >= MAX_RECORDING_SPAN} onClick={() => zoom(2)}><Minus aria-hidden="true"/></Button>
        <Button variant="ghost" size="icon" aria-label="Aumentar zoom da linha do tempo" disabled={!view || view.end - view.start <= MIN_RECORDING_SPAN} onClick={() => zoom(.5)}><Plus aria-hidden="true"/></Button>
      </div>}
    </div>
    {!compact && view && zone && <RecordingRuler nameWidth={nameWidth} onNameWidthChange={resizeName} name={camera.name} zone={zone} view={view} intervals={intervals} position={position} onNavigate={navigate} onSeek={seek}/>}

  </section>;
}
