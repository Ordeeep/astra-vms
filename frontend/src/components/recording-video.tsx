"use client";

import {useEffect, useRef, useState} from "react";
import {Button} from "@/components/ui/button";
import Hls from "hls.js";
import {LoaderCircle} from "lucide-react";
import {ApiError, captureScopedAPI, mediaContext} from "@/lib/api";
import {archiveVideoCodecs} from "@/lib/video-compatibility";
import {recordingSeekInstant, recordingTimestamp, type RecordingMapping} from "@/lib/recording-navigation";

type Session = {id: string; manifest: string};
type Manifest = {mapping: RecordingMapping[]};

// Each cell follows the mosaic clock, including after buffering and during reverse playback.
export function RecordingVideo({tenant, camera, at, playing, speed}: {
  tenant: string; camera: string; at: number; playing: boolean; speed: number;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const target = useRef({at, playing, speed});
  target.current = {at, playing, speed};
  const [message, setMessage] = useState(""), [loading, setLoading] = useState(true);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const element = video.current!;
    const scoped = captureScopedAPI(), context = mediaContext();
    let active = true, opening = false, failed = false, compatibility = false;
    let session: Session | undefined, player: Hls | undefined;
    let parts: {start: number; end: number; mediaStart: number}[] = [];
    const remove = (s: Session) => void scoped(`/tenants/${tenant}/playback-sessions/${s.id}`, "DELETE").catch(() => {});
    const close = () => {
      element.onerror = null;
      player?.destroy(); player = undefined;
      element.pause(); element.removeAttribute("src"); element.load();
      if (session) remove(session);
      session = undefined; parts = [];
    };
    const fail = (reason: string, adapt = false) => {
      if (!active) return;
      close();
      if (adapt && !compatibility) { compatibility = true; return; }
      failed = true; setLoading(false); setMessage(reason);
    };
    async function open(at: number) {
      opening = true; setLoading(true); setMessage(""); close();
      try {
        let start = at;
        if (target.current.speed < 0) {
          const query = new URLSearchParams({start: new Date(at - 8000).toISOString(), end: new Date(at + 1).toISOString()});
          const result = await scoped<{intervals: {start: string; end: string}[]}>(`/tenants/${tenant}/cameras/${camera}/recording-availability?${query}`);
          if (!active) return;
          const block = result.intervals.find(i => recordingTimestamp(i.start) <= at && recordingTimestamp(i.end) > at);
          if (block) start = recordingSeekInstant(Math.max(at - 8000, recordingTimestamp(block.start)));
        }
        const opened = await scoped<Session>(`/tenants/${tenant}/playback-sessions`, "POST", {
          cameraId: camera, start: new Date(start).toISOString(), end: new Date(start < at ? at + 1 : at + 3600000).toISOString(),
          codecs: archiveVideoCodecs(), forceCompatibility: compatibility,
        });
        if (!active || !context.valid()) { remove(opened); return; }
        session = opened;
        const url = new URL(opened.manifest, location.origin); url.searchParams.set("format", "json");
        const manifest = await scoped<Manifest>(url.pathname.replace(/^\/api\/v1/, "") + url.search);
        if (!active || session !== opened) return;
        parts = manifest.mapping.map(p => ({start: recordingTimestamp(p.start), end: recordingTimestamp(p.end), mediaStart: p.mediaStart}));
        const part = parts.find(p => p.start <= at && p.end > at);
        if (!part || !Hls.isSupported()) { fail("Não foi possível reproduzir este trecho."); return; }
        player = new Hls({startPosition: part.mediaStart + (at - part.start) / 1000, maxBufferLength: 3, maxMaxBufferLength: 5, backBufferLength: 12,
          xhrSetup(xhr) { if (!active || !context.valid()) { xhr.abort(); return; } xhr.withCredentials = true; xhr.setRequestHeader("X-VMS-Context", context.headers["X-VMS-Context"]); },
        });
        player.on(Hls.Events.ERROR, (_, data) => {
          if (session === opened && data.fatal) fail("Não foi possível reproduzir este trecho.", data.type === Hls.ErrorTypes.MEDIA_ERROR);
        });
        element.onerror = () => { if (session === opened) fail("Falha ao decodificar a gravação.", true); };
        player.loadSource(opened.manifest); player.attachMedia(element);
      } catch (error) {
        if (active) fail(error instanceof ApiError && ["recording_gap","recording_unavailable"].includes(error.code) ? "Sem gravação neste horário." : (error as Error).message);
      } finally { opening = false; }
    }
    const sync = () => {
      if (opening || failed || !context.valid()) return;
      const {at, speed, playing} = target.current;
      const part = parts.find(p => p.start <= at && p.end > at);
      if (!part) { void open(Math.ceil(at)); return; }
      if (element.readyState < 1 || element.seeking) return;
      const offset = part.mediaStart + (at - part.start) / 1000;
      const forward = playing && speed > 0;
      element.playbackRate = Math.abs(speed);
      if (!forward) element.pause();
      if (Math.abs(element.currentTime - offset) > (forward ? .25 * Math.max(1, speed) : .025)) element.currentTime = offset;
      if (element.readyState >= 2) {
        setLoading(false);
        if (forward && element.paused) void element.play().catch(error => { if (active && error.name !== "AbortError") fail("Não foi possível iniciar a reprodução."); });
      }
    };
    let renewing = false;
    const heartbeat = setInterval(() => {
      const current = session;
      if (!current || renewing) return;
      renewing = true;
      void scoped(`/tenants/${tenant}/playback-sessions/${current.id}`, "PUT", {}, 6000).catch(() => {
        if (active && session === current) fail("Consulta encerrada. Verifique suas permissões.");
      }).finally(() => { renewing = false; });
    }, 2000);
    sync();
    const timer = setInterval(sync, 100);
    return () => { active = false; clearInterval(timer); clearInterval(heartbeat); close(); };
  }, [tenant, camera, retry]);
  return <div className="live-video-stack recording-video">
    <video ref={video} muted playsInline aria-label="Vídeo gravado"/>
    {loading && <div className="video-state" role="status"><LoaderCircle className="animate-spin" aria-label="Carregando gravação…"/></div>}
    {message && <div className="recording-video-message" role="alert">{message}<Button variant="secondary" onClick={() => setRetry(v => v + 1)}>Tentar novamente</Button></div>}
  </div>;
}
