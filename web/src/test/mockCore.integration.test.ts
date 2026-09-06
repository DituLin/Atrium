/**
 * Integration test against the **real** mock core (`web/mock/server.ts`), not a
 * fake: the process is spawned, pairing is driven end to end (start → approve
 * via the control hook → claim), `/api/v1/home` is fetched with the cookie the
 * claim set, and a real WebSocket session exchanges heartbeat / heartbeat.ack
 * and receives an injected `navigate` command.
 */

import { spawn } from 'node:child_process';
import type { ChildProcessWithoutNullStreams } from 'node:child_process';

import WebSocket from 'ws';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import {
  currentSlide,
  initialSlideshowState,
  playableCount,
  slideshowReducer,
} from '../app/slideshow';
import type { SlideshowState } from '../app/slideshow';
import { classifyMediaStatus } from '../core/media';
import { decodeEnvelope, encodeEnvelope } from '../core/ws';
import type { PhotoItem, PhotoListResponse } from '../types/api';
import type { ServerMessage } from '../types/ws';

const PORT = 8791;
const BASE = `http://127.0.0.1:${PORT}`;
// Vitest runs with `web/` as the working directory.
const webRoot = process.cwd();

let child: ChildProcessWithoutNullStreams;
let cookie = '';
let token = '';

function post(path: string, body: unknown, headers: Record<string, string> = {}): Promise<Response> {
  return fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...headers },
    body: JSON.stringify(body ?? {}),
  });
}

