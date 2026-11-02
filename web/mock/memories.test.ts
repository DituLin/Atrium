import { describe, expect, it } from 'vitest';
import { memoriesPage } from './memories.ts';
import { PHOTOS } from './data.ts';
describe('mock daily memories contract', () => {
  it('keeps one daily order, resumes inclusively, pages exclusively, and resets old-day cursors', () => {
    const first = memoriesPage(PHOTOS, '2026-11-02', 'tv-a', null, 2);
    expect(first).toEqual(memoriesPage(PHOTOS, '2026-11-02', 'tv-a', null, 2));
    const resume = memoriesPage(PHOTOS, '2026-11-02', 'tv-a', first.items[1]!.resume_cursor, 2);
    expect(resume.items[0]?.id).toBe(first.items[1]?.id);
    const next = memoriesPage(PHOTOS, '2026-11-02', 'tv-a', first.next_cursor, 2);
    expect(next.items[0]?.id).not.toBe(first.items[1]?.id);
    expect(memoriesPage(PHOTOS, '2026-11-03', 'tv-a', first.next_cursor, 2))
      .toEqual(memoriesPage(PHOTOS, '2026-11-03', 'tv-a', null, 2));
    expect(() => memoriesPage(PHOTOS, '2026-11-02', 'tv-b', first.next_cursor, 2)).toThrow();
  });
});
