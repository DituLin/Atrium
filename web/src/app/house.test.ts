import { describe, expect, it, vi } from 'vitest';
import { createHouseLoader, houseAvailability } from './house';
import { appReducer, createInitialState, selectScreen } from './state';
import type { AppAction } from './state';
import type { HouseResponse } from '../types/api';
import { serverNow } from '../core/clock';

import { houseFixture } from '../test/houseFixture';
function harness() {
  let state = createInitialState(1000);
  const dispatch = (action: AppAction) => { state = appReducer(state, action); };
  const getHouse = vi.fn(async () => houseFixture());
  const loader = createHouseLoader({ getHouse }, dispatch, () => 1000);
  return { get: () => state, dispatch, getHouse, loader };
}
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }

describe('House public projection lifecycle', () => {
  it('coalesces concurrent loads and uses generated_at for the exact server-clock expiry boundary', async () => {
    const h = harness(); const first = h.loader.load(); const second = h.loader.load();
    expect(first).toBe(second); await first;
    expect(h.getHouse).toHaveBeenCalledTimes(1);
    const state = h.get();
    expect(houseAvailability(state.house.snapshot!.nas[0]!, serverNow(state.clock, 60999), false)).toBe('available');
    expect(houseAvailability(state.house.snapshot!.nas[0]!, serverNow(state.clock, 61000), false)).toBe('stale');
  });
  it('retains authorized data as stale on transport failure, but replaces server failed and empty collections', async () => {
    const h = harness(); await h.loader.load(); h.getHouse.mockRejectedValueOnce(new Error('offline'));
    await expect(h.loader.load()).rejects.toThrow('offline');
    expect(h.get().house.status).toBe('stale'); expect(h.get().house.snapshot?.nas[0]?.items[0]?.health).toBe('offline');
    const failed = houseFixture(); failed.nas[0] = { ...failed.nas[0]!, availability: 'failed', reason: 'read_failed', observed_at: null, expires_at: null, items: [] };
    h.getHouse.mockResolvedValueOnce(failed); await h.loader.load(); expect(h.get().house.snapshot?.nas[0]?.items).toEqual([]);
    h.getHouse.mockResolvedValueOnce({ ...houseFixture(), nas: [] }); await h.loader.load(); expect(h.get().house.snapshot?.nas).toEqual([]);
  });
  it('fails honestly without a previous projection', async () => {
    const h = harness(); h.getHouse.mockRejectedValueOnce(new Error('offline')); const task = h.loader.load();
    expect(h.get().house.status).toBe('loading'); await expect(task).rejects.toThrow(); expect(h.get().house.status).toBe('failed');
    const retry = h.loader.load(); expect(h.get().house.status).toBe('loading'); await retry;
  });
  it('invalidates pending work immediately at source change; old results cannot revive removed sources', async () => {
    const h = harness(); await h.loader.load(); const pending = deferred<HouseResponse>(); h.getHouse.mockReturnValueOnce(pending.promise);
    const old = h.loader.load().catch(error => error); h.loader.invalidate(); expect(h.get().house.snapshot).toBeNull();
    h.getHouse.mockResolvedValueOnce({ ...houseFixture(), nas: [] }); await h.loader.load(); pending.resolve(houseFixture());
    expect(await old).toMatchObject({ name: 'AbortError' }); expect(h.get().house.snapshot?.nas).toEqual([]);
  });
  it('purges at auth expiry and rejects late actions, and keeps House independent of home/WS after fresh authorization', async () => {
    const h = harness(); await h.loader.load(); h.dispatch({ type: 'router.navigate', route: { name: 'house' } });
    expect(selectScreen(h.get())).toBe('house'); h.dispatch({ type: 'connection.superseded' }); expect(selectScreen(h.get())).toBe('connect');
    h.dispatch({ type: 'app.authExpired' }); expect(h.get().house.snapshot).toBeNull();
    h.dispatch({ type: 'app.needsPairing' }); h.dispatch({ type: 'app.authOk' }); expect(selectScreen(h.get())).toBe('pair');
  });
});

it('aborts invalidated source fetches rather than accumulating outstanding transport requests', async () => {
  let active = 0; let peak = 0;
  const getHouse = vi.fn((signal?: AbortSignal) => new Promise<HouseResponse>((_, reject) => {
    active += 1; peak = Math.max(active, peak);
    signal?.addEventListener('abort', () => { active -= 1; reject(new DOMException('Cancelled', 'AbortError')); });
  }));
  const loader = createHouseLoader({ getHouse }, () => {});
  const tasks: Promise<unknown>[] = [];
  for (let count = 0; count < 10; count++) { tasks.push(loader.load().catch(error => error)); loader.invalidate(); }
  expect(active).toBe(0); expect(peak).toBe(1);
  await Promise.all(tasks);
});