beforeAll(async () => {
  child = spawn('node', ['mock/server.ts'], {
    cwd: webRoot,
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
}, 20_000);

afterAll(() => {
  child?.kill();
});

describe('mock core: pairing', () => {
  it('rejects /home without a credential', async () => {
    const response = await fetch(`${BASE}/api/v1/home`);
    expect(response.status).toBe(401);
    const body = (await response.json()) as { error: { code: string } };
    expect(body.error.code).toBe('unauthorized');
  });

  it('runs start → approve → claim and then authenticates with the cookie', async () => {
    const started = (await (await post('/api/v1/pair/start', { client_hint: 'test' })).json()) as {
      pairing_id: string;
      code: string;
      poll_interval_ms: number;
    };
    expect(started.code).toMatch(/^\d{6}$/);
    expect(started.poll_interval_ms).toBe(3000);

    const pending = (await (await fetch(`${BASE}/api/v1/pair/${started.pairing_id}`)).json()) as {
      status: string;
    };
    expect(pending.status).toBe('pending');

    // Stands in for `atrium admin pair approve <code>`.
    const approved = await post('/__control/approve', {
      code: started.code,
      name: 'Living room TV',
      screen_id: 'living_room_tv',
    });
    expect(approved.status).toBe(200);

    const claimResponse = await post(`/api/v1/pair/${started.pairing_id}/claim`, {});
    expect(claimResponse.status).toBe(200);
    const setCookie = claimResponse.headers.get('set-cookie') ?? '';
    expect(setCookie).toContain('atrium_screen=');
    expect(setCookie).toContain('HttpOnly');
    expect(setCookie).toContain('SameSite=Strict');
    cookie = setCookie.split(';')[0] ?? '';
    const claimed = (await claimResponse.json()) as { screen_id: string; token: string };
    expect(claimed.screen_id).toBe('living_room_tv');
    token = claimed.token;
    expect(token.startsWith('atr_scr_')).toBe(true);

    // A second claim is gone (design §6.8 step 3).
    const second = await post(`/api/v1/pair/${started.pairing_id}/claim`, {});
    expect(second.status).toBe(410);

    const home = await fetch(`${BASE}/api/v1/home`, { headers: { cookie } });
    expect(home.status).toBe(200);
    const snapshot = (await home.json()) as {
      schema_version: number;
      home: { name: string };
      widgets: Array<{ type: string }>;
    };
    expect(snapshot.schema_version).toBe(1);
    expect(snapshot.home.name).toBe('Demo Home');
    expect(snapshot.widgets.map((w) => w.type)).toContain('clock');
  });

  it('serves a photo collection and a placeholder image', async () => {
    const list = (await (
      await fetch(`${BASE}/api/v1/photos?collection=recent&limit=5`, { headers: { cookie } })
    ).json()) as { items: Array<{ id: string }>; next_cursor: string | null };
    expect(list.items).toHaveLength(5);
    // Opaque keyset cursor (design §6.4), not an offset.
    expect(list.next_cursor).toMatch(/^[A-Za-z0-9_-]+$/);

    const media = await fetch(
      `${BASE}/api/v1/media/photos/${list.items[0]?.id ?? ''}?variant=thumb`,
      { headers: { cookie } },
    );
    expect(media.status).toBe(200);
    expect(media.headers.get('content-type')).toContain('image/');
  });
});

describe('mock core: websocket session', () => {
  it('exchanges session.ready, heartbeat / heartbeat.ack and a navigate command', async () => {
    const socket = new WebSocket(`ws://127.0.0.1:${PORT}/api/v1/screens/connect`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    const received: ServerMessage[] = [];
    const waiters: Array<{ type: string; resolve: (message: ServerMessage) => void }> = [];
    // The listener is attached before `open` resolves: `session.ready` is sent
    // by the server the moment the handshake completes.
    socket.on('message', (raw: WebSocket.RawData) => {
      const message = decodeEnvelope(String(raw));
      if (!message) return;
      received.push(message);
      for (let i = waiters.length - 1; i >= 0; i -= 1) {
        const waiter = waiters[i];
        if (waiter && waiter.type === message.type) {
          waiters.splice(i, 1);
          waiter.resolve(message);
        }
      }
    });
    const waitFor = (type: string, timeoutMs = 5000): Promise<ServerMessage> =>
      new Promise((resolve, reject) => {
        const existing = received.find((m) => m.type === type);
        if (existing) return resolve(existing);
        const timer = setTimeout(() => reject(new Error(`timeout waiting for ${type}`)), timeoutMs);
        waiters.push({
          type,
          resolve: (message) => {
            clearTimeout(timer);
            resolve(message);
          },
        });
      });

    await new Promise<void>((resolve, reject) => {
      socket.on('open', () => resolve());
      socket.on('error', reject);
    });

    const ready = await waitFor('session.ready');
    expect(ready.type).toBe('session.ready');
    if (ready.type === 'session.ready') {
      expect(ready.payload.screen.id).toBe('living_room_tv');
      expect(Number.isNaN(Date.parse(ready.payload.server_time))).toBe(false);
    }

    socket.send(
      encodeEnvelope(
        'heartbeat',
        { route: { name: 'dashboard' }, applied_sequence: 0, client_version: '0.1.0+test' },
        Date.now(),
      ),
    );
    const ack = await waitFor('heartbeat.ack');
    expect(ack.type).toBe('heartbeat.ack');

    const commandPromise = waitFor('screen.command');
    const issued = await post('/__control/command', {
      kind: 'navigate',
      payload: { route: 'photos', collection: 'captured_today' },
    });
    expect(issued.status).toBe(202);
    const command = await commandPromise;
    if (command.type === 'screen.command') {
      expect(command.payload.kind).toBe('navigate');
      expect(command.payload.sequence).toBeGreaterThan(0);
    }

    const changedPromise = waitFor('data.changed');
    await post('/__control/changed', { topics: ['home'] });
    const changed = await changedPromise;
    if (changed.type === 'data.changed') {
      expect(changed.payload.topics).toEqual(['home']);
    }

    socket.close(1000);
  }, 20_000);
});

/**
 * The V0.2 path end to end against the real mock: a seeded `random` round in
 * two pages, then the media rules of design §7.3 driven through the reducer
 * with responses the control hook injects.
 */
describe('mock core: slideshow round and media rules (W-201)', () => {
  const get = (path: string): Promise<Response> => fetch(`${BASE}${path}`, { headers: { cookie } });
  const page = async (path: string): Promise<PhotoListResponse> =>
    (await (await get(path)).json()) as PhotoListResponse;

  it('walks a seeded round in two pages without repeats and reseeds at the end', async () => {
    const first = await page('/api/v1/photos?collection=random&limit=20&seed=round-1');
    expect(first.items.length).toBe(20);
    expect(first.meta?.seed).toBe('round-1');
    expect(first.next_cursor).toBe('round-1:20');

    const second = await page(
      `/api/v1/photos?collection=random&limit=20&cursor=${encodeURIComponent(first.next_cursor ?? '')}`,
    );
    // End of the round: the client must start a new one with a new seed.
    expect(second.next_cursor).toBeNull();

    const ids = [...first.items, ...second.items].map((item) => item.id);
    expect(new Set(ids).size).toBe(ids.length);

    let state: SlideshowState = slideshowReducer(initialSlideshowState, {
      type: 'slideshow.roundStarted',
      seed: 'round-1',
    });
    for (const response of [first, second]) {
      state = slideshowReducer(state, {
        type: 'slideshow.pageLoaded',
        seed: 'round-1',
        items: response.items,
        nextCursor: response.next_cursor,
      });
    }
    // Items whose preview is not ready never enter the round.
    const notReady = [...first.items, ...second.items].filter(
      (item) => item.preview.status !== 'ready',
    );
    expect(notReady.length).toBeGreaterThan(0);
    expect(state.round.length).toBe(ids.length - notReady.length);
    expect(state.cursor).toBeNull();

    // Playing to the end of the round sets `roundComplete` exactly once.
    let guard = 0;
    while (!state.roundComplete && guard < 200) {
      state = slideshowReducer(state, { type: 'slideshow.advance' });
      guard += 1;
    }
    expect(state.roundComplete).toBe(true);
  });

  it('serves 200 for a ready preview, then an injected 202 and 404 as specified', async () => {
    const first = await page('/api/v1/photos?collection=random&limit=10&seed=media-round');
    let state = slideshowReducer(
      slideshowReducer(initialSlideshowState, {
        type: 'slideshow.roundStarted',
        seed: 'media-round',
      }),
      {
        type: 'slideshow.pageLoaded',
        seed: 'media-round',
        items: first.items,
        nextCursor: first.next_cursor,
      },
    );
    const current = currentSlide(state) as PhotoItem;
    const before = playableCount(state);

    const ok = await get(`/api/v1/media/photos/${current.id}?variant=preview`);
    expect(ok.status).toBe(200);
    expect(classifyMediaStatus(ok.status)).toBe('ready');

    // 202 processing: retried after 5 s, at most three times.
    await post('/__control/media', { id: current.id, status: 202, times: 1 });
    const processing = await get(`/api/v1/media/photos/${current.id}`);
    expect(processing.status).toBe(202);
    expect(processing.headers.get('retry-after')).toBe('5');
    state = slideshowReducer(state, { type: 'slideshow.mediaProcessing', id: current.id });
    expect(state.retries[current.id]).toBe(1);
    expect(state.skipped).not.toContain(current.id);
    // The override was consumed, so the retry gets the bytes.
    expect((await get(`/api/v1/media/photos/${current.id}`)).status).toBe(200);

    // 404: dropped from the list for good.
    const doomed = state.round[1] as PhotoItem;
    await post('/__control/media', { id: doomed.id, status: 404 });
    const gone = await get(`/api/v1/media/photos/${doomed.id}`);
    expect(gone.status).toBe(404);
    expect(classifyMediaStatus(gone.status)).toBe('gone');
    state = slideshowReducer(state, { type: 'slideshow.mediaGone', id: doomed.id });
    expect(state.round.map((item) => item.id)).not.toContain(doomed.id);
    expect(playableCount(state)).toBe(before - 1);
  });

  it('returns neighbours inside a collection for the viewer', async () => {
    const list = await page('/api/v1/photos?collection=recent&limit=5');
    const middle = list.items[1] as PhotoItem;
    const detail = (await (await get(`/api/v1/photos/${middle.id}?neighbors=recent`)).json()) as {
      item: PhotoItem;
      neighbors: { previous_id: string | null; next_id: string | null };
    };
    expect(detail.item.id).toBe(middle.id);
    expect(detail.neighbors.previous_id).toBe(list.items[0]?.id);
    expect(detail.neighbors.next_id).toBe(list.items[2]?.id);
  });
});
