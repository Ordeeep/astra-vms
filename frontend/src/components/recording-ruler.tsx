"use client";

import {useEffect, useMemo, useRef, useState} from "react";
import type {RecordingInterval} from "@/lib/recording-timeline";
import {recordingClock, recordingDate, recordingInstant, recordingLabel, recordingTicks, shiftRecordingDate, zoomRecordingWindow} from "@/lib/recording-navigation";

export function RecordingRuler({header, hoverFraction, onHover, name, zone, view, intervals, position, onNavigate, onSeek, onPreview, onCancelPreview}: {
  header: boolean; hoverFraction: number | null; onHover: (fraction: number | null) => void;
  name: string; zone: string; view: RecordingInterval; intervals: RecordingInterval[]; position: number | null;
  onNavigate: (view: RecordingInterval) => void; onSeek: (at: number) => void; onPreview: (at: number) => void; onCancelPreview: () => void;
}) {
  const scale = useRef<HTMLDivElement>(null), [width, setWidth] = useState(800);
  const [preview, setPreview] = useState<number | null>(null);
  const drag = useRef<{id: number; x: number; view: RecordingInterval; kind: "pan" | "seek"; moved: boolean} | null>(null);
  const latest = useRef({view, onNavigate});
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
  const fractionAt = (x: number) => {
    const box = scale.current!.getBoundingClientRect();
    return Math.max(0, Math.min(1, (x - box.left) / box.width));
  };
  const point = (x: number) => view.start + fractionAt(x) * span;
  const ticks = useMemo(() => header ? recordingTicks(view, zone, width) : [], [header, view, zone, width]);
  const marker = preview ?? position;
  const dates = useMemo(() => {
    const result: {date: string; at: number}[] = [];
  for (let date = recordingDate(view.start, zone), i = 0; i < 4; date = shiftRecordingDate(date, 1), i++) {
    const at = recordingInstant(date, "00:00:00", zone);
    if (at !== null && at < view.end) result.push({date, at: Math.max(view.start, at)});
  }
    return result;
  }, [view, zone]);
  return <div className="recording-ruler-row" data-compact={!header}>
    <div className="recording-camera-column"><strong className="recording-camera-name" title={name}>{name}</strong></div>
    <div className="recording-ruler" ref={scale} role="group" aria-label="Linha do tempo de gravações" tabIndex={0}
      onPointerDown={event => {
        if (event.button !== 0 || !event.isPrimary) return;
        const seek = !!(event.target as HTMLElement).closest("[data-recording-marker]");
        drag.current = {id: event.pointerId, x: event.clientX, view, kind: seek ? "seek" : "pan", moved: false};
        event.currentTarget.setPointerCapture(event.pointerId); event.preventDefault(); event.currentTarget.focus();
      }}
      onPointerMove={event => {
        const current = drag.current; onHover(fractionAt(event.clientX));
        if (!current || current.id !== event.pointerId) return;
        if (Math.abs(event.clientX - current.x) > 4) current.moved = true;
        if (current.kind === "seek") { const at = point(event.clientX); setPreview(at); onPreview(at); }
        else if (current.moved) { const offset = -(event.clientX - current.x) / scale.current!.clientWidth * (current.view.end - current.view.start); const next = {start: current.view.start + offset, end: current.view.end + offset}; onNavigate(next); }
      }}
      onPointerUp={event => {
        const current = drag.current; drag.current = null;
        if (current?.id === event.pointerId && (current.kind === "seek" || !current.moved)) onSeek(point(event.clientX));
        setPreview(null); if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
      }} onPointerCancel={() => { if (drag.current?.kind === "seek") onCancelPreview(); drag.current = null; setPreview(null); }} onLostPointerCapture={() => { if (drag.current?.kind === "seek") onCancelPreview(); drag.current = null; setPreview(null); }} onPointerLeave={event => {
        const next = event.relatedTarget;
        if (!drag.current && !(next instanceof Element && next.closest(".recording-ruler"))) onHover(null);
      }}
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
      {header && marker !== null && marker >= view.start && marker <= view.end && <div className="recording-marker" data-recording-marker role="slider" tabIndex={0} style={{left: `${left(marker)}%`}}
        aria-label="Posição de reprodução" aria-valuemin={view.start} aria-valuemax={view.end} aria-valuenow={marker} aria-valuetext={recordingLabel(marker, zone)}
        onKeyDown={event => { if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) { event.preventDefault(); event.stopPropagation(); onSeek(event.key === "Home" ? view.start : event.key === "End" ? view.end - 1 : marker + (event.key === "ArrowLeft" ? -1 : 1) * (event.shiftKey ? 10000 : 1000)); } }}><i/></div>}
      {header && hoverFraction !== null && <>
        <i className="recording-hover-line" style={{left: `${hoverFraction * 100}%`}}/>
        <output className="recording-hover-time" style={{left: `clamp(min(50%, 36px), ${hoverFraction * 100}%, max(50%, calc(100% - 36px)))`}}>
          {recordingClock(view.start + hoverFraction * span, zone)}
        </output>
      </>}
    </div>
  </div>;
}
