export type LivePosition = { cameraId: string; profile: string; fallback?: string[] };
export type LiveTile = LivePosition & { tileId: number };
export type LiveMosaic = { id: string; ownerId: string; name: string; visibility: string; layout?: string; positions: LivePosition[]; revision: number; editable: boolean };

export type MosaicLayout = {
  id: string;
  label: string;
  shortLabel?: string;
  columns: number;
  rows: number;
  areas: readonly string[];
};

function grid(columns: number, rows: number): MosaicLayout {
  return {
    id: `${columns}x${rows}`,
    label: `${columns}×${rows}`,
    columns,
    rows,
    areas: Array.from({ length: columns * rows }, (_, index) =>
      `${Math.floor(index / columns) + 1} / ${index % columns + 1}`),
  };
}

// Each repeated letter describes one rectangular tile. The first occurrence
// fixes its position in the camera list and in the saved composition.
function featured(id: string, label: string, shortLabel: string, template: string[]): MosaicLayout {
  const bounds = new Map<string, { top: number; left: number; bottom: number; right: number }>();
  template.forEach((line, row) => {
    [...line].forEach((name, column) => {
      const existing = bounds.get(name);
      if (existing) {
        existing.bottom = row + 2;
        existing.right = Math.max(existing.right, column + 2);
      } else {
        bounds.set(name, { top: row + 1, left: column + 1, bottom: row + 2, right: column + 2 });
      }
    });
  });
  return {
    id, label, shortLabel, rows: template.length, columns: template[0].length,
    areas: Array.from(bounds.values(), ({ top, left, bottom, right }) => `${top} / ${left} / ${bottom} / ${right}`),
  };
}

export const mosaicLayouts = [
  grid(1, 1),
  grid(2, 2),
  grid(3, 3),
  grid(4, 4),
  featured("corner-6", "6 posições · destaque no canto", "1+5", ["AAB", "AAC", "DEF"]),
  featured("corner-8", "8 posições · destaque no canto", "1+7", ["AAAB", "AAAC", "AAAD", "EFGH"]),
  featured("center-13", "13 posições · destaque central", "1+12", ["ABCD", "EFFG", "HFFI", "JKLM"]),
  featured("side-5", "5 posições · destaque à esquerda", "1+4", ["AAAB", "AAAC", "AAAD", "AAAE"]),
  featured("top-6", "6 posições · dois destaques no topo", "2+4", ["AABB", "AABB", "CDEF"]),
  grid(3, 2),
  grid(4, 3),
  grid(5, 4),
] as const;

export const extraMosaicLayouts = [
  grid(5, 5), grid(6, 6), grid(7, 7), grid(8, 8), grid(9, 9), grid(10, 10),
  grid(6, 5), grid(8, 6), grid(9, 7), grid(11, 8), grid(12, 9),
] as const;
export const allMosaicLayouts = [...mosaicLayouts, ...extraMosaicLayouts];

export function savedMosaicLayout(mosaic: Pick<LiveMosaic, "layout" | "positions">): MosaicLayout {
  const explicit = allMosaicLayouts.find(layout => layout.id === mosaic.layout);
  // Older views encode the square size only through the number of saved slots.
  const legacySize = Math.max(1, Math.min(4, Math.ceil(Math.sqrt(mosaic.positions.length))));
  return explicit ?? mosaicLayouts[legacySize - 1];
}

export function mosaicPositions(positions: readonly LivePosition[], count: number): LivePosition[] {
  return Array.from({ length: count }, (_, index) => ({
    cameraId: positions[index]?.cameraId ?? "",
    profile: positions[index]?.profile ?? "",
    fallback: [...(positions[index]?.fallback ?? [])],
  }));
}

export function restoreMosaic(mosaic: Pick<LiveMosaic, "layout" | "positions">, current: readonly LiveTile[], nextTileId: () => number): LiveTile[] {
  const remaining = [...current];
  return mosaicPositions(mosaic.positions, savedMosaicLayout(mosaic).areas.length).map(position => {
    // Match occurrences separately: a camera can occupy multiple slots. Reuse
    // its local tile so changing views can retain an already connected player.
    let index = remaining.findIndex(tile => tile.cameraId === position.cameraId && tile.profile === position.profile);
    if (index < 0) index = remaining.findIndex(tile => tile.cameraId === position.cameraId);
    const tileId = index < 0 ? nextTileId() : remaining.splice(index, 1)[0].tileId;
    return { ...position, tileId };
  });
}
