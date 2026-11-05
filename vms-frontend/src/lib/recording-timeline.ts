export type RecordingInterval = { start: number; end: number };
export type RecordingSegment = { start: string; end: string };

export function localDateValue(date = new Date()) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

export function recordingDay(value: string): RecordingInterval | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
  const start = new Date(`${value}T00:00:00`);
  if (!Number.isFinite(start.getTime()) || localDateValue(start) !== value) return null;
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return { start: start.getTime(), end: end.getTime() };
}

// Keep each request below the API's 24-hour and segment-count limits. Local
// calendar days can also contain 23 or 25 hours across daylight-saving changes.
export function recordingWindows(day: RecordingInterval): RecordingInterval[] {
  const windows: RecordingInterval[] = [];
  for (let start = day.start; start < day.end; start += 6 * 3600_000) {
    windows.push({ start, end: Math.min(day.end, start + 6 * 3600_000) });
  }
  return windows;
}

export function recordingIntervals(segments: RecordingSegment[], day: RecordingInterval): RecordingInterval[] {
  const clipped = segments.map(segment => ({
    start: Math.max(day.start, Date.parse(segment.start)),
    end: Math.min(day.end, Date.parse(segment.end)),
  })).filter(segment => Number.isFinite(segment.start) && Number.isFinite(segment.end) && segment.end > segment.start)
    .sort((a, b) => a.start - b.start);
  const intervals: RecordingInterval[] = [];
  for (const segment of clipped) {
    const previous = intervals.at(-1);
    if (previous && segment.start <= previous.end) previous.end = Math.max(previous.end, segment.end);
    else intervals.push({ ...segment });
  }
  return intervals;
}
