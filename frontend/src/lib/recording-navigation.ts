import type {RecordingInterval} from "./recording-timeline";

export const MIN_RECORDING_SPAN = 60_000;
export const MAX_RECORDING_SPAN = 48 * 3600_000;
export const DEFAULT_RECORDING_SPAN = 24 * 3600_000;
export function recordingTimestamp(value: string) {
  const fraction = value.match(/\.(\d+)(?:Z|[+-]\d{2}:?\d{2})$/)?.[1] ?? "";
  return Date.parse(value) + (fraction.length > 3 ? Number(`0.${fraction.slice(3)}`) : 0);
}
export function recordingSeekInstant(value: number | string) {
  // Enter the first representable browser millisecond inside the interval.
  // Truncating a server boundary can put the request BEFORE the first part,
  // causing recording_gap and repeatedly selecting that same next block.
  return Math.ceil(typeof value === "number" ? value : recordingTimestamp(value));
}
const formats = new Map<string, Intl.DateTimeFormat>();
function parts(at: number, zone: string) {
  let format = formats.get(zone);
  if (!format) {
    format = new Intl.DateTimeFormat("en-CA", {timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23"});
    formats.set(zone, format);
  }
  return Object.fromEntries(format.formatToParts(at).map(part => [part.type, part.value]));
}
export function recordingDate(at: number, zone: string) {
  const p = parts(at, zone);
  return `${p.year}-${p.month}-${p.day}`;
}
export function recordingClock(at: number, zone: string) {
  const p = parts(at, zone);
  return `${p.hour}:${p.minute}:${p.second}`;
}
export function recordingLabel(at: number, zone: string) {
  return new Intl.DateTimeFormat("pt-BR", {timeZone: zone, dateStyle: "short", timeStyle: "medium"}).format(at);
}
export function shiftRecordingDate(date: string, days: number) {
  const value = new Date(`${date}T12:00:00Z`);
  value.setUTCDate(value.getUTCDate() + days);
  return value.toISOString().slice(0, 10);
}
// Reject nonexistent civil times (DST forward) and choose the first occurrence
// of an ambiguous time (DST backward). Never use the browser's local timezone.
export function recordingInstant(date: string, clock: string, zone: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !/^\d{2}:\d{2}(:\d{2})?$/.test(clock)) return null;
  const time = clock.length === 5 ? `${clock}:00` : clock;
  const naive = Date.parse(`${date}T${time}Z`);
  if (!Number.isFinite(naive) || new Date(naive).toISOString().slice(0, 19) !== `${date}T${time}`) return null;
  const matches = new Set<number>();
  for (let hours = -36; hours <= 36; hours += 12) {
    const sample = naive + hours * 3600_000, p = parts(sample, zone);
    const offset = Date.parse(`${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}:${p.second}Z`) - sample;
    const candidate = naive - offset;
    if (recordingDate(candidate, zone) === date && recordingClock(candidate, zone) === time) matches.add(candidate);
  }
  return matches.size ? Math.min(...matches) : null;
}
export function initialRecordingWindow(zone: string, now = Date.now()): RecordingInterval {
  const start = recordingInstant(recordingDate(now, zone), "00:00:00", zone) ?? now - DEFAULT_RECORDING_SPAN / 2;
  return {start, end: start + DEFAULT_RECORDING_SPAN};
}
export function zoomRecordingWindow(view: RecordingInterval, factor: number, anchor = .5): RecordingInterval {
  const span = view.end - view.start, next = Math.max(MIN_RECORDING_SPAN, Math.min(MAX_RECORDING_SPAN, span * factor));
  const fraction = Math.max(0, Math.min(1, anchor));
  const start = view.start + (span - next) * fraction;
  return {start, end: start + next};
}
export function recordingCalendar(month: string) {
  const first = `${month}-01`, weekday = new Date(`${first}T12:00:00Z`).getUTCDay();
  return Array.from({length: 42}, (_, i) => shiftRecordingDate(first, i - weekday));
}
export function shiftRecordingMonth(month: string, delta: number) {
  const date = new Date(`${month}-01T12:00:00Z`);
  date.setUTCMonth(date.getUTCMonth() + delta);
  return date.toISOString().slice(0, 7);
}
export function recordingTicks(view: RecordingInterval, zone: string, width: number) {
  const choices = [1000, 5000, 10000, 15000, 30000, 60000, 300000, 900000, 1800000, 3600000, 10800000, 21600000, 43200000, 86400000];
  const step = choices.find(value => value / (view.end - view.start) * width >= 76) ?? choices.at(-1)!;
  const divisions = [1000, 1000, 2000, 5000, 5000, 10000, 60000, 300000, 300000, 900000, 1800000, 3600000, 7200000, 14400000];
  const minor = divisions[choices.indexOf(step)];
  const p = parts(view.start, zone);
  const offset = Date.parse(`${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}:${p.second}Z`) - Math.floor(view.start / 1000) * 1000;
  const first = Math.ceil((view.start + offset) / minor) * minor - offset;
  const ticks: {at: number; major: boolean; label: string}[] = [];
  for (let at = first; at <= view.end; at += minor) {
    const major = Math.abs((at + offset) / step - Math.round((at + offset) / step)) < .001;
    const clock = recordingClock(at, zone);
    ticks.push({at, major, label: major ? (step < 60000 ? clock : clock.slice(0, 5)) : ""});
  }
  return ticks;
}
export type RecordingMapping = {mediaStart: number; start: string; end: string};
export function originalRecordingInstant(mapping: RecordingMapping[], position: number): number | null {
  let match: RecordingMapping | undefined;
  for (const part of mapping) { if (position >= part.mediaStart) match = part; else break; }
  // Keep server submillisecond precision until the final browser clock rounding.
  return match ? Math.min(recordingTimestamp(match.end), Math.round(recordingTimestamp(match.start) + Math.max(0, position - match.mediaStart) * 1000)) : null;
}
