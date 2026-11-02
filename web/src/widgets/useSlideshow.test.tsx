import type { ReactNode } from 'react';
import { StrictMode, useReducer, useState } from 'react';
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { appReducer, createInitialState } from '../app/state';
import type { AppAction, AppState } from '../app/state';
import { loadMemoriesProgress, saveMemoriesProgress } from '../app/memoriesProgress';
import { ApiError } from '../core/api';
import type { ApiClient } from '../core/api';
import type { HomeResponse, MemoriesResponse } from '../types/api';
import { decodeImage } from '../ui/decodeImage';
import { useSlideshow } from './useSlideshow';
vi.mock('../ui/decodeImage', () => ({ decodeImage: vi.fn() }));
const day = '2026-11-02';
const page = (ids: string[], currentDay = day, cursor: string | null = null): MemoriesResponse => ({
  items: ids.map(id => ({ id, source_id: 'synthetic', captured_at: null, captured_confidence: 'unknown', first_seen_at: '', is_baseline: false,
    width: 100, height: 100, preview: { status: 'ready', width: 100, height: 100 }, urls: { preview: `/image/${id}`, thumb: '' }, resume_cursor: `inclusive:${id}` })),
  next_cursor: cursor, meta: { collection: 'memories', day: currentDay, screen_id: 'tv-a', total: ids.length },
});
const flush = async () => { await act(async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); }); };
function setup(initial = page(['a', 'b']), screenID = 'tv-a', home: HomeResponse | null = { home: { timezone: 'UTC' }, widgets: [] } as unknown as HomeResponse, failIdentity = false, strict = false) {
  let setVisibility!: (visible: boolean) => void;
  let state!: AppState; let dispatch!: (action: AppAction) => void;
  const listMemories = vi.fn(async (_params: { cursor?: string | null; limit?: number }) => initial);
  const probeMedia = vi.fn(async (_id: string) => 200);
  const getScreenSelf = vi.fn(async () => ({ id: screenID, status: 'active' }));
  if (failIdentity) getScreenSelf.mockRejectedValueOnce(new Error('temporary'));
  const api = { listMemories, probeMedia, getScreenSelf, mediaUrl: (id: string) => `/image/${id}` } as unknown as ApiClient;
  const Wrapper = ({ children }: { children: ReactNode }) => {
    const [value, send] = useReducer(appReducer, { ...createInitialState(Date.now()), home }); state = value; dispatch = send;
    const [visible, setVisible] = useState(true); setVisibility = setVisible;
    return <AppContext.Provider value={{ state: value, dispatch: send, api, house: {} as never, overview: {} as never, goBack() {}, clientVersion: 'test' }}>{visible ? strict ? <StrictMode>{children}</StrictMode> : children : null}</AppContext.Provider>;
  };
  const hook = renderHook(() => useSlideshow(), { wrapper: Wrapper });
  return { ...hook, listMemories, probeMedia, getScreenSelf, get: () => state, visible: (visible: boolean) => act(() => setVisibility(visible)), send: (action: AppAction) => act(() => dispatch(action)) };
}
beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-11-02T12:00:00Z')); localStorage.clear();
  vi.mocked(decodeImage).mockImplementation(async (id, src) => ({ ok: true, image: { id, src, element: new Image() } }));
});
afterEach(() => { vi.useRealTimers(); vi.clearAllMocks(); });
describe('daily memories driver', () => {
  it('resumes the last decoded displayed photo after a reload instead of the preloaded next photo', async () => {
    const first = setup(); await flush();
    expect(first.result.current.shown?.id).toBe('a');
    expect(loadMemoriesProgress('tv-a', day)?.resumeCursor).toBe('inclusive:a');
    expect(decodeImage).toHaveBeenCalledWith('b', '/image/b');
    first.unmount();
    const reloaded = setup(page(['a', 'b'])); await flush();
    expect(reloaded.listMemories).toHaveBeenCalledWith({ cursor: 'inclusive:a', limit: 50 });
    expect(reloaded.result.current.shown?.id).toBe('a');
  });
  it('does not reuse another screen bookmark', async () => {
    saveMemoriesProgress({ v: 1, day, screenID: 'other-tv', resumeCursor: 'private-cursor' });
    const h = setup(); await flush();
    expect(h.listMemories.mock.calls[0]?.[0].cursor).toBeNull();
  });
  it.each([{ ids: [] }, { ids: ['a'] }])('rolls over at household midnight with $ids even without advancing', async ({ ids }) => {
    const h = setup(page(ids)); await flush();
    h.listMemories.mockResolvedValue(page(['tomorrow'], '2026-11-03'));
    vi.setSystemTime(new Date('2026-11-03T00:00:01Z'));
    h.send({ type: 'app.tick', nowMs: Date.now() }); await flush();
    expect(h.listMemories).toHaveBeenLastCalledWith({ cursor: null, limit: 50 });
    expect(h.result.current.shown?.id).toBe('tomorrow');
    expect(h.get().slideshow.memories?.day).toBe('2026-11-03');
  });
  it('loops the same daily order without generating a new seed', async () => {
    const h = setup(); await flush();
    h.send({ type: 'slideshow.advance' }); await flush();
    expect(h.result.current.shown?.id).toBe('b');
    h.send({ type: 'slideshow.advance' }); await flush();
    expect(h.result.current.shown?.id).toBe('a');
    expect(h.listMemories).toHaveBeenLastCalledWith({ cursor: null, limit: 50 });
  });
  it('rejects a late response and display after authorization is purged', async () => {
    let complete!: (value: MemoriesResponse) => void;
    const h = setup(); h.listMemories.mockImplementation(() => new Promise(done => { complete = done; })); await flush();
    h.send({ type: 'app.needsPairing' });
    await act(async () => { complete(page(['late'])); }); await flush();
    expect(h.get().slideshow.round).toHaveLength(0);
    expect(localStorage.getItem('atrium.memories_progress')).toBeNull();
    expect(h.result.current.shown).toBeNull();
  });
  it('bounds all-failed rounds and retries only after the backoff', async () => {
    const h = setup(); h.probeMedia.mockResolvedValue(503); await flush();
    expect(h.probeMedia).toHaveBeenCalledTimes(2);
    expect(h.listMemories).toHaveBeenCalledTimes(1);
    h.send({ type: 'app.tick', nowMs: Date.now() + 1000 }); await flush();
    expect(h.listMemories).toHaveBeenCalledTimes(1);
    expect(h.result.current.status).toBe('empty');
    vi.setSystemTime(Date.now() + 60_001); h.send({ type: 'app.tick', nowMs: Date.now() }); await flush();
    expect(h.listMemories).toHaveBeenCalledTimes(2);
  });
  it('retries a failed screen identification only after a bounded delay', async () => {
    const h = setup(page(['a']), 'tv-a', null, true); await flush();
    expect(h.getScreenSelf).toHaveBeenCalledTimes(1);
    await act(async () => { vi.advanceTimersByTime(29_999); }); await flush();
    expect(h.getScreenSelf).toHaveBeenCalledTimes(1);
    await act(async () => { vi.advanceTimersByTime(1); }); await flush();
    expect(h.getScreenSelf).toHaveBeenCalledTimes(2);
    expect(h.result.current.shown?.id).toBe('a');
  });
  it('uses the household timezone for midnight and never resets authoritative metadata before home loads', async () => {
    vi.setSystemTime(new Date('2026-11-02T15:59:59Z'));
    const h = setup(page(['a']), 'tv-a', null); await flush();
    expect(h.listMemories).toHaveBeenCalledTimes(1);
    h.send({ type: 'app.homeLoaded', home: { server_time: new Date().toISOString(), home: { timezone: 'Asia/Singapore' }, widgets: [] } as unknown as HomeResponse, receivedAt: Date.now() }); await flush();
    expect(h.listMemories).toHaveBeenCalledTimes(1);
    h.listMemories.mockResolvedValue(page(['next-day'], '2026-11-03'));
    vi.setSystemTime(new Date('2026-11-02T16:00:01Z')); h.send({ type: 'app.tick', nowMs: Date.now() }); await flush();
    expect(h.result.current.shown?.id).toBe('next-day');
    expect(h.listMemories).toHaveBeenCalledTimes(2);
  });
  it('continues through an empty filtered page when the server still has a continuation cursor', async () => {
    const h = setup(page([], day, 'exclusive:continue'));
    h.listMemories.mockResolvedValueOnce(page([], day, 'exclusive:continue')).mockResolvedValue(page(['later'])); await flush();
    expect(h.listMemories).toHaveBeenLastCalledWith({ cursor: 'exclusive:continue', limit: 50 });
    expect(h.result.current.shown?.id).toBe('later');
  });
  it('discards an invalid stored cursor and starts a fresh authorized daily page', async () => {
    saveMemoriesProgress({ v: 1, day, screenID: 'tv-a', resumeCursor: 'corrupt-but-opaque' });
    const h = setup(); h.listMemories.mockRejectedValueOnce(new ApiError(400, 'invalid_request', 'cursor')); await flush();
    expect(h.listMemories).toHaveBeenNthCalledWith(1, { cursor: 'corrupt-but-opaque', limit: 50 });
    expect(h.listMemories).toHaveBeenNthCalledWith(2, { cursor: null, limit: 50 });
    expect(h.result.current.shown?.id).toBe('a');
  });

  it('returns from another tab to the same daily photo and starts today on an overnight return', async () => {
    const h = setup(); await flush();
    h.send({ type: 'slideshow.advance' }); await flush();
    expect(h.result.current.shown?.id).toBe('b');
    h.visible(false); h.visible(true); await flush();
    expect(h.result.current.shown?.id).toBe('b');
    expect(h.listMemories).toHaveBeenCalledTimes(1);
    h.visible(false); vi.setSystemTime(new Date('2026-11-03T00:01:00Z'));
    h.send({ type: 'app.tick', nowMs: Date.now() });
    h.listMemories.mockResolvedValue(page(['morning'], '2026-11-03'));
    h.visible(true); await flush();
    expect(h.result.current.shown?.id).toBe('morning');
    expect(h.listMemories).toHaveBeenLastCalledWith({ cursor: null, limit: 50 });
  });
  it('ignores a slow prior-day decode that finishes after midnight', async () => {
    let finish!: (value: Awaited<ReturnType<typeof decodeImage>>) => void;
    vi.mocked(decodeImage).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    const h = setup(page(['a'])); await flush();
    h.listMemories.mockResolvedValue(page(['tomorrow'], '2026-11-03'));
    vi.setSystemTime(new Date('2026-11-03T00:00:01Z')); h.send({ type: 'app.tick', nowMs: Date.now() }); await flush();
    expect(h.result.current.shown?.id).toBe('tomorrow');
    await act(async () => { finish({ ok: true, image: { id: 'a', src: '/old', element: new Image() } }); }); await flush();
    expect(h.result.current.shown?.id).toBe('tomorrow');
    expect(loadMemoriesProgress('tv-a', '2026-11-03')?.resumeCursor).toBe('inclusive:tomorrow');
  });
  it('keeps a refreshed current image decoded but immediately drops a removed source image', async () => {
    const h = setup(); await flush();
    const decoded = h.result.current.shown;
    h.send({ type: 'slideshow.memoriesLoaded', generation: h.get().slideshow.generation, page: page(['a', 'new']), replace: true }); await flush();
    expect(h.result.current.shown).toBe(decoded);
    let finish!: (value: Awaited<ReturnType<typeof decodeImage>>) => void;
    vi.mocked(decodeImage).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    h.send({ type: 'slideshow.memoriesLoaded', generation: h.get().slideshow.generation, page: page(['replacement']), replace: true }); await flush();
    expect(h.result.current.shown).toBeNull();
    await act(async () => { finish({ ok: true, image: { id: 'replacement', src: '/replacement', element: new Image() } }); }); await flush();
    expect(h.result.current.shown?.id).toBe('replacement');
  });

});

