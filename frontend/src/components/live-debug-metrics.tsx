"use client";

import {useEffect, useState, type RefObject} from "react";
import {ArrowDownToLine, Cpu, Gauge, ScanLine} from "lucide-react";

type Metrics = {fps?: number; resolution?: string; codec?: string; bitrate?: string};
const bitrateFormat = new Intl.NumberFormat("pt-BR", {minimumFractionDigits: 2, maximumFractionDigits: 2});

export function LiveDebugMetrics({peer, video}: {
  peer?: RefObject<RTCPeerConnection | undefined>;
  video?: RefObject<HTMLVideoElement | null>;
}) {
  const [metrics, setMetrics] = useState<Metrics>({});

  useEffect(() => {
    const connection = peer?.current;
    const element = video?.current;
    if (!connection || !element) return;
    let active = true;
    let reading = false;
    let previous: RTCInboundRtpStreamStats | undefined;

    async function sample(peer: RTCPeerConnection, element: HTMLVideoElement) {
      if (reading || document.hidden) return;
      reading = true;
      let fps: number | undefined;
      let codec: string | undefined;
      let bitrate: string | undefined;
      let width = element.videoWidth;
      let height = element.videoHeight;

      try {
        const stats = await peer.getStats();
        const inbound = [...stats.values()].find(report =>
          report.type === "inbound-rtp" && (report.kind === "video" || report.mediaType === "video")
        ) as RTCInboundRtpStreamStats | undefined;
        if (inbound) {
          fps = inbound.framesPerSecond;
          if (previous && inbound.id === previous.id && inbound.timestamp > previous.timestamp) {
            const elapsed = inbound.timestamp - previous.timestamp;
            if (fps === undefined && inbound.framesDecoded !== undefined && previous.framesDecoded !== undefined) {
              const frames = inbound.framesDecoded - previous.framesDecoded;
              if (frames >= 0) fps = frames * 1000 / elapsed;
            }
            if (inbound.bytesReceived !== undefined && previous.bytesReceived !== undefined) {
              const bytes = inbound.bytesReceived - previous.bytesReceived;
              if (bytes >= 0) bitrate = bitrateFormat.format(bytes * 8 / (elapsed * 1000));
            }
          }
          width ||= inbound.frameWidth ?? 0;
          height ||= inbound.frameHeight ?? 0;
          const mimeType: string | undefined = inbound.codecId ? stats.get(inbound.codecId)?.mimeType : undefined;
          codec = mimeType?.replace(/^video\//i, "").toUpperCase().replace(/^H(26[45])$/, "H.$1");
        }
        previous = inbound;
      } catch {
        previous = undefined;
      } finally {
        reading = false;
      }

      if (!active) return;
      const resolution = width && height ? `${width} × ${height}` : undefined;
      const roundedFps = fps === undefined ? undefined : Math.round(fps);
      setMetrics(current => current.fps === roundedFps && current.resolution === resolution &&
        current.codec === codec && current.bitrate === bitrate
        ? current
        : {fps: roundedFps, resolution, codec, bitrate});
    }

    void sample(connection, element);
    const timer = setInterval(() => void sample(connection, element), 1000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [peer, video]);

  return (
    <div className="live-debug-metrics" role="group" aria-label="Métricas da transmissão ao vivo">
      <span>
        <Gauge aria-hidden="true"/>
        {metrics.fps ?? "—"} FPS
      </span>
      <span>
        <ScanLine aria-hidden="true"/>
        <span className="sr-only">Resolução </span>
        {metrics.resolution ?? "—"}
      </span>
      <span>
        <Cpu aria-hidden="true"/>
        <span className="sr-only">Codec </span>
        {metrics.codec ?? "—"}
      </span>
      <span>
        <ArrowDownToLine aria-hidden="true"/>
        <span className="sr-only">Bitrate recebido </span>
        {metrics.bitrate ?? "—"} Mbps
      </span>
    </div>
  );
}
