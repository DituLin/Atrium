import { describe, expect, it, vi } from 'vitest';
import type { ApiClient } from '../core/api';
import type { PhotoItem } from '../types/api';
import { createRefetchers } from './refetchers';
import { appReducer, createInitialState } from './state';
import type { AppAction } from './state';

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
  await Promise.resolve(); expect(settled).toBe(false); expect(getHome).not.toHaveBeenCalled();
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
  await Promise.resolve(); expect(settled).toBe(false); expect(getHome).not.toHaveBeenCalled();
  finish(); await task; expect(state.router).toBe(router);
});
