import { describe, expect, it, vi } from 'vitest';
import type { ApiClient } from '../core/api';
import type { PhotoItem } from '../types/api';
import { createRefetchers } from './refetchers';
import { appReducer, createInitialState } from './state';
import type { AppAction } from './state';
import { syncSnapshot } from './snapshotSync';

const photos = Array.from({ length: 120 }, (_, i) => ({ id: `p${i}`, preview: { status: 'ready' } }) as PhotoItem);
function setup(items = photos) {
  let state = createInitialState(0);
  const dispatch = (action: AppAction) => { state = appReducer(state, action); };
  dispatch({ type: 'router.navigate', route: { name: 'photos', collection: 'all' } });
  dispatch({ type: 'photos.open', collection: 'all' });
  dispatch({ type: 'photos.pageLoaded', collection: 'all', generation: state.collection.generation, items: photos.slice(0, 100), nextCursor: '100', meta: null, append: false });
  dispatch({ type: 'photos.focus', index: 75 });
  dispatch({ type: 'photos.remember', scrollTop: 560 });
  dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: 'p75', collection: 'all' } });
  const listPhotos = vi.fn(async ({ cursor }: { cursor?: string }) => {
    const start = Number(cursor ?? 0);
    return { items: items.slice(start, start + 50), next_cursor: start + 50 < items.length ? String(start + 50) : null };
  });
  const refetchers = createRefetchers({ api: { listPhotos } as unknown as ApiClient, dispatch, getState: () => state });
  return { dispatch, get: () => state, listPhotos, refetchers };
}

describe('refreshing a viewer return window', () => {
  it('rehydrates every loaded page before restoring a paginated card', async () => {
    const h = setup();
    await h.refetchers.collection();
    h.dispatch({ type: 'router.back' });
    expect(h.get().collection.items[h.get().collection.focusIndex]?.id).toBe('p75');
    expect(h.get().collection.returnNotice).toBeNull();
    expect(h.get().collection.returnFocus?.scrollTop).toBe(560);
    expect(h.get().collection.items).toHaveLength(100);
    expect(h.get().collection.cursor).toBe('100');
    expect(h.listPhotos).toHaveBeenCalledTimes(2);
  });
  it('selects a nearest valid card when a refreshed complete window confirms removal', async () => {
    const h = setup(photos.slice(0, 100).filter(item => item.id !== 'p75'));
    await h.refetchers.collection();
    h.dispatch({ type: 'router.back' });
    expect(h.get().collection.items[h.get().collection.focusIndex]?.id).toBe('p76');
    expect(h.get().collection.returnNotice).toContain('原照片已移除');
  });
});

it('retains the known window when new rows move the entry beyond a bounded refresh', async () => {
  const newcomers = Array.from({ length: 50 }, (_, i) => ({ id: `new${i}` }) as PhotoItem);
  const h = setup([...newcomers, ...photos]);
  await h.refetchers.collection();
  h.dispatch({ type: 'router.back' });
  expect(h.get().collection.items[h.get().collection.focusIndex]?.id).toBe('p75');
  expect(h.get().collection.returnNotice).not.toContain('已移除');
  expect(h.get().collection.returnNotice).toContain('保留原浏览位置');
  expect(h.listPhotos).toHaveBeenCalledTimes(2);
});

it('does not overwrite pagination and a newer bookmark established during refresh', async () => {
  const h = setup();
  // Reproduce the first-page window before refresh starts.
  h.dispatch({ type: 'router.back' });
  h.dispatch({ type: 'photos.restored' });
  h.dispatch({ type: 'photos.pageLoaded', collection: 'all', generation: h.get().collection.generation, items: photos.slice(0, 50), nextCursor: '50', meta: null, append: false });
  h.dispatch({ type: 'photos.focus', index: 5 });
  let resolve!: (value: { items: PhotoItem[]; next_cursor: string }) => void;
  h.listPhotos.mockImplementationOnce(() => new Promise(done => { resolve = done; }));
  const refresh = h.refetchers.collection();
  h.dispatch({ type: 'photos.pageLoaded', collection: 'all', generation: h.get().collection.generation, items: photos.slice(50, 100), nextCursor: '100', meta: null, append: true });
  h.dispatch({ type: 'photos.focus', index: 75 });
  h.dispatch({ type: 'photos.remember', scrollTop: 900 });
  h.dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: 'p75', collection: 'all' } });
  resolve({ items: photos.slice(0, 50), next_cursor: '50' });
  await refresh;
  h.dispatch({ type: 'router.back' });
  expect(h.get().collection.items).toHaveLength(100);
  expect(h.get().collection.items[h.get().collection.focusIndex]?.id).toBe('p75');
  expect(h.get().collection.returnFocus?.scrollTop).toBe(900);
  expect(h.get().collection.returnNotice).not.toContain('已移除');
});

it('awaits House independently of failing home and preserves route, history and focus on remote refresh', async () => {
  let state = createInitialState(0);
  const dispatch = (action: AppAction) => { state = appReducer(state, action); };
  dispatch({ type: 'router.navigate', route: { name: 'house' }, sourceFocus: 'nav-house' });
  const router = state.router;
  let finish!: () => void;
  const house = { load: vi.fn(() => new Promise<void>(resolve => { finish = resolve; })), invalidate() {} };
  const getHome = vi.fn(async () => { throw new Error('photo query failure'); });
  const refetch = createRefetchers({ api: { getHome } as unknown as ApiClient, house, dispatch, getState: () => state });
  let settled = false; const task = refetch.route({ name: 'house' }).then(() => { settled = true; });
  await Promise.resolve(); expect(settled).toBe(false); expect(getHome).toHaveBeenCalledTimes(1);
  finish(); await task; expect(state.router).toBe(router);
});

