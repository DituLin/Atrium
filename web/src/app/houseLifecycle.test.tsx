import { act, render, renderHook } from '@testing-library/react';
import { useReducer } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ApiClient } from '../core/api';
import type * as WsModule from '../core/ws';
import type { WsClientOptions } from '../core/ws';
import type { ServerMessage } from '../types/ws';
import { houseFixture } from '../test/houseFixture';
import { createHouseLoader } from './house';
import { createInitialState, appReducer } from './state';
import type { AppAction, AppState } from './state';
import { useHouseLifecycle } from './useHouseLifecycle';
import { useConnection } from './useConnection';

const socket = vi.hoisted(() => ({ options: null as WsClientOptions | null }));
vi.mock('../core/ws', async original => ({ ...await original<typeof WsModule>(), createWsClient: (options: WsClientOptions) => {
  socket.options = options; return { start() {}, stop() {}, sendState() {}, sendAck() {} };
} }));
afterEach(() => { vi.useRealTimers(); Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); });
function visibility(value: string) { Object.defineProperty(document, 'visibilityState', { configurable: true, value }); document.dispatchEvent(new Event('visibilitychange')); }
it('polls at 30s only on visible House, refreshes immediately on foreground, entry and reconnect, and cleans timers', async () => {
  vi.useFakeTimers();
  const house = { load: vi.fn(async () => {}), invalidate: vi.fn() };
  const hook = renderHook(({ active, connection }) => useHouseLifecycle(house, active, connection), { initialProps: { active: true, connection: 'online' } });
  expect(house.load).toHaveBeenCalledTimes(1);
  await act(() => vi.advanceTimersByTimeAsync(29999)); expect(house.load).toHaveBeenCalledTimes(1);
  await act(() => vi.advanceTimersByTimeAsync(1)); expect(house.load).toHaveBeenCalledTimes(2);
  act(() => visibility('hidden')); await act(() => vi.advanceTimersByTimeAsync(90000)); expect(house.load).toHaveBeenCalledTimes(2);
  act(() => visibility('visible')); expect(house.load).toHaveBeenCalledTimes(3);
  hook.rerender({ active: false, connection: 'online' }); await act(() => vi.advanceTimersByTimeAsync(60000)); expect(house.load).toHaveBeenCalledTimes(3);
  hook.rerender({ active: true, connection: 'reconnecting' }); expect(house.load).toHaveBeenCalledTimes(4);
  hook.rerender({ active: true, connection: 'online' }); expect(house.load).toHaveBeenCalledTimes(5);
  hook.unmount(); expect(vi.getTimerCount()).toBe(0);
});
it('source notifications purge and invalidate before throttling; a delayed throttle cannot poll a hidden House', async () => {
  vi.useFakeTimers();
  let state: AppState;
  let send!: (action: AppAction) => void;
  let resolveOld!: (snapshot: ReturnType<typeof houseFixture>) => void;
  const getHouse = vi.fn(async () => houseFixture());
  const api = { getHouse, getHome: vi.fn(async () => ({ server_time: houseFixture().generated_at, widgets: [] })) } as unknown as ApiClient;
  const house = createHouseLoader(api, action => send(action));
  function Harness() {
    const [current, dispatch] = useReducer(appReducer, 0, createInitialState);
    state = current; send = dispatch;
    useConnection({ api, house, state: current, dispatch, clientVersion: 'test', enabled: true });
    return null;
  }
  render(<Harness />);
  act(() => send({ type: 'router.navigate', route: { name: 'house' } }));
  await act(() => house.load());
  getHouse.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve; }));
  const notify = () => socket.options!.onMessage({ type: 'data.changed', payload: { topics: ['nas'], version: 2 } } as ServerMessage);
  act(notify); expect(state!.house.snapshot).toBeNull();
  // Another source change within the throttle interval must invalidate this request immediately.
  act(notify);
  await act(async () => { resolveOld(houseFixture()); await Promise.resolve(); });
  expect(state!.house.snapshot).toBeNull();
  act(() => visibility('hidden'));
  const calls = getHouse.mock.calls.length;
  await act(() => vi.advanceTimersByTimeAsync(1000));
  expect(getHouse).toHaveBeenCalledTimes(calls);
});
