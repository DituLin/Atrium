import { expect, it } from 'vitest';
import { trendSegments } from './trendCurve';

it('keeps a smooth curve between neighboring samples on uneven time intervals', () => {
  for (const ys of [[0, 8, 9, 1], [10, 10, 2, 2], [1, 2, 20, 21]]) {
    const points = ys.map((y, i) => ({ x: [0, 1, 7, 8][i]!, y }));
    const [segment] = trendSegments(points, 30);
    const numbers = segment!.line.match(/-?\d+(?:\.\d+)?(?:e[+-]?\d+)?/g)!.map(Number);
    expect(numbers.slice(0, 2)).toEqual([0, ys[0]]);
    for (let i = 0; i < points.length - 1; i++) {
      const [cx1, cy1, cx2, cy2, x, y] = numbers.slice(2 + i * 6, 8 + i * 6);
      expect([x, y]).toEqual([points[i + 1]!.x, points[i + 1]!.y]);
      expect(cx1).toBeGreaterThanOrEqual(points[i]!.x);
      expect(cx2).toBeLessThanOrEqual(x!);
      for (let step = 0; step <= 100; step++) {
        const t = step / 100, u = 1 - t;
        const value = u ** 3 * points[i]!.y + 3 * u ** 2 * t * cy1! + 3 * u * t ** 2 * cy2! + t ** 3 * y!;
        expect(value).toBeGreaterThanOrEqual(Math.min(ys[i]!, ys[i + 1]!) - 1e-9);
        expect(value).toBeLessThanOrEqual(Math.max(ys[i]!, ys[i + 1]!) + 1e-9);
      }
    }
  }
});
it('splits duplicate time samples and invalid coordinates without fabricating connections', () => {
  const segments = trendSegments([{ x: 0, y: 1 }, { x: 0, y: 2 }, { x: 1, y: 3 }, null, { x: 2, y: NaN }, { x: 3, y: 4 }], 10);
  expect(segments).toHaveLength(3);
  expect(segments.map(s => s.points.length)).toEqual([1, 2, 1]);
  expect(segments[0]!.fill).toBe('');
  expect(segments[1]!.fill).toMatch(/Z$/);
  expect(JSON.stringify(segments)).not.toMatch(/NaN|Infinity/);
});
it('handles no points and a single point without an area', () => {
  expect(trendSegments([], 10)).toEqual([]);
  expect(trendSegments([{ x: 4, y: 5 }], 10)).toEqual([{ points: [{ x: 4, y: 5 }], line: 'M 4 5', fill: '' }]);
});