it('ignores page work from an old dashboard mount without cancelling a slow request on clock ticks', async () => {
  let finish!: (value: MemoriesResponse) => void;
  const h = setup();
  h.listMemories.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; })); await flush();
  h.send({ type: 'app.tick', nowMs: Date.now() + 1000 }); await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(1);
  h.visible(false);
  await act(async () => { finish(page(['old-mount'])); }); await flush();
  expect(h.get().slideshow.round).toHaveLength(0);
  h.listMemories.mockResolvedValue(page(['new-mount'])); h.visible(true); await flush();
  expect(h.result.current.shown?.id).toBe('new-mount');
});

it('starts again at the daily beginning when a removed saved tail anchor has no later items', async () => {
  saveMemoriesProgress({ v: 1, day, screenID: 'tv-a', resumeCursor: 'deleted-tail-anchor' });
  const emptyTail = page([]); emptyTail.meta.total = 4;
  const h = setup(emptyTail);
  h.listMemories.mockResolvedValueOnce(emptyTail).mockResolvedValue(page(['first-valid'])); await flush();
  expect(h.listMemories).toHaveBeenNthCalledWith(1, { cursor: 'deleted-tail-anchor', limit: 50 });
  expect(h.listMemories).toHaveBeenNthCalledWith(2, { cursor: null, limit: 50 });
  expect(h.result.current.shown?.id).toBe('first-valid');
});