it('awaits Overview independently of failing home and preserves route, history and focus on remote refresh', async () => {
  let state = createInitialState(0);
  const dispatch = (action: AppAction) => { state = appReducer(state, action); };
  dispatch({ type: 'router.navigate', route: { name: 'briefing' }, sourceFocus: 'nav-overview' });
  const router = state.router;
  let finish!: () => void;
  const overview = { load: vi.fn(() => new Promise<void>(resolve => { finish = resolve; })), invalidate() {} };
  const getHome = vi.fn(async () => { throw new Error('photo query failure'); });
  const refetch = createRefetchers({ api: { getHome } as unknown as ApiClient, overview, dispatch, getState: () => state });
  let settled = false; const task = refetch.route({ name: 'briefing' }).then(() => { settled = true; });
  await Promise.resolve(); expect(settled).toBe(false); expect(getHome).toHaveBeenCalledTimes(1);
  finish(); await task; expect(state.router).toBe(router);
});

it('awaits optional Today weather and keeps a newer Home refresh when replies arrive out of order', async () => {
  const older = { schema_version: 1 as const, version: 1, server_time: '2026-11-04T08:00:00Z', home: { name: 'Synthetic home', timezone: 'UTC' }, widgets: [] };
  const newer = { ...older, version: 2, server_time: '2026-11-04T08:00:01Z' };
  let state = createInitialState(0);
  let first!: (value: typeof older) => void;
  let second!: (value: typeof older) => void;
  const getHome = vi.fn().mockImplementationOnce(() => new Promise(resolve => { first = resolve; }))
    .mockImplementationOnce(() => new Promise(resolve => { second = resolve; }));
  const refetch = createRefetchers({ api: { getHome } as unknown as ApiClient, overview: { load: async () => {}, invalidate() {} },
    dispatch: action => { state = appReducer(state, action); }, getState: () => state });
  let settled = false;
  const route = refetch.route({ name: 'briefing' }).then(() => { settled = true; });
  await Promise.resolve(); expect(settled).toBe(false);
  const recent = refetch.home();
  expect(getHome).toHaveBeenCalledTimes(2);
  second(newer); await recent;
  first(older); await route;
  expect(state.home?.version).toBe(2);
});

it.each([false, true])('does not let another refresh owner or socket preflight replace newer weather (preflight=%s)', async preflight => {
  const older = { schema_version: 1 as const, version: 1, server_time: '2026-11-04T08:00:00Z', home: { name: 'Synthetic home', timezone: 'UTC' }, widgets: [] };
  const newer = { ...older, version: 2, server_time: '2026-11-04T08:00:01Z' };
  let state = createInitialState(0);
  let first!: (value: typeof older) => void;
  let second!: (value: typeof older) => void;
  const getHome = vi.fn().mockImplementationOnce(() => new Promise(resolve => { first = resolve; }))
    .mockImplementationOnce(() => new Promise(resolve => { second = resolve; }));
  const api = { getHome } as unknown as ApiClient;
  const ports = { api, dispatch: (action: AppAction) => { state = appReducer(state, action); }, getState: () => state };
  const firstOwner = createRefetchers(ports), secondOwner = createRefetchers(ports);
  const pending = preflight ? syncSnapshot(ports) : firstOwner.home();
  const recent = secondOwner.home();
  second(newer); await recent;
  const clock = state.clock;
  first(older); await pending;
  expect(state.home?.version).toBe(2);
  expect(state.clock).toBe(clock);
});

it('refreshes daily memories inclusively around the visible photo and drops stale refreshes after movement', async () => {
  let state = createInitialState(0);
  const dispatch = (action: AppAction) => { state = appReducer(state, action); };
  const item = (id: string) => ({ id, preview: { status: 'ready' }, resume_cursor: `inclusive:${id}` }) as PhotoItem & { resume_cursor: string };
  const page = { items: [item('b'), item('c')], next_cursor: null, meta: { collection: 'memories' as const, day: '2026-11-02', screen_id: 'tv', total: 2 } };
  dispatch({ type: 'slideshow.memoriesStarted', resumeCursor: null });
  dispatch({ type: 'slideshow.memoriesLoaded', generation: state.slideshow.generation, page });
  dispatch({ type: 'slideshow.displayed', id: 'b', generation: state.slideshow.generation });
  let finish!: (value: typeof page) => void;
  const listMemories = vi.fn(() => new Promise<typeof page>(resolve => { finish = resolve; }));
  const refetch = createRefetchers({ api: { listMemories } as unknown as ApiClient, dispatch, getState: () => state });
  const pending = refetch.slideshow();
  expect(listMemories).toHaveBeenCalledWith({ cursor: 'inclusive:b', limit: 50 });
  dispatch({ type: 'slideshow.advance' });
  dispatch({ type: 'slideshow.displayed', id: 'c', generation: state.slideshow.generation });
  finish(page); await pending;
  expect(state.slideshow.displayedId).toBe('c');
  expect(state.slideshow.round[state.slideshow.index]?.id).toBe('c');
});
