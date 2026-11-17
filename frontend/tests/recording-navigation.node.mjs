import assert from 'node:assert/strict';
import {test} from 'node:test';
import {recordingDate,recordingClock,recordingInstant,initialRecordingWindow,zoomRecordingWindow,recordingCalendar,recordingTicks,originalRecordingInstant,recordingSeekInstant} from '../src/lib/recording-navigation.ts';

test('site civil time is independent of the browser and rejects nonexistent times',()=>{
  const zone='America/New_York';
  assert.equal(recordingInstant('2026-03-08','02:30:00',zone),null);
  assert.equal(recordingInstant('2026-02-30','12:00:00',zone),null);
  assert.equal(recordingInstant('2026-11-01','01:30:00',zone),Date.parse('2026-11-01T05:30:00Z'));
  const instant=recordingInstant('2026-11-17','14:20:31','America/Sao_Paulo');
  assert.equal(instant,Date.parse('2026-11-17T17:20:31Z'));
  assert.equal(recordingDate(instant,'Asia/Tokyo'),'2026-11-18');
  assert.equal(recordingClock(instant,'America/Sao_Paulo'),'14:20:31');
  assert.deepEqual(initialRecordingWindow('America/Sao_Paulo',instant),{start:Date.parse('2026-11-17T03:00:00Z'),end:Date.parse('2026-11-18T03:00:00Z')});
});

test('zoom preserves the pointer instant and enforces one minute through two days',()=>{
  const view={start:1000000,end:87400000},anchor=.73;
  const next=zoomRecordingWindow(view,.5,anchor);
  assert.equal(next.start+(next.end-next.start)*anchor,view.start+(view.end-view.start)*anchor);
  const min=zoomRecordingWindow(view,1e-9),max=zoomRecordingWindow(view,1e9);
  assert.equal(min.end-min.start,60000);assert.equal(max.end-max.start,172800000);
  const ticks=recordingTicks(view,'America/Sao_Paulo',900);
  assert.ok(ticks.length>20&&ticks.some(t=>t.major)&&ticks.some(t=>!t.major));
  assert.equal(recordingCalendar('2026-11').length,42);
  assert.equal(recordingCalendar('2026-11')[0],'2026-11-01');
});

test('playback clock maps actual media time across files and clamps at real ends',()=>{
  const mapping=[{mediaStart:0,start:'2026-11-17T23:59:58Z',end:'2026-11-18T00:00:00Z'},{mediaStart:2,start:'2026-11-18T00:00:00Z',end:'2026-11-18T00:00:03Z'}];
  assert.equal(originalRecordingInstant(mapping,2.5),Date.parse('2026-11-18T00:00:00.500Z'));
  assert.equal(originalRecordingInstant(mapping,100),Date.parse('2026-11-18T00:00:03Z'));
  assert.equal(originalRecordingInstant([],0),null);
});

test('nanosecond archive timestamps do not display the previous second on an exact seek',()=>{
  const mapping=[{mediaStart:0,start:'2026-11-17T18:20:37.8222465Z',end:'2026-11-17T18:20:41.1822465Z'}];
  assert.equal(originalRecordingInstant(mapping,2.1777535),Date.parse('2026-11-17T18:20:40Z'));
});

test('next recording starts inside the returned interval instead of looping before its microsecond boundary',()=>{
  const target=recordingSeekInstant('2026-11-17T18:27:49.833074Z');
  assert.equal(new Date(target).toISOString(),'2026-11-17T18:27:49.834Z');
  assert.equal(recordingSeekInstant('2026-11-17T18:27:54.308448Z'),Date.parse('2026-11-17T18:27:54.309Z'));
  assert.equal(recordingSeekInstant('2026-11-17T18:27:54.308Z'),Date.parse('2026-11-17T18:27:54.308Z'));
  assert.equal(recordingSeekInstant('2026-11-17T15:27:49.833074-03:00'),Date.parse('2026-11-17T18:27:49.834Z'));
  assert.equal(recordingSeekInstant(Date.parse('2026-11-17T18:27:54Z')+.4),Date.parse('2026-11-17T18:27:54.001Z'));
});
