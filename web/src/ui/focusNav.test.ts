import { describe, expect, it } from 'vitest';

import { nextFocusIndex } from './focusNav';

const grid = { count: 7, columns: 3 };

describe('roving focus arithmetic', () => {
  it('moves within a row and never wraps at the edges', () => {
    expect(nextFocusIndex(0, 'right', grid)).toBe(1);
    expect(nextFocusIndex(2, 'right', grid)).toBe(2);
    expect(nextFocusIndex(0, 'left', grid)).toBe(0);
    expect(nextFocusIndex(1, 'left', grid)).toBe(0);
  });

  it('moves between rows and clamps on a short last row', () => {
    expect(nextFocusIndex(0, 'down', grid)).toBe(3);
    expect(nextFocusIndex(4, 'down', grid)).toBe(4);
    expect(nextFocusIndex(6, 'down', grid)).toBe(6);
    expect(nextFocusIndex(3, 'up', grid)).toBe(0);
    expect(nextFocusIndex(1, 'up', grid)).toBe(1);
  });

  it('handles an empty or single-column list', () => {
    expect(nextFocusIndex(0, 'right', { count: 0, columns: 3 })).toBe(-1);
    expect(nextFocusIndex(0, 'down', { count: 3, columns: 1 })).toBe(1);
    expect(nextFocusIndex(2, 'down', { count: 3, columns: 1 })).toBe(2);
  });
});
