import assert from 'node:assert/strict';
import {test} from 'node:test';
import {setTargetAction, targetActionState} from '../src/lib/permission-grants.ts';

const tenant = {scope: 'tenant', resourceId: 'tenant-a'};
const site = {scope: 'site', resourceId: 'site-a'};
const camera = {scope: 'camera', resourceId: 'camera-a'};

test('editing one resource preserves other scopes, actions and unavailable resources', () => {
  const original = [
    {...tenant, action: 'live'}, {...site, action: 'playback'},
    {...camera, action: 'export'}, {scope: 'camera', resourceId: 'unavailable', action: 'live'},
  ];
  const added = setTargetAction(original, site, 'live', true);
  assert.deepEqual(added.slice(0, 4), original);
  const removed = setTargetAction(added, site, 'playback', false);
  assert.deepEqual(removed, [original[0], original[2], original[3], {...site, action: 'live'}]);
  assert.equal(original.length, 4);
});

test('does not combine an action from one scope with a resource from another', () => {
  const grants = [{...tenant, action: 'live'}, {...site, action: 'playback'}, {...camera, action: 'export'}];
  assert.equal(targetActionState(grants, {scope:'camera',resourceId:'camera-b'}, 'export', tenant.resourceId, site.resourceId).checked, false);
  assert.equal(targetActionState(grants, {scope:'camera',resourceId:'camera-b'}, 'playback', tenant.resourceId, 'site-b').checked, false);
  assert.equal(targetActionState(grants, camera, 'live', 'other-tenant', site.resourceId).checked, false);
});

test('shows tenant and site origins while preserving independent direct grants', () => {
  const grants = [{...tenant, action: 'live'}, {...site, action: 'live'}, {...camera, action: 'live'}];
  const state = targetActionState(grants, camera, 'live', tenant.resourceId, site.resourceId);
  assert.equal(state.direct, true);
  assert.deepEqual(state.parents, grants.slice(0, 2));
  const withoutDirect = setTargetAction(grants, camera, 'live', false);
  const inherited = targetActionState(withoutDirect, camera, 'live', tenant.resourceId, site.resourceId);
  assert.equal(inherited.direct, false);
  assert.equal(inherited.checked, true);
  assert.deepEqual(withoutDirect, grants.slice(0, 2));
});

test('turning off a broader scope does not erase narrower grants or other actions', () => {
  const grants = [{...tenant, action: 'live'}, {...site, action: 'live'}, {...camera, action: 'playback'}];
  const result = setTargetAction(grants, tenant, 'live', false);
  assert.deepEqual(result, grants.slice(1));
  assert.equal(targetActionState(result, camera, 'live', tenant.resourceId, site.resourceId).checked, true);
  assert.equal(targetActionState(result, {scope:'camera',resourceId:'other'}, 'live', tenant.resourceId, 'other-site').checked, false);
});

test('repeated check and uncheck cannot introduce duplicates or leave duplicate grants enabled', () => {
  const grant = {...site, action:'playback'};
  const once = setTargetAction([], site, 'playback', true);
  assert.deepEqual(setTargetAction(once, site, 'playback', true), [grant]);
  assert.deepEqual(setTargetAction([grant, grant], site, 'playback', false), []);
});

test('selecting export never silently grants playback', () => {
  const result = setTargetAction([], camera, 'export', true);
  assert.deepEqual(result, [{...camera, action: 'export'}]);
  assert.equal(targetActionState(result, camera, 'playback', tenant.resourceId, site.resourceId).checked, false);
});


test('a site grant covers every camera in that site, including cameras added later', () => {
  const grants = setTargetAction([], site, 'live', true);
  assert.deepEqual(grants, [{...site, action:'live'}]);
  for (const resourceId of ['camera-a', 'camera-b', 'camera-added-later']) {
    assert.equal(targetActionState(grants, {scope:'camera',resourceId}, 'live', tenant.resourceId, site.resourceId).checked, true);
    assert.equal(targetActionState(grants, {scope:'camera',resourceId}, 'live', tenant.resourceId, 'other-site').checked, false);
  }
});

test('site-only editing never promotes or deletes legacy camera grants', () => {
  const legacy = [{...camera, action:'export'}, {scope:'camera',resourceId:'unavailable-camera',action:'live'}];
  const enabled = setTargetAction(legacy, site, 'export', true);
  assert.deepEqual(enabled, [...legacy, {...site,action:'export'}]);
  const disabled = setTargetAction(enabled, site, 'export', false);
  assert.deepEqual(disabled, legacy);
});
