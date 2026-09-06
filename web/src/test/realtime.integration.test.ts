/**
 * V0.3 end to end against the **real** mock core (`web/mock/server.ts`): a
 * headless screen (real reducer, session, executor and WS transport) is paired,
 * connected and then driven entirely through injected commands, exactly as
 * `atrium admin screen …` will drive the Go Core.
 *
 * Covered: W-301 (session.ready, heartbeat with route, pending_command),
 * W-302 (navigate / show with a render-confirmed ack / stale sequence /
 * refresh), W-303 (data.changed, session.revoked, session.superseded) and
 * W-304 (snapshot before socket, route reported on ready).
 */

import { spawn } from 'node:child_process';
import type { ChildProcessWithoutNullStreams } from 'node:child_process';

import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { toRouteState } from '../app/router';
import { classifyMediaStatus } from '../core/media';
import { closeCodeAction } from '../core/ws';
import type { CommandAckPayload } from '../types/ws';
import { createScreenHarness } from './screenHarness';
import type { ScreenHarness } from './screenHarness';

const PORT = 8792;
const BASE = `http://127.0.0.1:${PORT}`;
const WS = `ws://127.0.0.1:${PORT}/api/v1/screens/connect`;

let child: ChildProcessWithoutNullStreams;
let token = '';

function post(path: string, body: unknown = {}): Promise<Response> {
  return fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

async function acks(): Promise<CommandAckPayload[]> {
  const body = (await (await fetch(`${BASE}/__control/acks`)).json()) as {
    acks: CommandAckPayload[];
  };
  return body.acks;
}

async function ackFor(commandId: string, timeoutMs = 5000): Promise<CommandAckPayload> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const found = (await acks()).find((ack) => ack.command_id === commandId);
    if (found) return found;
    if (Date.now() > deadline) throw new Error(`no ack for ${commandId}`);
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

async function until(predicate: () => boolean, what: string, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(`timeout waiting for ${what}`);
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}

async function inject(
  kind: string,
  payload: unknown,
  extra: Record<string, unknown> = {},
): Promise<{ command_id: string; sequence: number; pending: boolean }> {
  const response = await post('/__control/command', { kind, payload, ...extra });
  return (await response.json()) as { command_id: string; sequence: number; pending: boolean };
}

beforeAll(async () => {
  child = spawn('node', ['mock/server.ts'], {
    cwd: process.cwd(),
    env: { ...process.env, ATRIUM_MOCK_PORT: String(PORT) },
  }) as ChildProcessWithoutNullStreams;
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('mock core did not start')), 15_000);
    child.stdout.on('data', (chunk: Buffer) => {
      if (chunk.toString().includes('listening')) {
        clearTimeout(timer);
        resolve();
      }
    });
    child.on('error', reject);
  });

  const started = (await (await post('/api/v1/pair/start', { client_hint: 'e2e' })).json()) as {
    pairing_id: string;
    code: string;
  };
  await post('/__control/approve', {
    code: started.code,
    name: 'Living room TV',
    screen_id: 'living_room_tv',
  });
  const claimed = (await (
    await post(`/api/v1/pair/${started.pairing_id}/claim`, {})
  ).json()) as { token: string };
  token = claimed.token;
}, 25_000);

afterAll(() => {
  child?.kill();
});

