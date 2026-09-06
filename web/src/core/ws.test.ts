import { describe, expect, it, vi } from 'vitest';

import { WS_CLOSE } from '../types/ws';
import type { RouteState } from '../types/ws';
import type { SocketLike, WsTimers } from './ws';
import { closeCodeAction, createWsClient, decodeEnvelope, encodeEnvelope, wsUrl } from './ws';

class FakeSocket implements SocketLike {
  onopen: ((event: unknown) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  readonly sent: string[] = [];
  closedWith: number | null = null;

  send(data: string): void {
    this.sent.push(data);
  }

  close(code?: number): void {
    this.closedWith = code ?? 1000;
    this.onclose?.({ code: this.closedWith });
  }

  open(): void {
    this.onopen?.({});
  }

  serverClose(code: number): void {
    this.onclose?.({ code });
  }
}

/** Deterministic timer queue so heartbeat cadence and backoff are observable. */
function fakeTimers() {
  let now = 0;
  const tasks = new Map<number, { at: number; fn: () => void }>();
  let nextHandle = 1;
  const timers: WsTimers = {
    now: () => now,
    setTimeout: (fn, ms) => {
      const handle = nextHandle++;
      tasks.set(handle, { at: now + ms, fn });
      return handle;
    },
    clearTimeout: (handle) => {
      tasks.delete(handle);
    },
    random: () => 0.5,
  };
  return {
    timers,
    advance(ms: number) {
      const target = now + ms;
      for (;;) {
        let due: [number, { at: number; fn: () => void }] | null = null;
        for (const entry of tasks.entries()) {
          if (entry[1].at <= target && (!due || entry[1].at < due[1].at)) due = entry;
        }
        if (!due) break;
        tasks.delete(due[0]);
        now = due[1].at;
        due[1].fn();
      }
      now = target;
    },
    pending: () => tasks.size,
  };
}

const ROUTE: RouteState = { name: 'dashboard' };

function makeClient(overrides: Partial<Parameters<typeof createWsClient>[0]> = {}) {
  const sockets: FakeSocket[] = [];
  const clock = fakeTimers();
  const messages: unknown[] = [];
  const closes: Array<{ code: number; action: string }> = [];
  const client = createWsClient({
    url: () => 'ws://core.test/api/v1/screens/connect',
    clientVersion: '0.1.0+test',
    getRoute: () => ROUTE,
    getAppliedSequence: () => 7,
    preflight: () => Promise.resolve(),
    onMessage: (m) => messages.push(m),
    onClosed: (code, action) => closes.push({ code, action }),
    socketFactory: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    timers: clock.timers,
    ...overrides,
  });
  return { client, sockets, clock, messages, closes };
}

describe('close code policy (design §7.2)', () => {
  it('maps every documented code', () => {
    expect(closeCodeAction(WS_CLOSE.unauthorized)).toBe('pair');
    expect(closeCodeAction(WS_CLOSE.revoked)).toBe('clear-and-pair');
    expect(closeCodeAction(WS_CLOSE.superseded)).toBe('stop');
    expect(closeCodeAction(WS_CLOSE.queueOverflow)).toBe('reconnect');
    expect(closeCodeAction(WS_CLOSE.protocolError)).toBe('reconnect');
    expect(closeCodeAction(WS_CLOSE.serverShutdown)).toBe('reconnect');
    expect(closeCodeAction(1006)).toBe('reconnect');
    expect(closeCodeAction(WS_CLOSE.normal)).toBe('idle');
  });
});

describe('envelope codec', () => {
  it('round-trips a heartbeat', () => {
    const raw = encodeEnvelope('heartbeat', { route: ROUTE, applied_sequence: 3, client_version: 'v' }, 0);
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    expect(parsed.schema_version).toBe(1);
    expect(parsed.type).toBe('heartbeat');
    expect(String(parsed.id)).toMatch(/^msg_/);
    expect(parsed.sent_at).toBe('1970-01-01T00:00:00.000Z');
  });

  it('rejects a wrong schema version and accepts unknown types as null', () => {
    expect(() => decodeEnvelope(JSON.stringify({ schema_version: 2, type: 'x', id: 'a' }))).toThrow();
    expect(decodeEnvelope(JSON.stringify({ schema_version: 1, type: 'future', id: 'a', payload: {} }))).toBeNull();
    const ok = decodeEnvelope(
      JSON.stringify({ schema_version: 1, type: 'heartbeat.ack', id: 'a', sent_at: '', payload: { server_time: 'x' } }),
    );
    expect(ok?.type).toBe('heartbeat.ack');
  });

  it('builds a wss URL for an https origin without any credential', () => {
    expect(wsUrl({ protocol: 'https:', host: 'core.test:8443' })).toBe(
      'wss://core.test:8443/api/v1/screens/connect',
    );
    expect(wsUrl({ protocol: 'http:', host: 'core.test' })).toContain('ws://');
  });
});

describe('ws client lifecycle', () => {
  it('calls /home before opening the socket', async () => {
    const preflight = vi.fn(() => Promise.resolve());
    const { client, sockets } = makeClient({ preflight });
    client.start();
    expect(sockets).toHaveLength(0);
    await Promise.resolve();
    expect(preflight).toHaveBeenCalledTimes(1);
    expect(sockets).toHaveLength(1);
  });

  it('heartbeats on open and then every 15 s', async () => {
    const { client, sockets, clock } = makeClient();
    client.start();
    await Promise.resolve();
    sockets[0]?.open();
    expect(sockets[0]?.sent).toHaveLength(1);
    clock.advance(15_000);
    clock.advance(15_000);
    expect(sockets[0]?.sent).toHaveLength(3);
    const beat = JSON.parse(sockets[0]?.sent[0] ?? '{}') as { payload: Record<string, unknown> };
    expect(beat.payload).toEqual({
      route: ROUTE,
      applied_sequence: 7,
      client_version: '0.1.0+test',
    });
  });

  it('stops reconnecting after 4003 superseded', async () => {
    const { client, sockets, clock, closes } = makeClient();
    client.start();
    await Promise.resolve();
    sockets[0]?.open();
    sockets[0]?.serverClose(WS_CLOSE.superseded);
    clock.advance(120_000);
    expect(closes[closes.length - 1]).toEqual({ code: 4003, action: 'stop' });
    expect(sockets).toHaveLength(1);
    expect(client.status()).toBe('stopped');
  });

  it('reconnects after 4004 with a jittered delay', async () => {
    const { client, sockets, clock } = makeClient();
    client.start();
    await Promise.resolve();
    sockets[0]?.open();
    sockets[0]?.serverClose(WS_CLOSE.queueOverflow);
    expect(client.status()).toBe('waiting');
    clock.advance(499);
    expect(sockets).toHaveLength(1);
    clock.advance(2);
    await Promise.resolve();
    expect(sockets).toHaveLength(2);
  });

  it('stops on a 401 from the preflight', async () => {
    const { client, sockets } = makeClient({
      preflight: () => Promise.reject(Object.assign(new Error('unauthorized'), { status: 401 })),
    });
    client.start();
    await Promise.resolve();
    await Promise.resolve();
    expect(sockets).toHaveLength(0);
    expect(client.status()).toBe('stopped');
  });

  it('closes with 4005 on a malformed frame', async () => {
    const { client, sockets } = makeClient();
    client.start();
    await Promise.resolve();
    sockets[0]?.open();
    sockets[0]?.onmessage?.({ data: 'not json' });
    expect(sockets[0]?.closedWith).toBe(WS_CLOSE.protocolError);
  });
});
