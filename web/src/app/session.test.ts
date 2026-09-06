/**
 * W-301 / W-303 — the WS session driven through the real transport
 * (`core/ws.ts`) with a fake socket and a deterministic clock.
 */

import { describe, expect, it } from 'vitest';

import { FakeSocket, fakeTimers } from '../test/fakeWs';
import type { WsClient } from '../core/ws';
import { createWsClient } from '../core/ws';
import type {
  CommandAckPayload,
  DataChangedTopic,
  HeartbeatPayload,
  ScreenCommand,
  StatePayload,
} from '../types/ws';
import type { AppRoute } from './router';
import { createSession } from './session';
import { appReducer, createInitialState } from './state';
import type { AppAction, AppState } from './state';

const READY = {
  screen: { id: 'living_room_tv', name: 'Living room TV' },
  server_time: '2026-09-06T12:00:00.000Z',
  home_version: 1,
};

async function harness() {
  let state: AppState = createInitialState(0);
  const dispatch = (action: AppAction): void => {
    state = appReducer(state, action);
  };
  const sockets: FakeSocket[] = [];
  const clock = fakeTimers();
  const notified: DataChangedTopic[][] = [];
  const refreshed: AppRoute[] = [];
  let client: WsClient | null = null;

  const session = createSession({
    getState: () => state,
    dispatch,
    sendAck: (ack) => client?.sendAck(ack),
    sendState: (route) => client?.sendState(route),
    notifyChanged: (topics) => notified.push([...topics]),
    refreshRoute: (route) => {
      refreshed.push(route);
      return Promise.resolve();
    },
    now: () => clock.timers.now(),
  });

  client = createWsClient({
    url: () => 'ws://core.test/api/v1/screens/connect',
    clientVersion: '0.3.0+test',
    getRoute: () => ({ name: state.router.route.name }),
    getAppliedSequence: () => state.router.appliedSequence,
    preflight: () => Promise.resolve(),
    onMessage: session.handleMessage,
    socketFactory: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    timers: clock.timers,
  });
  client.start();
  // The preflight resolves on a microtask before the socket is created.
  await Promise.resolve();
  await Promise.resolve();
  const socket = sockets[0] as FakeSocket;
  socket.open();
  return { get: () => state, socket, clock, notified, refreshed, session, client };
}

describe('heartbeat cadence (W-301)', () => {
  it('sends one heartbeat on open and one every 15 s after that', async () => {
    const h = await harness();
    const beats = (): HeartbeatPayload[] => h.socket.framesOf<HeartbeatPayload>('heartbeat');
    expect(beats()).toHaveLength(1);
    h.clock.advance(14_999);
    expect(beats()).toHaveLength(1);
    h.clock.advance(1);
    expect(beats()).toHaveLength(2);
    h.clock.advance(45_000);
    expect(beats()).toHaveLength(5);
    expect(beats()[0]).toEqual({
      route: { name: 'dashboard' },
      applied_sequence: 0,
      client_version: '0.3.0+test',
    });
  });
});

const PENDING: ScreenCommand = {
  command_id: 'cmd_pending',
  sequence: 4,
  kind: 'navigate',
  payload: { route: 'photos', collection: 'captured_today' },
  issued_at: '2026-09-06T11:59:55Z',
  expires_at: '2026-09-06T12:00:05Z',
};

describe('session.ready (W-301)', () => {
  it('syncs the clock, goes online, reports the route, and applies pending_command', async () => {
    const h = await harness();
    h.socket.deliver('session.ready', { ...READY, pending_command: PENDING });

    expect(h.get().connection.status).toBe('online');
    // offset = server_time - fake now (0)
    expect(h.get().clock.offsetMs).toBe(Date.parse(READY.server_time));

    const reported = h.socket.framesOf<StatePayload>('state');
    expect(reported[0]).toEqual({ route: { name: 'dashboard' } });

    expect(h.get().router.route).toEqual({ name: 'photos', collection: 'captured_today' });
    expect(h.get().router.appliedSequence).toBe(4);
    const acks = h.socket.framesOf<CommandAckPayload>('command.ack');
    expect(acks).toEqual([
      {
        command_id: 'cmd_pending',
        status: 'applied',
        route: { name: 'photos', collection: 'captured_today' },
      },
    ]);
  });

  it('keeps the clock in sync from every heartbeat.ack', async () => {
    const h = await harness();
    h.socket.deliver('session.ready', READY);
    h.socket.deliver('heartbeat.ack', { server_time: '2026-09-06T12:00:30.000Z' });
    expect(h.get().clock.offsetMs).toBe(Date.parse('2026-09-06T12:00:30.000Z'));
  });
});

describe('change push and session termination (W-303)', () => {
  it('forwards data.changed topics to the throttled refetcher', async () => {
    const h = await harness();
    h.socket.deliver('session.ready', READY);
    h.socket.deliver('data.changed', { topics: ['home', 'photos'], version: 9 });
    h.socket.deliver('data.changed', { topics: ['nas'], version: 10 });
    expect(h.notified).toEqual([['home', 'photos'], ['nas']]);
  });

  it('session.revoked clears local state and lands on pair', async () => {
    const h = await harness();
    h.socket.deliver('session.ready', READY);
    h.socket.deliver('screen.command', {
      ...PENDING,
      command_id: 'cmd_nav',
      payload: { route: 'photos', collection: 'recent' },
    });
    expect(h.get().router.appliedSequence).toBe(4);

    h.socket.deliver('session.revoked', { reason: 'revoked_by_operator' });
    expect(h.get().needsPairing).toBe(true);
    expect(h.get().home).toBeNull();
    expect(h.get().slideshow.round).toHaveLength(0);
    expect(h.get().router.route).toEqual({ name: 'pair' });
    // The applied sequence goes with the identity.
    expect(h.get().router.appliedSequence).toBe(0);
  });

  it('session.superseded stops the client and explains itself on the connect screen', async () => {
    const h = await harness();
    h.socket.deliver('session.ready', READY);
    h.socket.deliver('session.superseded', {});
    expect(h.get().connection.stopReason).toBe('superseded');

    // The close frame follows; the transport must not reconnect after it.
    h.socket.serverClose(4003);
    expect(h.client.status()).toBe('stopped');
    expect(h.clock.pending()).toBe(0);
  });
});

describe('refresh over the socket (W-302)', () => {
  it('re-reads the current route and acks after the data came back', async () => {
    const h = await harness();
    h.socket.deliver('session.ready', READY);
    h.socket.deliver('screen.command', {
      ...PENDING,
      command_id: 'cmd_refresh',
      sequence: 6,
      kind: 'refresh',
      payload: {},
    });
    await Promise.resolve();
    await Promise.resolve();
    expect(h.refreshed).toEqual([{ name: 'dashboard' }]);
    const acks = h.socket.framesOf<CommandAckPayload>('command.ack');
    expect(acks).toEqual([
      { command_id: 'cmd_refresh', status: 'applied', route: { name: 'dashboard' } },
    ]);
  });
});