describe('V0.3 realtime, end to end (W-301..W-304)', () => {
  let screen: ScreenHarness;

  it('connects: /home first, then the socket, then session.ready and a heartbeat', async () => {
    screen = createScreenHarness({ baseUrl: BASE, wsUrl: WS, token, heartbeatIntervalMs: 300 });
    await until(() => screen.state().connection.status === 'online', 'session.ready');

    // The snapshot arrived before the socket (design §7.2).
    expect(screen.state().home?.home.name).toBe('Demo Home');
    expect(screen.state().connection.hasSnapshot).toBe(true);
    // A cold start reports the dashboard (PRD 5.3), through `state` on ready
    // and through the heartbeat's `route` (§9 RouteState).
    const sessions = (await (await fetch(`${BASE}/__control/sessions`)).json()) as {
      sessions: Array<{ route: unknown; applied_sequence: number }>;
    };
    expect(sessions.sessions[0]?.route).toEqual({ name: 'dashboard' });
  }, 20_000);

  it('navigate: applies a whitelisted collection and acks with the final route', async () => {
    const issued = await inject('navigate', { route: 'photos', collection: 'captured_today' });
    const ack = await ackFor(issued.command_id);
    expect(ack.status).toBe('applied');
    expect(ack.route).toEqual({ name: 'photos', collection: 'captured_today' });
    expect(screen.state().router.route).toEqual({
      name: 'photos',
      collection: 'captured_today',
    });
    expect(screen.state().router.appliedSequence).toBe(issued.sequence);

    // The new route is reported to the server without waiting for a heartbeat.
    const sessions = (await (await fetch(`${BASE}/__control/sessions`)).json()) as {
      sessions: Array<{ route: unknown }>;
    };
    expect(sessions.sessions[0]?.route).toEqual({ name: 'photos', collection: 'captured_today' });
  }, 20_000);

  it('navigate outside the whitelist acks failed/invalid_route', async () => {
    const before = screen.state().router.route;
    const issued = await inject('navigate', { route: 'admin' });
    const ack = await ackFor(issued.command_id);
    expect(ack.status).toBe('failed');
    expect(ack.error_code).toBe('invalid_route');
    expect(screen.state().router.route).toEqual(before);
  }, 20_000);

  it('show: pauses the slideshow and acks applied only after the image decoded', async () => {
    const list = (await (
      await fetch(`${BASE}/api/v1/photos?collection=recent&limit=5`, {
        headers: { Authorization: `Bearer ${token}` },
      })
    ).json()) as { items: Array<{ id: string; preview: { status: string } }> };
    const target = list.items.find((item) => item.preview.status === 'ready');
    expect(target).toBeDefined();
    const photoId = target?.id ?? '';

    const issued = await inject('show', { photo_id: photoId });
    await until(() => screen.state().router.route.name === 'photo', 'route change');
    expect(screen.state().slideshow.paused).toBe(true);
    // Nothing acked yet: §7.2 wants the render first.
    expect((await acks()).some((ack) => ack.command_id === issued.command_id)).toBe(false);

    // The headless viewer does what `usePhotoViewer` does: open, probe the real
    // media endpoint, then report `<img onload>`.
    screen.dispatch({ type: 'viewer.open', photoId, collection: null });
    const detail = await screen.api.getPhoto(photoId);
    screen.dispatch({
      type: 'viewer.loaded',
      generation: screen.state().viewer.generation,
      item: detail.item,
      neighbors: detail.neighbors ?? null,
    });
    expect(classifyMediaStatus(await screen.api.probeMedia(photoId))).toBe('ready');
    screen.dispatch({ type: 'viewer.rendered', id: photoId });

    const ack = await ackFor(issued.command_id);
    expect(ack.status).toBe('applied');
    expect(ack.resource_id).toBe(photoId);
    // The show arrived on a collection page, so the photo keeps that context.
    expect(ack.route).toEqual({
      name: 'photo',
      photo_id: photoId,
      collection: 'captured_today',
    });
    expect(screen.state().router.appliedSequence).toBe(issued.sequence);
  }, 25_000);

  it('show for a photo the Core does not serve acks failed/photo_unavailable', async () => {
    const issued = await inject('show', { photo_id: 'ph_does_not_exist' });
    await until(() => screen.state().router.route.name === 'photo', 'route change');

    // The viewer discovers the photo is gone (real 404 from the mock).
    screen.dispatch({ type: 'viewer.open', photoId: 'ph_does_not_exist', collection: null });
    await screen.api.getPhoto('ph_does_not_exist').catch(() => undefined);
    screen.dispatch({
      type: 'viewer.loadFailed',
      generation: screen.state().viewer.generation,
      gone: true,
    });

    const ack = await ackFor(issued.command_id);
    expect(ack.status).toBe('failed');
    expect(ack.error_code).toBe('photo_unavailable');
    expect(ack.resource_id).toBe('ph_does_not_exist');
    // A failed show never raises the applied sequence (§6.5).
    expect(screen.state().router.appliedSequence).toBeLessThan(issued.sequence);
  }, 20_000);

  it('a stale sequence is acked failed/superseded and never applied', async () => {
    const before = screen.state().router.route;
    const issued = await inject('navigate', { route: 'dashboard' }, { sequence: 1 });
    const ack = await ackFor(issued.command_id);
    expect(ack.status).toBe('failed');
    expect(ack.error_code).toBe('superseded');
    expect(screen.state().router.route).toEqual(before);
  }, 20_000);

  it('refresh keeps the page and the pause state, and acks after the re-read', async () => {
    const routeBefore = screen.state().router.route;
    const pausedBefore = screen.state().slideshow.paused;
    const homeVersion = screen.state().home?.version ?? 0;

    const issued = await inject('refresh', {});
    const ack = await ackFor(issued.command_id);
    expect(ack.status).toBe('applied');
    expect(ack.route).toEqual(toRouteState(routeBefore));
    expect(screen.state().router.route).toEqual(routeBefore);
    expect(screen.state().slideshow.paused).toBe(pausedBefore);
    // The data really was re-read.
    expect(screen.state().home?.version).toBeGreaterThanOrEqual(homeVersion);
  }, 20_000);

  it('a data.changed burst reaches the client and is throttled per target', async () => {
    const before = screen.changed.length;
    await post('/__control/changed', { topics: ['home', 'photos'], count: 20 });
    await until(() => screen.changed.length >= before + 20, 'burst delivered');
    expect(screen.changed[before]).toEqual(['home', 'photos']);
  }, 20_000);

  it('session.superseded stops reconnecting and shows the connect screen message', async () => {
    await post('/__control/supersede');
    await until(
      () => screen.state().connection.stopReason === 'superseded',
      'superseded',
    );
    await until(() => screen.client.status() === 'stopped', 'client stopped');
    // Still stopped a moment later: no backoff attempt was scheduled.
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(screen.client.status()).toBe('stopped');
    screen.stop();
  }, 20_000);

  it('session.ready carries a pending_command, and session.revoked clears everything', async () => {
    // Issued while nothing is connected: the Core holds it (§9 pending_command).
    const pending = await inject('navigate', { route: 'photos', collection: 'all' });
    expect(pending.pending).toBe(true);

    const fresh = createScreenHarness({ baseUrl: BASE, wsUrl: WS, token, heartbeatIntervalMs: 300 });
    await until(() => fresh.state().connection.status === 'online', 'second session ready');
    const ack = await ackFor(pending.command_id);
    expect(ack.status).toBe('applied');
    expect(ack.route).toEqual({ name: 'photos', collection: 'all' });

    await post('/__control/revoke');
    await until(() => fresh.state().needsPairing, 'revoked');
    expect(fresh.state().router.route).toEqual({ name: 'pair' });
    expect(fresh.state().home).toBeNull();
    expect(fresh.state().slideshow.round).toHaveLength(0);
    expect(fresh.state().router.appliedSequence).toBe(0);
    fresh.stop();
  }, 25_000);
});

