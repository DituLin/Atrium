import { act, fireEvent, render, waitFor } from '@testing-library/react';
import { StrictMode, useReducer } from 'react';
import { expect, it, vi } from 'vitest';
import type { ApiClient } from '../core/api';
import type { WsClientOptions } from '../core/ws';
import type * as WsModule from '../core/ws';
import { PhotoScreen } from '../screens/PhotoScreen';
import type { PhotoItem } from '../types/api';
import type { CommandAckPayload, ServerMessage } from '../types/ws';
import { AppContext } from './context';
import { appReducer, createInitialState } from './state';
import type { AppAction, AppState } from './state';
import { useConnection } from './useConnection';
const socket = vi.hoisted(() => ({ options: null as WsClientOptions | null, acks: [] as CommandAckPayload[] }));
vi.mock('../core/ws', async (original) => ({
  ...await original<typeof WsModule>(),
  createWsClient: (options: WsClientOptions) => {
    socket.options = options;
    return { start() {}, stop() {}, sendState() {}, sendAck(ack: CommandAckPayload) { socket.acks.push(ack); } };
  },
}));
const item: PhotoItem = { id: 'photo', source_id: 'source', captured_at: null, captured_confidence: 'unknown', first_seen_at: '2026-09-07T00:00:00Z', is_baseline: false, width: 100, height: 100, preview: { status: 'ready', width: 100, height: 100 }, urls: { preview: '/preview', thumb: '/thumb' } };
function harness(strict = false, probeMedia = vi.fn(async () => 200)) {
  socket.acks = [];
  let current: AppState;
  let dispatch: (action: AppAction) => void;
  const api = { getPhoto: vi.fn(async () => ({ item })), probeMedia, mediaUrl: () => '/preview' } as unknown as ApiClient;
  function Harness() {
    const [state, send] = useReducer(appReducer, 0, createInitialState);
    current = state; dispatch = send;
    useConnection({ api, state, dispatch: send, enabled: true, clientVersion: 'test' });
    return <AppContext.Provider value={{ api, state, dispatch: send, clientVersion: 'test', goBack: () => send({ type: 'router.back' }) }}>{state.router.route.name === 'photo' ? <PhotoScreen /> : <div>Dashboard</div>}</AppContext.Provider>;
  }
  const view = render(strict ? <StrictMode><Harness /></StrictMode> : <Harness />);
  return { view, state: () => current,
    dispatch: (action: AppAction) => act(() => dispatch(action)),
    command: (sequence: number, kind: 'show' | 'navigate') => act(() => socket.options!.onMessage({ schema_version: 1, id: `event${sequence}`, sent_at: '2026-09-07T00:00:00Z', type: 'screen.command', payload: { command_id: `cmd${sequence}`, sequence, kind, payload: kind === 'show' ? { photo_id: 'photo' } : { route: 'dashboard' }, issued_at: '2026-09-07T00:00:00Z', expires_at: '2026-09-07T00:00:10Z' } } as ServerMessage)),
  };
}
for (const leave of [false, true]) {
  it(`requires a fresh image load for same-photo show; leave=${leave}`, async () => {
    const h = harness(); h.command(1, 'show');
    await waitFor(() => expect(h.view.container.querySelector('img')).not.toBeNull());
    const first = h.view.container.querySelector('img')!; fireEvent.load(first);
    expect(socket.acks.map(ack => ack.command_id)).toEqual(['cmd1']);
    const generation = h.state().viewer.generation;
    if (leave) h.command(2, 'navigate');
    const before = socket.acks.length; h.command(3, 'show');
    expect(socket.acks).toHaveLength(before);
    await waitFor(() => expect(h.view.container.querySelector('img')).not.toBeNull());
    const second = h.view.container.querySelector('img')!;
    expect(second).not.toBe(first);
    h.dispatch({ type: 'viewer.rendered', id: 'photo', generation });
    expect(socket.acks).toHaveLength(before);
    fireEvent.load(second);
    expect(socket.acks[socket.acks.length - 1]?.command_id).toBe('cmd3');
    expect(socket.acks[socket.acks.length - 1]?.status).toBe('applied');
    expect(h.state().router.appliedSequence).toBe(3);
  });
}

it('loads and acknowledges the current photo after StrictMode effect replay', async () => {
  const h = harness(true);
  h.command(1, 'show');
  await waitFor(() => expect(h.view.container.querySelector('img')).not.toBeNull());
  fireEvent.load(h.view.container.querySelector('img')!);
  expect(socket.acks[socket.acks.length - 1]?.status).toBe('applied');
});

it('waits through processing and mounts a fresh image only after media becomes ready', async () => {
  vi.useFakeTimers();
  try {
    const probe = vi.fn().mockResolvedValueOnce(202).mockResolvedValue(200);
    const h = harness(false, probe);
    h.command(1, 'show');
    await act(async () => { await Promise.resolve(); });
    expect(h.state().viewer.status).toBe('processing');
    expect(h.view.container.querySelector('img')).toBeNull();
    expect(socket.acks).toHaveLength(0);
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    const image = h.view.container.querySelector('img');
    expect(image).not.toBeNull();
    expect(socket.acks).toHaveLength(0);
    fireEvent.load(image!);
    expect(socket.acks[0]?.status).toBe('applied');
  } finally { vi.useRealTimers(); }
});

it('reclassifies a failed image request and remounts after processing completes', async () => {
  vi.useFakeTimers();
  try {
    const probe = vi.fn().mockResolvedValueOnce(200).mockResolvedValueOnce(202).mockResolvedValue(200);
    const h = harness(false, probe);
    h.command(1, 'show');
    await act(async () => { await Promise.resolve(); });
    const first = h.view.container.querySelector('img')!;
    fireEvent.error(first);
    await act(async () => { await Promise.resolve(); });
    expect(h.state().viewer.status).toBe('processing');
    expect(socket.acks).toHaveLength(0);
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    const second = h.view.container.querySelector('img')!;
    expect(second).not.toBeNull();
    expect(second).not.toBe(first);
    fireEvent.load(second);
    expect(socket.acks[0]?.status).toBe('applied');
  } finally { vi.useRealTimers(); }
});

for (const leave of ['back', 'navigate'] as const) {
  it(`cannot acknowledge an abandoned show through a later same-photo visit after ${leave}`, async () => {
    const h = harness();
    h.dispatch({ type: 'router.navigate', route: { name: 'photos', collection: 'all' } });
    h.command(1, 'show');
    await waitFor(() => expect(h.view.container.querySelector('img')).not.toBeNull());
    if (leave === 'back') h.dispatch({ type: 'router.back' });
    else h.command(2, 'navigate');
    h.dispatch({ type: 'router.navigate', route: { name: 'dashboard' } });
    h.dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: 'photo' } });
    await waitFor(() => expect(h.view.container.querySelector('img')).not.toBeNull());
    fireEvent.load(h.view.container.querySelector('img')!);
    expect(socket.acks.filter(ack => ack.command_id === 'cmd1' && ack.status === 'applied')).toHaveLength(0);
    expect(h.state().router.route).toEqual({ name: 'photo', photoId: 'photo' });
  });
}
