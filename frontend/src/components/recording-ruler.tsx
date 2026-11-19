"use client";

import {useEffect, useRef, useState, type ReactNode} from "react";
import type {RecordingInterval} from "@/lib/recording-timeline";
import {recordingDate, recordingInstant, recordingLabel, recordingTicks, shiftRecordingDate, zoomRecordingWindow} from "@/lib/recording-navigation";

export function RecordingRuler({clock, nameWidth, onNameWidthChange, name, zone, view, intervals, position, onNavigate, onSeek}: {
  clock: ReactNode;
  nameWidth: number; onNameWidthChange: (width: number) => void;
  name: string; zone: string; view: RecordingInterval; intervals: RecordingInterval[]; position: number | null;
  onNavigate: (view: RecordingInterval) => void; onSeek: (at: number) => void;
}) {
  const row = useRef<HTMLDivElement>(null), scale = useRef<HTMLDivElement>(null), [width, setWidth] = useState(800);
  const [hover, setHover] = useState<number | null>(null), [preview, setPreview] = useState<number | null>(null);
  const drag = useRef<{id: number; x: number; view: RecordingInterval; kind: "pan" | "seek"; moved: boolean} | null>(null);
  const resize = useRef<{id: number; x: number; width: number} | null>(null), latest = useRef({view, onNavigate});
  latest.current = {view, onNavigate};
  useEffect(() => {
    const element = scale.current; if (!element) return;
    const observer = new ResizeObserver(() => setWidth(element.clientWidth)); observer.observe(element);
    const wheel = (event: WheelEvent) => {
      if (event.ctrlKey || event.metaKey || !event.deltaY) return;
      event.preventDefault();
      const box = element.getBoundingClientRect();
      latest.current.onNavigate(zoomRecordingWindow(latest.current.view, event.deltaY < 0 ? 1 / 1.5 : 1.5, (event.clientX - box.left) / box.width));
    };
    element.addEventListener("wheel", wheel, {passive: false});
    return () => { observer.disconnect(); element.removeEventListener("wheel", wheel); };
  }, []);
  const span = view.end - view.start, left = (at: number) => (at - view.start) / span * 100;
  const point = (x: number, window = view) => {
    const box = scale.current!.getBoundingClientRect();
    return window.start + Math.max(0, Math.min(1, (x - box.left) / box.width)) * (window.end - window.start);
  };
  const ticks = recordingTicks(view, zone, width), marker = preview ?? position;
  const dates: {date: string; at: number}[] = [];
  for (let date = recordingDate(view.start, zone), i = 0; i < 4; date = shiftRecordingDate(date, 1), i++) {
    const at = recordingInstant(date, "00:00:00", zone);
    if (at !== null && at < view.end) dates.push({date, at: Math.max(view.start, at)});
  }
  return <div className="recording-ruler-row" ref={row}>
    <div className="recording-camera-column">{clock}<strong className="recording-camera-name" title={name}>{name}</strong></div>
    <div className="recording-name-divider" role="separator" tabIndex={0} aria-label="Largura do nome da câmera" aria-orientation="vertical" aria-valuemin={64} aria-valuemax={600} aria-valuenow={nameWidth}
      onPointerDown={event => { if (event.button !== 0) return; event.preventDefault(); resize.current = {id: event.pointerId, x: event.clientX, width: event.currentTarget.previousElementSibling!.getBoundingClientRect().width}; event.currentTarget.setPointerCapture(event.pointerId); }}
      onPointerMove={event => { const current = resize.current; if (current?.id === event.pointerId) onNameWidthChange(Math.round(Math.max(64, Math.min(600, (row.current?.clientWidth ?? 1000) * .45, current.width + event.clientX - current.x)))); }}
      onPointerUp={() => { resize.current = null; }} onPointerCancel={() => { resize.current = null; }}
      onKeyDown={event => { if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) { event.preventDefault(); onNameWidthChange(event.key === "Home" ? 64 : event.key === "End" ? 600 : Math.max(64, Math.min(600, nameWidth + (event.key === "ArrowLeft" ? -16 : 16)))); } }}/>
    <div className="recording-ruler" ref={scale} role="group" aria-label="Linha do tempo de gravações" tabIndex={0}
      onPointerDown={event => {
        if (event.button !== 0 || !event.isPrimary) return;
        const seek = !!(event.target as HTMLElement).closest("[data-recording-marker]");
        drag.current = {id: event.pointerId, x: event.clientX, view, kind: seek ? "seek" : "pan", moved: false};
        event.currentTarget.setPointerCapture(event.pointerId); event.preventDefault(); event.currentTarget.focus();
      }}
      onPointerMove={event => {
        const current = drag.current; setHover(point(event.clientX));
        if (!current || current.id !== event.pointerId) return;
        if (Math.abs(event.clientX - current.x) > 4) current.moved = true;
        if (current.kind === "seek") setPreview(point(event.clientX));
        else if (current.moved) { const offset = -(event.clientX - current.x) / scale.current!.clientWidth * (current.view.end - current.view.start); const next = {start: current.view.start + offset, end: current.view.end + offset}; setHover(point(event.clientX, next)); onNavigate(next); }
      }}
      onPointerUp={event => {
        const current = drag.current; drag.current = null;
        if (current?.id === event.pointerId && (current.kind === "seek" || !current.moved)) onSeek(point(event.clientX));
        setPreview(null); if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
      }} onPointerCancel={() => { drag.current = null; setPreview(null); }} onLostPointerCapture={() => { drag.current = null; setPreview(null); }} onPointerLeave={() => { if (!drag.current) setHover(null); }}
      onKeyDown={event => {
        if (event.target !== event.currentTarget) return;
        if (["ArrowLeft", "ArrowRight"].includes(event.key)) { event.preventDefault(); const delta = span * (event.key === "ArrowLeft" ? -.1 : .1); onNavigate({start: view.start + delta, end: view.end + delta}); }
        if (event.key === "Enter") { event.preventDefault(); onSeek((view.start + view.end) / 2); }
        if (["+", "=", "-"].includes(event.key)) { event.preventDefault(); onNavigate(zoomRecordingWindow(view, event.key === "-" ? 2 : .5)); }
      }}>
      <div className="recording-date-scale" aria-hidden="true">{dates.map(day => <span key={day.date} style={{left: `${left(day.at)}%`}}>{day.date.split("-").reverse().join("/")}</span>)}</div>
      <div className="recording-hour-scale" aria-hidden="true">{ticks.map(tick => <i key={tick.at} data-major={tick.major} style={{left: `${left(tick.at)}%`}}>{tick.label && <span style={{transform: left(tick.at) < 3 ? "none" : left(tick.at) > 97 ? "translateX(-100%)" : undefined}}>{tick.label}</span>}</i>)}</div>
      <div className="recording-band" aria-label="Períodos gravados">
        {intervals.map(interval => {
          const start = Math.max(interval.start, view.start), end = Math.min(interval.end, view.end);
          return end > start && <span key={`${interval.start}/${interval.end}`} className="recording-available" style={{left: `${left(start)}%`, width: `${(end - start) / span * 100}%`}}/>;
        })}
        {dates.slice(1).map(day => <i className="recording-day-boundary" key={day.date} style={{left: `${left(day.at)}%`}}/>)}
      </div>
      {marker !== null && marker >= view.start && marker <= view.end && <div className="recording-marker" data-recording-marker role="slider" tabIndex={0} style={{left: `${left(marker)}%`}}
        aria-label="Posição de reprodução" aria-valuemin={view.start} aria-valuemax={view.end} aria-valuenow={marker} aria-valuetext={recordingLabel(marker, zone)}
        onKeyDown={event => { if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) { event.preventDefault(); event.stopPropagation(); onSeek(event.key === "Home" ? view.start : event.key === "End" ? view.end - 1 : marker + (event.key === "ArrowLeft" ? -1 : 1) * (event.shiftKey ? 10000 : 1000)); } }}><i/></div>}
      {hover !== null && <><i className="recording-hover-line" style={{left: `${left(hover)}%`}}/><output className="recording-hover-time" style={{left: Math.max(Math.min(width / 2, 95), Math.min(width - Math.min(width / 2, 95), left(hover) / 100 * width))}}>{recordingLabel(hover, zone)}</output></>}
    </div>
  </div>;
}