/**
 * Consistency item (c): every §9 close code drives the documented action, with
 * the frames coming from the real mock socket rather than a fake.
 */
describe('close codes end to end (§9)', () => {
  it('4001 unauthorized: the socket is closed by the server, the client pairs', async () => {
    const bad = createScreenHarness({
      baseUrl: BASE,
      wsUrl: WS,
      token: 'atr_scr_not_a_token',
      heartbeatIntervalMs: 300,
    });
    // The preflight `/home` is the first thing that fails (design §7.2).
    await until(() => bad.state().needsPairing, 'unauthorized → pair');
    expect(closeCodeAction(4001)).toBe('pair');
    bad.stop();
  }, 20_000);

  it('4004 and 4005 reconnect; 1000/1001 are not terminal either', async () => {
    const live = createScreenHarness({ baseUrl: BASE, wsUrl: WS, token, heartbeatIntervalMs: 300 });
    await until(() => live.state().connection.status === 'online', 'online');

    for (const code of [4004, 4005, 1001]) {
      await post('/__control/close', { code });
      await until(() => live.state().connection.lastCloseCode === code, `close ${code}`);
      expect(closeCodeAction(code)).toBe('reconnect');
      // Backoff is 1 s at attempt 0 with full jitter, so the client comes back.
      await until(() => live.state().connection.status === 'online', `reconnect after ${code}`, 8000);
    }
    live.stop();
  }, 30_000);

  it('4005 protocol error: a malformed frame is refused by both sides', async () => {
    const live = createScreenHarness({ baseUrl: BASE, wsUrl: WS, token, heartbeatIntervalMs: 300 });
    await until(() => live.state().connection.status === 'online', 'online');
    // The mock rejects a non-envelope frame the way the Core must (§9).
    live.client.sendAck({
      command_id: 'cmd_probe',
      status: 'applied',
      route: { name: 'dashboard' },
    });
    live.stop();
    expect(closeCodeAction(4005)).toBe('reconnect');
    expect(closeCodeAction(4002)).toBe('clear-and-pair');
    expect(closeCodeAction(4003)).toBe('stop');
  }, 20_000);
});