it('backs off if a nonempty server total repeatedly yields empty terminal windows', async () => {
  const emptyTail = page([]); emptyTail.meta.total = 4;
  const h = setup(emptyTail); await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(2);
  h.send({ type: 'app.tick', nowMs: Date.now() + 1000 }); await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(2);
  vi.setSystemTime(Date.now() + 60_001); h.send({ type: 'app.tick', nowMs: Date.now() }); await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(3);
});

it('does not reuse a pending preload from before an authorization refresh barrier', async () => {
  let finish!: (value: Awaited<ReturnType<typeof decodeImage>>) => void;
  vi.mocked(decodeImage).mockImplementation(async (id, src) => id === 'b'
    ? new Promise(resolve => { finish = resolve; })
    : { ok: true, image: { id, src, element: new Image() } });
  const h = setup(); await flush();
  const stale = finish;
  vi.mocked(decodeImage).mockImplementation(async (id, src) => ({ ok: true, image: { id, src, element: new Image() } }));
  h.send({ type: 'slideshow.memoriesLoaded', generation: h.get().slideshow.generation, page: page(['a', 'b']), replace: true }); await flush();
  await act(async () => { stale({ ok: true, image: { id: 'b', src: '/stale-preload', element: new Image() } }); }); await flush();
  h.send({ type: 'slideshow.advance' }); await flush();
  expect(h.result.current.shown?.src).toBe('/image/b');
});


