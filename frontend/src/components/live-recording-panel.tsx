"use client";

import { useEffect, useState } from "react";
import { Circle, LoaderCircle, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { allPages } from "@/components/management";
import { api } from "@/lib/api";
import { onEnvironmentChange } from "@/lib/events";
import { localDateValue, recordingDay, recordingIntervals, recordingWindows, type RecordingSegment } from "@/lib/recording-timeline";

type Camera = { id: string; name: string };
type Availability = { key: string; segments: RecordingSegment[]; loading: boolean; error: string };

export function LiveRecordingPanel({ tenantId, camera, onClose }: {
  tenantId: string; camera: Camera; onClose: () => void;
}) {
  const [date, setDate] = useState(() => localDateValue());
  const [revision, setRevision] = useState(0);
  const key = `${tenantId}/${camera.id}/${date}/${revision}`;
  const [availability, setAvailability] = useState<Availability>({ key: "", segments: [], loading: true, error: "" });
  const day = recordingDay(date);
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

  useEffect(() => onEnvironmentChange([], () => setRevision(value => value + 1)), []);
  useEffect(() => {
    let active = true, busy = false;
    const day = recordingDay(date);
    setAvailability({ key, segments: [], loading: true, error: "" });
    async function refresh() {
      if (!day || busy || !active) return;
      busy = true;
      try {
        // Historical access may differ from the live camera's current site.
        const cameras = await allPages<Camera>(`/tenants/${tenantId}/recording-cameras`);
        if (!active) return;
        if (!cameras.some(item => item.id === camera.id)) throw new Error("Gravações desta câmera indisponíveis para sua conta.");
        const pages = await Promise.all(recordingWindows(day).map(window => {
          const query = new URLSearchParams({ start: new Date(window.start).toISOString(), end: new Date(window.end).toISOString() });
          return api<{ items: RecordingSegment[] }>(`/tenants/${tenantId}/cameras/${camera.id}/timeline?${query}`, "GET", undefined, true);
        }));
        if (active) setAvailability({ key, segments: pages.flatMap(page => page.items), loading: false, error: "" });
      } catch (error) {
        if (active) setAvailability({ key, segments: [], loading: false, error: (error as Error).message });
      } finally { busy = false; }
    }
    void refresh();
    const timer = setInterval(() => void refresh(), 30_000);
    return () => { active = false; clearInterval(timer); };
  }, [tenantId, camera.id, date, key]);

  const current = availability.key === key;
  const loading = !current || availability.loading;
  const error = current ? availability.error : "";
  const intervals = day && current && !error ? recordingIntervals(availability.segments, day) : [];
  const clock = (at: number) => new Date(at).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  const ticks = day ? Array.from({ length: 9 }, (_, index) => {
    const at = new Date(day.start);
    at.setHours(index * 3, 0, 0, 0);
    return { label: `${String(index * 3).padStart(2, "0")}:00`, left: (at.getTime() - day.start) / (day.end - day.start) * 100 };
  }) : [];

  return <section className="live-recording-panel" aria-label="Gravações da câmera">
    <header className="live-recording-header">
      <span className="live-recording-heading"><Circle aria-hidden="true"/>Gravação</span>
      <Input type="date" aria-label="Data das gravações" value={date} onChange={event => {
        if (recordingDay(event.target.value)) setDate(event.target.value);
      }}/>
      <span className="live-recording-timezone">{timeZone}</span>
      <Button type="button" variant="ghost" size="icon" className="live-recording-close" aria-label="Fechar painel de gravações" onClick={onClose}><X aria-hidden="true"/></Button>
    </header>
    <div className="live-recording-scroll">
      <div className="live-recording-row">
        <strong className="live-recording-camera" title={camera.name}>{camera.name}</strong>
        <div className="live-recording-scale">
          <div className="live-recording-ticks" aria-hidden="true">{ticks.map(tick => <span key={tick.label} style={{ left: `${tick.left}%` }}>{tick.label}</span>)}</div>
          <div className="live-recording-track" role="img" aria-label={`Disponibilidade de gravações de ${camera.name} em ${date}`} aria-busy={loading}>
            {ticks.map(tick => <i className="live-recording-gridline" key={tick.label} style={{ left: `${tick.left}%` }}/>)}
            {day && intervals.map(interval => <span className="live-recording-interval" key={interval.start}
              title={`${clock(interval.start)} – ${interval.end === day.end ? "24:00:00" : clock(interval.end)}`}
              style={{ left: `${(interval.start - day.start) / (day.end - day.start) * 100}%`, width: `${(interval.end - interval.start) / (day.end - day.start) * 100}%` }}/>) }
          </div>
        </div>
      </div>
    </div>
    <div className="live-recording-status" role={error ? "alert" : "status"}>
      {loading ? <><LoaderCircle className="animate-spin" aria-hidden="true"/>Consultando gravações…</> : error ? error : intervals.length === 0 ? "Nenhuma gravação disponível para sua conta nesta data." : <><span className="live-recording-legend"/>Gravações disponíveis · Intervalos vazios: sem imagens disponíveis para sua conta.</>}
    </div>
  </section>;
}
