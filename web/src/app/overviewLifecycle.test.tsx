import { act, render, renderHook } from '@testing-library/react';
import { useReducer } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ApiClient } from '../core/api';
import type * as WsModule from '../core/ws';
import type { WsClientOptions } from '../core/ws';
import type { ServerMessage } from '../types/ws';
import { overviewFixture } from '../test/overviewFixture';
import { createOverviewLoader } from './overview';
import { createInitialState, appReducer } from './state';
import type { AppAction, AppState } from './state';
import { useOverviewLifecycle } from './useOverviewLifecycle';
import { useConnection } from './useConnection';

const socket = vi.hoisted(() => ({ options: null as WsClientOptions | null }));
vi.mock('../core/ws', async original => ({ ...await original<typeof WsModule>(), createWsClient: (options: WsClientOptions) => {
  socket.options = options; return { start() {}, stop() {}, sendState() {}, sendAck() {} };
} }));
afterEach(() => { vi.useRealTimers(); Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); });
function visibility(value: string) { Object.defineProperty(document, 'visibilityState', { configurable: true, value }); document.dispatchEvent(new Event('visibilitychange')); }
it('polls at 30s only on visible Overview, refreshes immediately on foreground, entry and reconnect, and cleans timers', async () => {
  vi.useFakeTimers();
  const overview = { load: vi.fn(async () => {}), invalidate: vi.fn() };
  const hook = renderHook(({ active, connection }) => useOverviewLifecycle(overview, active, connection), { initialProps: { active: true, connection: 'online' } });
  expect(overview.load).toHaveBeenCalledTimes(1);
  await act(() => vi.advanceTimersByTimeAsync(29999)); expect(overview.load).toHaveBeenCalledTimes(1);
  await act(() => vi.advanceTimersByTimeAsync(1)); expect(overview.load).toHaveBeenCalledTimes(2);
  act(() => visibility('hidden')); await act(() => vi.advanceTimersByTimeAsync(90000)); expect(overview.load).toHaveBeenCalledTimes(2);
  act(() => visibility('visible')); expect(overview.load).toHaveBeenCalledTimes(3);
  hook.rerender({ active: false, connection: 'online' }); await act(() => vi.advanceTimersByTimeAsync(60000)); expect(overview.load).toHaveBeenCalledTimes(3);
  hook.rerender({ active: true, connection: 'reconnecting' }); expect(overview.load).toHaveBeenCalledTimes(4);
  hook.rerender({ active: true, connection: 'online' }); expect(overview.load).toHaveBeenCalledTimes(5);
  hook.unmount(); expect(vi.getTimerCount()).toBe(0);
});
it('source notifications purge and invalidate before throttling; a delayed throttle cannot poll a hidden Overview', async () => {
  vi.useFakeTimers();
  let state: AppState;
  let send!: (action: AppAction) => void;
  let resolveOld!: (snapshot: ReturnType<typeof overviewFixture>) => void;
  const getOverview = vi.fn(async () => overviewFixture());
  const api = { getOverview, getHome: vi.fn(async () => ({ server_time: overviewFixture().generated_at, widgets: [] })) } as unknown as ApiClient;
  const overview = createOverviewLoader(api, action => send(action));
  const house = { load: async () => {}, invalidate: () => {} };
  function Harness() {
    const [current, dispatch] = useReducer(appReducer, 0, createInitialState);
    state = current; send = dispatch;
    useConnection({ api, house, overview, state: current, dispatch, clientVersion: 'test', enabled: true });
    return null;
  }
  render(<Harness />);
  act(() => send({ type: 'router.navigate', route: { name: 'briefing' } }));
  await act(() => overview.load());
  getOverview.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve; }));
  const notify = () => socket.options!.onMessage({ type: 'data.changed', payload: { topics: ['nas'], version: 2 } } as ServerMessage);
  act(notify); expect(state!.overview.snapshot).toBeNull();
  // Another source change within the throttle interval must invalidate this request immediately.
  act(notify);
  await act(async () => { resolveOld(overviewFixture()); await Promise.resolve(); });
  expect(state!.overview.snapshot).toBeNull();
  act(() => visibility('hidden'));
  const calls = getOverview.mock.calls.length;
  await act(() => vi.advanceTimersByTimeAsync(1000));
  expect(getOverview).toHaveBeenCalledTimes(calls);
});
