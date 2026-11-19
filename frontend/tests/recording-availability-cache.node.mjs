import {test} from 'node:test';
import assert from 'node:assert/strict';
import {RecordingAvailabilityCache} from '../src/lib/recording-availability-cache.ts';
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const start=Date.parse('2026-11-19T00:00:00Z');
const view={start,end:start+3600000};

test('pan and zoom retain blue coverage and reuse known day without HTTP',async()=>{
 let calls=0;
 const cache=new RecordingAvailabilityCache(async()=>{calls++;return {intervals:[{start,end:start+10000}]};},()=>{});
 cache.setView(view);cache.refresh();await tick();
 const shifted={start:start+500,end:start+1800000};cache.setView(shifted);
 assert.equal(cache.snapshot(shifted).intervals.length,1);
 cache.refresh();await tick();assert.equal(calls,1);cache.clear();
});
test('event refresh grows blue during navigation and authoritative removal shrinks it',async()=>{
 let finish;let calls=0;
 const cache=new RecordingAvailabilityCache(async()=>{if(++calls===1)return {intervals:[{start,end:start+10000}]};return new Promise(resolve=>finish=resolve);},()=>{});
 cache.setView(view);cache.refresh();await tick();
 cache.invalidate({start,end:start+20000});cache.refresh();
 const moved={start:start+1000,end:view.end+1000};cache.setView(moved);
 assert.equal(cache.snapshot(moved).intervals[0].end,start+10000);
 finish({intervals:[{start,end:start+20000}]});await tick();
 assert.equal(cache.snapshot(moved).intervals[0].end,start+20000);
 cache.invalidate();cache.refresh();finish({intervals:[]});await tick();
 assert.deepEqual(cache.snapshot(moved).intervals,[]);cache.clear();
});
test('event received during HTTP reconciles newest revision without losing notifications',async()=>{
 const pending=[];
 const cache=new RecordingAvailabilityCache(()=>new Promise(resolve=>pending.push(resolve)),()=>{});
 cache.setView(view);cache.refresh();cache.invalidate();cache.refresh();assert.equal(pending.length,1);
 pending[0]({intervals:[]});await tick();assert.equal(pending.length,2);
 pending[1]({intervals:[{start,end:start+2000}]});await tick();assert.equal(cache.snapshot(view).intervals.length,1);cache.clear();
});
test('obsolete page fetch is aborted and clearing authorization removes all known data',async()=>{
 let signal;
 const cache=new RecordingAvailabilityCache(async(_range,value)=>{signal=value;return {intervals:[{start,end:start+1000}]};},()=>{});
 cache.setView(view);cache.refresh();cache.setView({start:start+86400000,end:start+86401000});assert.equal(signal.aborted,true);
 await tick();cache.setView(view);cache.refresh();await tick();assert.equal(cache.snapshot(view).intervals.length,1);
 cache.clear();assert.equal(cache.snapshot(view).intervals.length,0);
});
test('temporary failure retains coverage; denied access removes it',async()=>{
 let error;
 const cache=new RecordingAvailabilityCache(async()=>{if(error)throw error;return {intervals:[{start,end:start+1000}]};},()=>{});
 cache.setView(view);cache.refresh();await tick();
 error=Object.assign(new Error('temporary'),{status:503});cache.invalidate();cache.refresh();await tick();
 assert.equal(cache.snapshot(view).intervals.length,1);assert.equal(cache.snapshot(view).error,'temporary');
 error=Object.assign(new Error('denied'),{status:403});cache.invalidate();cache.refresh();await tick();
 assert.equal(cache.snapshot(view).intervals.length,0);assert.equal(cache.snapshot(view).error,'denied');cache.clear();
});
test('archive event queries only changed seconds and replaces that slice without losing other coverage',async()=>{
 const ranges=[];let calls=0;
 const cache=new RecordingAvailabilityCache(async range=>{ranges.push(range);return {intervals:++calls===1?[{start,end:start+60000}]:[{start:start+20000,end:start+25000}]};},()=>{});
 cache.setView(view);cache.refresh();await tick();
 cache.invalidate({start:start+20000,end:start+30000});cache.refresh();await tick();
 assert.deepEqual(ranges[1],{start:start+20000,end:start+30000});
 assert.deepEqual(cache.snapshot(view).intervals,[{start,end:start+25000},{start:start+30000,end:start+60000}]);cache.clear();
});
