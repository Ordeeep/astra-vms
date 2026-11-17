"use client";

import {useEffect, useRef, useState} from "react";
import Hls from "hls.js";
import {LoaderCircle} from "lucide-react";
import {ApiError, captureScopedAPI, mediaContext} from "@/lib/api";
import {archiveVideoCodecs} from "@/lib/video-compatibility";
import {originalRecordingInstant, recordingSeekInstant, recordingTimestamp, type RecordingMapping} from "@/lib/recording-navigation";

export type RecordingRequest = {at: number; serial: number};
export type RecordingVideoState = {phase: "loading" | "ready" | "gap" | "ended" | "error"; message: string; next?: number};
type Session = {id: string; manifest: string};
type Manifest = {mapping: RecordingMapping[]; offset: number; nextStart: string | null};

// Owns one bounded HLS session. Only committed seeks replace the transport;
// Reverse playback seeks decoded frames because browsers reject negative rates.
// Sessions are replaced only on seeks or when crossing the loaded time window.
export function RecordingVideo({tenant, camera, request, playing, speed, onPosition, onState, onEnded}: {
  tenant: string; camera: string; request: RecordingRequest | null; playing: boolean; speed: number;
  onPosition: (at: number) => void; onState: (state: RecordingVideoState) => void;
  onEnded: (at: number, next: number | undefined, serial: number) => void;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const latest = useRef({playing, speed, onPosition, onState, onEnded});
  latest.current = {playing, speed, onPosition, onState, onEnded};
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    const element = video.current;
    if (!element) return;
    element.playbackRate = Math.abs(speed);
    if (!playing || speed < 0) element.pause();
    else if (element.readyState >= 2) void element.play().catch(() => latest.current.onState({phase: "error", message: "Não foi possível iniciar. Tente reproduzir novamente."}));
  }, [playing, speed]);

  useEffect(() => {
    if (!request || !video.current) return;
    const element: HTMLVideoElement = video.current;
    const scoped = captureScopedAPI(), context = mediaContext();
    let active = true, generation = 0, retried = false, hls: Hls | undefined, session: Session | undefined;
    let timer: ReturnType<typeof setInterval> | undefined, reverseTimer: ReturnType<typeof setInterval> | undefined, lastInstant = request.at;
    const close = () => {
      generation++;
      if (timer) clearInterval(timer);
      if (reverseTimer) clearInterval(reverseTimer);
      reverseTimer = undefined;
      timer = undefined;
      hls?.destroy(); hls = undefined;
      element.onerror = element.ontimeupdate = element.onended = element.oncanplay = null;
      element.pause(); element.removeAttribute("src"); element.load();
      const previous = session; session = undefined;
      if (previous) void scoped(`/tenants/${tenant}/playback-sessions/${previous.id}`, "DELETE").catch(() => {});
    };
    const fail = (message: string, adapt = false) => {
      if (!active) return;
      if (adapt && !retried) { retried = true; close(); void open(true, lastInstant); return; }
      close(); setLoading(false); latest.current.onState({phase: "error", message});
    };
    async function open(forceCompatibility = false, at = request!.at) {
      const run = ++generation, current = () => active && run === generation && context.valid();
      setLoading(true); latest.current.onState({phase: "loading", message: "Preparando gravação…"});
      try {
        const reverse = latest.current.speed < 0;
        let start = at;
        if (reverse) {
          // Load history containing the target, without jumping over archive gaps.
          const query = new URLSearchParams({start: new Date(at - 8000).toISOString(), end: new Date(at + 1).toISOString()});
          const availability = await scoped<{intervals: {start: string; end: string}[]}>(`/tenants/${tenant}/cameras/${camera}/recording-availability?${query}`);
          if (!current()) return;
          const interval = availability.intervals.find(part => recordingTimestamp(part.start) <= at && recordingTimestamp(part.end) > at);
          if (!interval) {
            close(); setLoading(false);
            latest.current.onState({phase: "gap", message: "Início do trecho gravado. Sem gravação anterior contínua."});
            return;
          }
          start = recordingSeekInstant(Math.max(at - 8000, recordingTimestamp(interval.start)));
        }
        const end = reverse ? at + 1 : start + 3600_000;
        const opened = await scoped<Session>(`/tenants/${tenant}/playback-sessions`, "POST", {
          cameraId: camera, start: new Date(start).toISOString(), end: new Date(end).toISOString(), codecs: archiveVideoCodecs(), forceCompatibility,
        });
        if (!current()) { void scoped(`/tenants/${tenant}/playback-sessions/${opened.id}`, "DELETE").catch(() => {}); return; }
        session = opened;
        let renewing = false;
        timer = setInterval(() => {
          if (renewing || !current()) return;
          renewing = true;
          void scoped(`/tenants/${tenant}/playback-sessions/${opened.id}`, "PUT", {}).catch(() => {
            if (current()) fail("Consulta encerrada. Verifique suas permissões ou tente novamente.");
          }).finally(() => { renewing = false; });
        }, 600);
        const url = new URL(opened.manifest, location.origin); url.searchParams.set("format", "json");
        const map = await scoped<Manifest>(url.pathname.replace(/^\/api\/v1/, "") + url.search);
        if (!current()) return;
        if (!map.mapping.length || !Hls.isSupported()) { fail("Não foi possível iniciar a gravação neste navegador."); return; }
        const targetPart = map.mapping.find(part => recordingTimestamp(part.start) <= at && recordingTimestamp(part.end) > at);
        if (reverse && !targetPart) { fail("Não foi possível localizar o instante para retroceder."); return; }
        const offset = reverse && targetPart ? targetPart.mediaStart + (at - recordingTimestamp(targetPart.start)) / 1000 : map.offset;
        const player = new Hls({maxBufferLength: 3, maxMaxBufferLength: 5, backBufferLength: 12, startPosition: offset, fragLoadingTimeOut: 30000,
          xhrSetup(xhr) { if (!current()) { xhr.abort(); return; } xhr.withCredentials = true; xhr.setRequestHeader("X-VMS-Context", context.headers["X-VMS-Context"]); },
        });
        hls = player;
        player.on(Hls.Events.ERROR, (_, data) => {
          if (current() && data.fatal) fail("Não foi possível reproduzir este trecho. Tente novamente.", data.type === Hls.ErrorTypes.MEDIA_ERROR);
        });
        const ready = () => {
          if (!current()) return;
          element.playbackRate = Math.abs(latest.current.speed);
          setLoading(false); latest.current.onState({phase: "ready", message: ""});
          if (latest.current.playing && latest.current.speed > 0) void element.play().catch(() => {
            if (current()) latest.current.onState({phase: "error", message: "Use Reproduzir para iniciar o vídeo."});
          });
        };
        element.oncanplay = ready;
        player.on(Hls.Events.MANIFEST_PARSED, () => {
          if (current() && latest.current.playing && latest.current.speed > 0) void element.play().catch(() => {});
        });
        element.onerror = () => { if (current()) fail("Falha ao decodificar a gravação.", [3, 4].includes(element.error?.code ?? 0)); };
        element.ontimeupdate = () => {
          if (!current()) return;
          const instant = originalRecordingInstant(map.mapping, element.currentTime);
          if (instant !== null) { lastInstant = instant; latest.current.onPosition(instant); }
        };
        element.onended = () => {
          if (!current() || latest.current.speed < 0) return;
          const last = recordingTimestamp(map.mapping.at(-1)!.end);
          latest.current.onPosition(last);
          close(); setLoading(false);
          latest.current.onEnded(last, map.nextStart ? recordingTimestamp(map.nextStart) : undefined, request!.serial);
        };
        let previousTick = performance.now();
        reverseTimer = setInterval(() => {
          const now = performance.now(), elapsed = Math.min((now - previousTick) / 1000, .25);
          previousTick = now;
          if (!current() || !latest.current.playing || latest.current.speed >= 0 || element.readyState < 2 || element.seeking) return;
          element.pause();
          const first = map.mapping[0], minimum = first.mediaStart;
          const target = element.currentTime + latest.current.speed * elapsed;
          if (target <= minimum) {
            const boundary = recordingTimestamp(first.start);
            // The instant before this session may belong to a preceding file,
            // a real gap, or an unauthorized range; availability decides.
            latest.current.onPosition(Math.ceil(boundary));
            close(); void open(false, Math.ceil(boundary) - 1);
          } else element.currentTime = target;
        }, 100);
        player.loadSource(opened.manifest); player.attachMedia(element);
      } catch (error) {
        if (!current()) return;
        if (error instanceof ApiError && ["recording_gap", "recording_unavailable"].includes(error.code)) {
          close(); setLoading(false);
          latest.current.onState({phase: "gap", message: "Sem gravação neste período.", next: typeof error.details.nextStart === "string" ? Date.parse(error.details.nextStart) : undefined});
        } else fail((error as Error).message);
      }
    }
    void open();
    return () => { active = false; close(); };
  }, [tenant, camera, request]);

  return <div className="live-video-stack recording-video">
    <video ref={video} muted playsInline aria-label="Vídeo gravado"/>
    {!request && <div className="video-state recording-video-prompt" role="status">Escolha um horário na linha do tempo</div>}
    {loading && <div className="video-state" role="status"><LoaderCircle className="animate-spin" aria-label="Carregando gravação…"/></div>}
  </div>;
}
