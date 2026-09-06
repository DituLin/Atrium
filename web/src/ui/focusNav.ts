/**
 * Pure roving-focus arithmetic, kept out of the React layer so the navigation
 * rules can be tested without a DOM.
 *
 * The grid is row-major with `columns` per row; the last row may be short.
 * Focus never wraps around an edge — on a TV, a wrap looks like a glitch.
 */

import type { RemoteKey } from './keys';

export interface GridShape {
  count: number;
  columns: number;
}

export function nextFocusIndex(
  current: number,
  direction: RemoteKey,
  shape: GridShape,
): number {
  const { count } = shape;
  if (count <= 0) return -1;
  const columns = Math.max(1, shape.columns);
  const index = Math.min(Math.max(current, 0), count - 1);
  const row = Math.floor(index / columns);
  const column = index % columns;

  switch (direction) {
    case 'left':
      return column === 0 ? index : index - 1;
    case 'right':
      return column === columns - 1 || index + 1 >= count ? index : index + 1;
    case 'up':
      return row === 0 ? index : index - columns;
    case 'down': {
      const candidate = index + columns;
      return candidate >= count ? index : candidate;
    }
    default:
      return index;
  }
}
