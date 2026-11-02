import { beforeEach, describe, expect, it, vi } from 'vitest';
import { clearMemoriesProgress, loadMemoriesProgress, saveMemoriesProgress } from './memoriesProgress';

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); });
const progress = { v: 1 as const, day: '2026-11-02', screenID: 'screen-a', resumeCursor: 'opaque-inclusive-anchor' };
describe('daily memories progress', () => {
  it('persists only a successful display anchor and isolates screens and days', () => {
    saveMemoriesProgress(progress);
    expect(loadMemoriesProgress('screen-a', '2026-11-02')).toEqual(progress);
    expect(loadMemoriesProgress('screen-b', '2026-11-02')).toBeNull();
    expect(loadMemoriesProgress('screen-a', '2026-11-03')).toBeNull();
    expect(JSON.parse(localStorage.getItem('atrium.memories_progress')!)).toEqual(progress);
    clearMemoriesProgress();
    expect(loadMemoriesProgress('screen-a', '2026-11-02')).toBeNull();
  });
  it.each(['{', 'null', '{"v":2}', '{"v":1,"day":"2026-11-02","screenID":"screen-a","resumeCursor":""}'])('ignores malformed progress %s', raw => {
    localStorage.setItem('atrium.memories_progress', raw);
    expect(loadMemoriesProgress('screen-a', '2026-11-02')).toBeNull();
  });
  it('survives disabled storage and quota errors', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('disabled'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('disabled'); });
    expect(() => saveMemoriesProgress(progress)).not.toThrow();
    expect(loadMemoriesProgress('screen-a', progress.day)).toBeNull();
    expect(() => clearMemoriesProgress()).not.toThrow();
  });
});
