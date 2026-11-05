export type VideoCodec = "h264" | "h265";

/** Capabilities are hints; the player retries compatible delivery if decoding fails. */
export function liveVideoCodecs(): VideoCodec[] {
  const codecs: VideoCodec[] = ["h264"];
  try {
    if (typeof RTCRtpReceiver !== "undefined" && RTCRtpReceiver.getCapabilities("video")?.codecs.some(codec => /video\/(h265|hevc)/i.test(codec.mimeType))) codecs.push("h265");
  } catch { /* Unknown capabilities use the widely supported delivery codec. */ }
  return codecs;
}

export function archiveVideoCodecs(): VideoCodec[] {
  const codecs: VideoCodec[] = ["h264"];
  try {
    if (typeof MediaSource !== "undefined" && MediaSource.isTypeSupported('video/mp4; codecs="hvc1.1.6.L93.B0"')) codecs.push("h265");
  } catch { /* Capability checks may be unavailable in restricted contexts. */ }
  return codecs;
}

export function compatibleReceiverCodecs(compatible: boolean): RTCRtpCodec[] {
  try {
    return RTCRtpReceiver.getCapabilities("video")?.codecs.filter(codec => /video\/h264/i.test(codec.mimeType) || (!compatible && /video\/(h265|hevc)/i.test(codec.mimeType)) || /video\/(rtx|red|ulpfec)/i.test(codec.mimeType)) ?? [];
  } catch { return []; }
}
