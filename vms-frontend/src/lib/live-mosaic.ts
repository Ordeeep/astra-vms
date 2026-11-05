export type LivePosition = { cameraId: string; profile: string; fallback?: string[] };
export type LiveTile = LivePosition & { tileId: number };
export type LiveMosaic = { id: string; ownerId: string; name: string; visibility: string; positions: LivePosition[]; revision: number; editable: boolean };

// Saving every visible slot, including empty ones, preserves the square layout
// using the existing mosaic API. Older compositions may have fewer slots.
export function mosaicSize(positions: readonly LivePosition[]): number {
  return Math.max(1, Math.min(4, Math.ceil(Math.sqrt(positions.length))));
}

export function mosaicPositions(positions: readonly LivePosition[], size: number): LivePosition[] {
  return Array.from({ length: size * size }, (_, index) => ({
    cameraId: positions[index]?.cameraId ?? "",
    profile: positions[index]?.profile ?? "",
    fallback: [...(positions[index]?.fallback ?? [])],
  }));
}

export function restoreMosaic(saved: readonly LivePosition[], current: readonly LiveTile[], nextTileId: () => number): LiveTile[] {
  const remaining = [...current];
  return mosaicPositions(saved, mosaicSize(saved)).map(position => {
    // Match occurrences separately: a camera can occupy multiple slots. Reuse
    // its local tile so changing views can retain an already connected player.
    let index = remaining.findIndex(tile => tile.cameraId === position.cameraId && tile.profile === position.profile);
    if (index < 0) index = remaining.findIndex(tile => tile.cameraId === position.cameraId);
    const tileId = index < 0 ? nextTileId() : remaining.splice(index, 1)[0].tileId;
    return { ...position, tileId };
  });
}