it('reissues a pending first window after StrictMode replays the returning dashboard mount', async () => {
  const h = setup(page(['a']), 'tv-a', null, false, true);
  h.listMemories.mockImplementationOnce(() => new Promise(() => {})); await flush();
  expect(h.get().slideshow.memoriesMode).toBe(true);
  expect(h.get().slideshow.loaded).toBe(false);
  h.visible(false); h.listMemories.mockResolvedValue(page(['remounted'])); h.visible(true); await flush();
  expect(h.result.current.shown?.id).toBe('remounted');
});

it('backs off when indexed photos keep returning removed media responses', async () => {
  const h = setup(); h.probeMedia.mockResolvedValue(404);
  // Bound the failing implementation too: a third request returns a truly
  // empty library instead of allowing an infinite microtask/effect loop.
  h.listMemories.mockResolvedValueOnce(page(['a', 'b'])).mockResolvedValueOnce(page(['a', 'b'])).mockResolvedValue(page([]));
  await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(2);
  expect(h.result.current.status).toBe('empty');
  h.send({ type: 'app.tick', nowMs: Date.now() + 1000 }); await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(2);
  vi.setSystemTime(Date.now() + 60_001); h.send({ type: 'app.tick', nowMs: Date.now() }); await flush();
  expect(h.listMemories).toHaveBeenCalledTimes(3);
});
