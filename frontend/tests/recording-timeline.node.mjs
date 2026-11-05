import assert from 'node:assert/strict';
import { test } from 'node:test';
import { recordingDay, recordingIntervals, recordingWindows } from '../src/lib/recording-timeline.ts';

test('clips to the selected day, combines adjacent/overlapping segments, and preserves real gaps', () => {
  const day = { start: 1000, end: 10000 };
  const segment = (start, end) => ({ start: new Date(start).toISOString(), end: new Date(end).toISOString() });
  assert.deepEqual(recordingIntervals([
    segment(7000, 12000), segment(2000, 4000), segment(0, 2000),
    segment(3000, 5000), segment(6000, 6000), { start: 'invalid', end: 'invalid' },
  ], day), [{ start: 1000, end: 5000 }, { start: 7000, end: 10000 }]);
});

test('handles local calendar days and splits 25-hour days into valid API requests', () => {
  const previous = process.env.TZ;
  process.env.TZ = 'America/New_York';
  try {
    const autumn = recordingDay('2026-11-01');
    const spring = recordingDay('2026-03-08');
    assert.equal(autumn.end - autumn.start, 25 * 3600_000);
    assert.equal(spring.end - spring.start, 23 * 3600_000);
    const windows = recordingWindows(autumn);
    assert.equal(windows[0].start, autumn.start);
    assert.equal(windows.at(-1).end, autumn.end);
    assert.ok(windows.every((window, i) => window.end - window.start <= 6 * 3600_000 && (!i || window.start === windows[i - 1].end)));
    assert.equal(recordingDay('2026-02-30'), null);
    assert.equal(recordingDay(''), null);
  } finally {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  }
});
