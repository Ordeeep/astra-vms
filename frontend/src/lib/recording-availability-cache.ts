import type {RecordingInterval} from "./recording-timeline";

const DAY = 86_400_000;
type Page = {intervals?: RecordingInterval[]; updated: number; version: number; dirty: boolean; dirtyRange?: RecordingInterval; requestedRange?: RecordingInterval; error: string; sourceDelaySeconds?: number; controller?: AbortController};
export type AvailabilitySnapshot = {intervals: RecordingInterval[]; loading: boolean; updating: boolean; error: string; sourceDelaySeconds: number};
type Loaded = {intervals: RecordingInterval[]; sourceDelaySeconds?: number};
const merge = (intervals: RecordingInterval[]) => intervals.sort((a, b) => a.start - b.start).reduce<RecordingInterval[]>((result, interval) => {
  const last = result.at(-1);
  if (last && interval.start <= last.end + .001) last.end = Math.max(last.end, interval.end);
  else result.push({...interval});
  return result;
}, []);

// One cache belongs to one authorized camera panel. Viewport changes never
// discard known coverage; each day is authoritative, including empty periods.
export class RecordingAvailabilityCache {
  private pages = new Map<number, Page>();
  private view: RecordingInterval | null = null;
  private load: (range: RecordingInterval, signal: AbortSignal) => Promise<Loaded>;
  private changed: () => void;
  constructor(load: (range: RecordingInterval, signal: AbortSignal) => Promise<Loaded>, changed: () => void) { this.load = load; this.changed = changed; }

  private keys(view: RecordingInterval) {
    const keys: number[] = [];
    for (let start = Math.floor(view.start / DAY) * DAY; start < view.end; start += DAY) keys.push(start);
    return keys;
  }

  setView(view: RecordingInterval) {
    this.view = view;
    const wanted = new Set(this.keys(view));
    for (const [key, page] of this.pages) if (!wanted.has(key) && page.controller) {
      page.dirtyRange = page.dirtyRange ?? page.requestedRange; page.requestedRange = undefined;
      page.version++; page.controller.abort(); page.controller = undefined;
    }
    while (this.pages.size > 16) {
      const candidate = [...this.pages.keys()].find(key => !wanted.has(key));
      if (candidate === undefined) break;
      this.pages.delete(candidate);
    }
  }

  invalidate(range?: RecordingInterval) {
    for (const [key, page] of this.pages) if (!range || range.end > key && range.start < key + DAY) {
      const affected = !range || !page.intervals ? {start: key, end: key + DAY} : {start: Math.max(key, range.start), end: Math.min(key + DAY, range.end)};
      const previous = page.dirtyRange ?? page.requestedRange;
      page.dirtyRange = previous ? {start: Math.min(previous.start, affected.start), end: Math.max(previous.end, affected.end)} : affected;
      page.version++; page.dirty = true;
    }
  }

  refresh() {
    if (!this.view) return;
    for (const key of this.keys(this.view)) {
      let page = this.pages.get(key);
      if (!page) { page = {updated: 0, version: 0, dirty: true, error: ""}; this.pages.set(key, page); }
      // Touch the LRU entry without tying its identity to the visible window.
      this.pages.delete(key); this.pages.set(key, page);
      if (page.controller || !page.dirty && Date.now() - page.updated < 60_000) continue;
      const target = page, controller = new AbortController(), version = page.version;
      const range = page.intervals && page.dirtyRange ? page.dirtyRange : {start: key, end: key + DAY};
      target.requestedRange = range; target.dirtyRange = undefined;
      target.controller = controller;
      void this.load(range, controller.signal).then(value => {
        if (controller.signal.aborted) return;
        const preserved = (target.intervals ?? []).flatMap(interval => [
          {start: interval.start, end: Math.min(interval.end, range.start)},
          {start: Math.max(interval.start, range.end), end: interval.end},
        ]).filter(interval => interval.end > interval.start);
        target.intervals = merge(preserved.concat(value.intervals.map(interval => ({start: Math.max(range.start, interval.start), end: Math.min(range.end, interval.end)})).filter(interval => interval.end > interval.start)));
        // One HTTP per page: this is newer than its previous snapshot, even
        // when another event arrived meanwhile. Show progress and reconcile
        // again, rather than starving a busy camera on a slower connection.
        target.dirty = target.version !== version; target.updated = Date.now(); target.error = "";
        target.sourceDelaySeconds = value.sourceDelaySeconds ?? target.sourceDelaySeconds ?? 0;
      }).catch(error => {
        if (controller.signal.aborted) return;
        if ([401, 403, 404].includes(error.status)) this.clear();
        else if (target.version !== version) return;
        target.error = error.message; target.dirty = true;
        target.dirtyRange = range;
        // Preserve the error even if authorization cleared the page map.
        if (!this.pages.has(key)) this.pages.set(key, target);
      }).finally(() => {
        if (target.controller !== controller) return;
        target.controller = undefined; target.requestedRange = undefined; this.changed();
        // Events arriving during HTTP must not be lost or install old data.
        if (!controller.signal.aborted && target.version !== version) this.refresh();
      });
    }
    this.changed();
  }

  snapshot(view: RecordingInterval | null): AvailabilitySnapshot {
    const result: AvailabilitySnapshot = {intervals: [], loading: false, updating: false, error: "", sourceDelaySeconds: 0};
    if (!view) return {...result, loading: true};
    for (const key of this.keys(view)) {
      const page = this.pages.get(key);
      if (!page?.intervals && !page?.error) result.loading = true;
      if (page?.dirty || page?.controller) result.updating = true;
      if (page?.error) result.error = page.error;
      result.sourceDelaySeconds = Math.max(result.sourceDelaySeconds, page?.sourceDelaySeconds ?? 0);
      for (const interval of page?.intervals ?? []) if (interval.end > view.start && interval.start < view.end) result.intervals.push(interval);
    }
    result.intervals = merge(result.intervals);
    return result;
  }

  clear() {
    for (const page of this.pages.values()) { page.controller?.abort(); page.version++; page.intervals = undefined; }
    this.pages.clear();
  }
}
