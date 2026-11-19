"use client";

import {useEffect, useMemo, useState} from "react";
import {api} from "@/lib/api";
import type {Notification} from "@/lib/events";
import type {RecordingInterval, RecordingSegment} from "@/lib/recording-timeline";
import {recordingTimestamp} from "@/lib/recording-navigation";
import {RecordingAvailabilityCache} from "@/lib/recording-availability-cache";

export function useRecordingAvailability(base: string, cameraId: string, view: RecordingInterval | null) {
  const [, redraw] = useState(0), [disconnected, setDisconnected] = useState(false);
  const cache = useMemo(() => new RecordingAvailabilityCache(async (range, signal) => {
    const query = new URLSearchParams({start: new Date(range.start).toISOString(), end: new Date(range.end).toISOString()});
    const value = await api<{intervals: RecordingSegment[]; sourceDelaySeconds?: number}>(`${base}?${query}`, "GET", undefined, true, AbortSignal.any([signal, AbortSignal.timeout(5000)]));
    return {intervals: value.intervals.map(interval => ({start: recordingTimestamp(interval.start), end: recordingTimestamp(interval.end)})), sourceDelaySeconds: value.sourceDelaySeconds};
  }, () => redraw(value => value + 1)), [base]);

  useEffect(() => {
    if (!view) return;
    cache.setView(view);
    const timer = setTimeout(() => cache.refresh(), 140);
    return () => clearTimeout(timer);
  }, [cache, view]);

  useEffect(() => {
    let scheduled: ReturnType<typeof setTimeout> | undefined;
    const schedule = () => { scheduled ??= setTimeout(() => { scheduled = undefined; cache.refresh(); }, 120); };
    const receive = (event: Event) => {
      const value = (event as CustomEvent<Notification>).detail;
      if (value.type === "Disconnected") { setDisconnected(true); return; }
      if (["Connected", "ResyncRequired"].includes(value.type)) setDisconnected(false);
      if (["AuthorizationChanged", "ResyncRequired"].includes(value.type)) { cache.clear(); redraw(value => value + 1); }
      if (["AuthorizationChanged", "ResyncRequired", "Connected"].includes(value.type)) { cache.invalidate(); schedule(); return; }
      if (!["ArchiveChanged", "RecordingChanged", "InventoryChanged"].includes(value.type)) return;
      if (value.cameraId && value.cameraId !== cameraId) return;
      const start = Date.parse(String(value.payload?.start)), end = Date.parse(String(value.payload?.end));
      cache.invalidate(Number.isFinite(start) && Number.isFinite(end) ? {start, end} : undefined);
      schedule();
    };
    window.addEventListener("vms:change", receive);
    // Reconcile missed notifications and retention changes without a busy poll.
    const fallback = setInterval(() => { cache.invalidate(); cache.refresh(); }, 60_000);
    return () => { window.removeEventListener("vms:change", receive); clearInterval(fallback); clearTimeout(scheduled); cache.clear(); };
  }, [cache, cameraId]);
  return {...cache.snapshot(view), disconnected};
}
