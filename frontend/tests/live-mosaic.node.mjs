import assert from 'node:assert/strict';
import { test } from 'node:test';
import { allMosaicLayouts, mosaicPositions, restoreMosaic, savedMosaicLayout } from '../src/lib/live-mosaic.ts';

test('preserves every visible slot and layout, without persisting local tile IDs or hidden cameras', () => {
  for (const size of [1, 2, 3, 4]) {
    const positions = Array.from({ length: 16 }, (_, tileId) => ({
      tileId, cameraId: tileId === 0 ? 'entrance' : '', profile: tileId === 0 ? 'main' : '', fallback: [],
    }));
    const saved = mosaicPositions(positions, size * size);
    assert.equal(saved.length, size * size);
    assert.equal(savedMosaicLayout({ positions: saved }).id, `${size}x${size}`);
    assert.equal(saved[0].cameraId, 'entrance');
    assert.ok(saved.every(position => !('tileId' in position)));
    saved[0].fallback.push('sub2');
    assert.deepEqual(positions[0].fallback, []);
  }
});

test('restores sparse and legacy compositions without shifting camera positions', () => {
  let serial = 10;
  const restored = restoreMosaic({ positions: [
    { cameraId: '', profile: '' },
    { cameraId: 'restricted-camera', profile: 'sub2', fallback: ['main'] },
  ] }, [], () => serial++);
  assert.equal(restored.length, 4);
  assert.equal(restored[0].cameraId, '');
  assert.deepEqual(restored[1], { tileId: 11, cameraId: 'restricted-camera', profile: 'sub2', fallback: ['main'] });
  assert.equal(restored[3].cameraId, '');
  assert.equal(savedMosaicLayout({ positions: [] }).id, '1x1');
});

test('keeps distinct player identities for repeated cameras and reuses players across view changes', () => {
  const current = [
    { tileId: 7, cameraId: 'a', profile: 'main' },
    { tileId: 2, cameraId: 'a', profile: 'sub2' },
    { tileId: 9, cameraId: 'b', profile: '' },
    { tileId: 3, cameraId: '', profile: '' },
  ];
  const before = structuredClone(current);
  let serial = 10;
  const saved = [
    { cameraId: 'b', profile: '' },
    { cameraId: 'a', profile: 'sub2' },
    { cameraId: 'a', profile: 'main' },
    { cameraId: 'c', profile: '' },
  ];
  const restored = restoreMosaic({ positions: saved }, current, () => serial++);
  assert.deepEqual(restored.map(tile => tile.tileId), [9, 2, 7, 10]);
  assert.deepEqual(current, before);
  assert.equal(new Set(restored.map(tile => tile.tileId)).size, 4);
});

test('profile changes retain the camera tile and saving after restoring does not inherit old hidden slots', () => {
  const current = [{ tileId: 3, cameraId: 'a', profile: 'main' }, { tileId: 0, cameraId: 'b', profile: '' }];
  const restored = restoreMosaic({ positions: [{ cameraId: 'a', profile: 'sub2', fallback: ['main'] }] }, current, () => 20);
  assert.equal(restored[0].tileId, 3);
  assert.deepEqual(mosaicPositions(restored, 1), [{ cameraId: 'a', profile: 'sub2', fallback: ['main'] }]);
  assert.equal(restored.length, 1);
});

test('covers every grid cell exactly once and restores the explicit layout even when slot counts coincide', () => {
  const expectedCounts = [1, 4, 9, 16, 6, 8, 13, 5, 6, 6, 12, 20, 25, 36, 49, 64, 81, 100, 30, 48, 63, 88, 108];
  assert.deepEqual(allMosaicLayouts.map(layout => layout.areas.length), expectedCounts);
  for (const layout of allMosaicLayouts) {
    const covered = new Set();
    for (const area of layout.areas) {
      const [top, left, bottom = top + 1, right = left + 1] = area.split(' / ').map(Number);
      assert.ok(top >= 1 && left >= 1 && bottom <= layout.rows + 1 && right <= layout.columns + 1, layout.id);
      for (let row = top; row < bottom; row++) {
        for (let column = left; column < right; column++) {
          const cell = `${row},${column}`;
          assert.ok(!covered.has(cell), `${layout.id}: overlapping cell ${cell}`);
          covered.add(cell);
        }
      }
    }
    assert.equal(covered.size, layout.rows * layout.columns, layout.id);
    const mosaic = { layout: layout.id, positions: [{ cameraId: 'a', profile: '' }] };
    let serial = 0;
    assert.equal(savedMosaicLayout(mosaic), layout);
    assert.equal(restoreMosaic(mosaic, [], () => serial++).length, layout.areas.length);
  }
});
