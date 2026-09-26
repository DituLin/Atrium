import { describe, expect, it } from 'vitest';

import type { PhotoItem } from '../../types/api';
import { gridSections, monthLabel, moveInSections } from './libraryLayout';
import type { GridSection } from './libraryLayout';

const options = { timezone: 'Asia/Shanghai', utcOffsetSeconds: 28800 };
const item = (id: string, captured_at: string | null, confidence: PhotoItem['captured_confidence'] = 'exact'): PhotoItem => ({
  id, source_id: 's', captured_at, captured_confidence: captured_at ? confidence : 'unknown', first_seen_at: '2026-09-01T00:00:00Z',
  is_baseline: false, width: 1, height: 1, preview: { status: 'ready', width: 1, height: 1 }, urls: { preview: '', thumb: '' },
});

describe('gridSections', () => {
  it('groups consecutive capture months in the home timezone, undated last', () => {
    const items = [
      item('a', '2026-09-23T12:38:00+08:00'),
      item('b', '2026-08-31T16:30:00Z'), // 2026-09-01 00:30 at home
      item('c', '2026-08-14T20:05:00+08:00'),
      item('d', null),
    ];
    expect(gridSections(items, 'all', options)).toEqual([
      { label: '2026年9月', start: 0, count: 2 },
      { label: '2026年8月', start: 2, count: 1 },
      { label: '拍摄时间未知', start: 3, count: 1 },
    ]);
  });

  it('stays flat for collections not ordered by capture time, and without any dates', () => {
    const dated = [item('a', '2026-09-23T12:38:00+08:00'), item('b', '2026-08-14T20:05:00+08:00')];
    for (const collection of ['recent', 'captured_today', 'random'] as const) {
      expect(gridSections(dated, collection, options)).toEqual([{ label: null, start: 0, count: 2 }]);
    }
    expect(gridSections([item('a', null), item('b', null)], 'all', options)).toEqual([{ label: null, start: 0, count: 2 }]);
  });

  it('treats an unparsable timestamp as unknown', () => {
    expect(monthLabel(item('x', 'not a date'), options)).toBe('拍摄时间未知');
  });
});

describe('moveInSections', () => {
  const flat: GridSection[] = [{ label: null, start: 0, count: 5 }];
  const grouped: GridSection[] = [
    { label: 'A', start: 0, count: 6 }, // rows: 0-3, 4-5
    { label: 'B', start: 6, count: 3 }, // row: 6-8
  ];

  it('matches the flat grid rules: no wrap, short tail keeps focus', () => {
    expect(moveInSections(flat, 2, 'down', 3)).toBe(2);
    expect(moveInSections(flat, 1, 'down', 3)).toBe(4);
    expect(moveInSections(flat, 2, 'right', 3)).toBe(2);
    expect(moveInSections(flat, 4, 'right', 3)).toBe(4);
  });

  it('leaves through the top and left edges', () => {
    expect(moveInSections(flat, 1, 'up', 4)).toBe(-1);
    expect(moveInSections(flat, 4, 'left', 4)).toBe(-1);
    expect(moveInSections(grouped, 6, 'left', 4)).toBe(-1);
  });

  it('crosses sections by column and clamps into short rows', () => {
    expect(moveInSections(grouped, 3, 'down', 4)).toBe(3); // own short row lacks column 3: stays, as in a flat grid
    expect(moveInSections(grouped, 5, 'down', 4)).toBe(7);
    expect(moveInSections(grouped, 4, 'down', 4)).toBe(6);
    expect(moveInSections(grouped, 8, 'up', 4)).toBe(5);
    expect(moveInSections(grouped, 6, 'up', 4)).toBe(4);
    expect(moveInSections(grouped, 8, 'down', 4)).toBe(8);
  });
});
