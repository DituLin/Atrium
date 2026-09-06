/**
 * Mock Atrium Core for web development (W-004).
 *
 *   npm run dev:mock      # http://127.0.0.1:8788
 *   npm run dev           # Vite proxies /api and /health here
 *
 * Implements the §8 routes the client needs, the §9 socket, and a `/__control`
 * hook that stands in for `atrium admin` (approve a pairing, push a command,
 * publish a change). Development only; never part of the bundle.
 */

import { createServer } from 'node:http';
import type { IncomingMessage, ServerResponse } from 'node:http';

import { WebSocketServer } from 'ws';
import type { WebSocket } from 'ws';

import { collectionPage, neighborsIn } from './collections.ts';
import { placeholderSvg, PHOTOS } from './data.ts';
import {
  applyOverrides,
  approve,
  bumpHomeVersion,
  claim,
  findByCode,
  homeSnapshot,
  nasStatus,
  pairings,
  POLL_INTERVAL_MS,
  screenForToken,
  startPairing,
} from './core.ts';
import type { Screen } from './core.ts';

const PORT = Number(process.env.ATRIUM_MOCK_PORT ?? 8788);
const HOST = process.env.ATRIUM_MOCK_HOST ?? '127.0.0.1';

function json(res: ServerResponse, status: number, body: unknown, headers: string[][] = []): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
    ...Object.fromEntries(headers),
  });
  res.end(payload);
}

function fail(res: ServerResponse, status: number, code: string, message = code): void {
  json(res, status, { error: { code, message } });
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let data = '';
    req.on('data', (chunk) => {
      data += chunk;
    });
    req.on('end', () => resolve(data));
  });
}

function cookieToken(req: IncomingMessage): string | null {
  const header = req.headers.cookie;
  if (!header) return null;
  for (const part of header.split(';')) {
    const [name, ...rest] = part.trim().split('=');
    if (name === 'atrium_screen') return rest.join('=');
  }
  return null;
}

function bearerToken(req: IncomingMessage): string | null {
  const header = req.headers.authorization;
  if (!header || !header.startsWith('Bearer ')) return null;
  return header.slice('Bearer '.length);
}

export function authenticate(req: IncomingMessage): Screen | null {
  return screenForToken(bearerToken(req) ?? cookieToken(req));
}

/* -------------------------------------------------------- media injection */

/**
 * Per-photo media overrides used to exercise the client's `202 / 404 / 410 /
 * 503` rules (design §7.3) without a real preview pipeline:
 *
 *   POST /__control/media {"id": "ph_03", "status": 202, "times": 2}
 *
 * `times` counts down, so an id can answer `202` twice and then serve bytes.
 * Omitting `times` keeps the override until it is cleared with `status: 200`.
 */
interface MediaOverride {
  status: number;
  times: number | null;
}

const mediaOverrides = new Map<string, MediaOverride>();

function takeOverride(id: string): number | null {
  const override = mediaOverrides.get(id);
  if (!override) return null;
  if (override.times !== null) {
    override.times -= 1;
    if (override.times <= 0) mediaOverrides.delete(id);
  }
  return override.status;
}

/* --------------------------------------------------------------- sessions */

interface Session {
  socket: WebSocket;
  screenId: string;
  lastHeartbeat: string | null;
  /** Latest `route` seen on a heartbeat or a `state` message (design §6.5). */
  route: unknown;
  appliedSequence: number;
}

const sessions = new Set<Session>();
let sequence = 0;
let messageCounter = 0;

/** Every `command.ack` the client sent, newest last (`/__control/acks`). */
const acks: unknown[] = [];
/** Commands that were issued while no session was connected (§9 pending). */
let pendingCommand: unknown = null;

function envelope(type: string, payload: unknown): string {
  messageCounter += 1;
  return JSON.stringify({
    schema_version: 1,
    type,
    id: `msg_mock_${messageCounter}`,
    sent_at: new Date().toISOString(),
    payload,
  });
}

function broadcast(type: string, payload: unknown): number {
  let sent = 0;
  for (const session of sessions) {
    session.socket.send(envelope(type, payload));
    sent += 1;
  }
  return sent;
}

/**
 * Injects a command. `sequence` may be forced (to replay a stale one) and
 * `ttl_ms` sets `expires_at`; the default is the PRD's 10 s. When no session is
 * connected the command is held and delivered inside the next `session.ready`,
 * which is how the real Core exercises `pending_command`.
 */
export function pushCommand(
  kind: string,
  payload: unknown,
  options: { sequence?: number; ttlMs?: number; commandId?: string } = {},
): { command_id: string; sequence: number; delivered: number; pending: boolean } {
  const next = options.sequence ?? sequence + 1;
  if (next > sequence) sequence = next;
  const ttl = options.ttlMs ?? 10_000;
  const command = {
    command_id: options.commandId ?? `cmd_${messageCounter}_${next}`,
    sequence: next,
    kind,
    payload,
    issued_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + ttl).toISOString(),
  };
  const delivered = broadcast('screen.command', command);
  if (delivered === 0) pendingCommand = command;
  return { command_id: command.command_id, sequence: next, delivered, pending: delivered === 0 };
}

/* ------------------------------------------------------------------ HTTP */

async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const url = new URL(req.url ?? '/', `http://${req.headers.host ?? 'localhost'}`);
  const path = url.pathname;
  const method = req.method ?? 'GET';

  if (path === '/health/live') return json(res, 200, { status: 'ok' });
  if (path === '/health/ready') return json(res, 200, { status: 'ok' });

  /* ---- control hook (stands in for `atrium admin`) ---- */
  if (path.startsWith('/__control/')) {
    const body = method === 'POST' ? JSON.parse((await readBody(req)) || '{}') : {};
    switch (path) {
      case '/__control/pairings':
        return json(res, 200, { pairings: [...pairings.values()] });
      case '/__control/approve': {
        const pairing = body.code
          ? findByCode(String(body.code))
          : pairings.get(String(body.pairing_id ?? ''));
        if (!pairing) return fail(res, 404, 'not_found', 'no such pairing');
        approve(pairing, body.name, body.screen_id);
        return json(res, 200, { pairing_id: pairing.pairing_id, status: pairing.status });
      }
      case '/__control/command':
        return json(
          res,
          202,
          pushCommand(String(body.kind ?? 'refresh'), body.payload ?? {}, {
            sequence: body.sequence === undefined ? undefined : Number(body.sequence),
            ttlMs: body.ttl_ms === undefined ? undefined : Number(body.ttl_ms),
            commandId: body.command_id === undefined ? undefined : String(body.command_id),
          }),
        );
      case '/__control/acks':
        return json(res, 200, { acks });
      case '/__control/changed': {
        // `count` replays the same notification so the client's per-target
        // throttle can be exercised with a burst (W-205 / W-303).
        const count = Math.max(1, Number(body.count ?? 1));
        let delivered = 0;
        for (let i = 0; i < count; i += 1) {
          delivered += broadcast('data.changed', {
            topics: body.topics ?? ['home', 'photos'],
            version: bumpHomeVersion(),
          });
        }
        return json(res, 200, { delivered, count });
      }
      case '/__control/revoke': {
        let closed = 0;
        for (const session of [...sessions]) {
          session.socket.send(envelope('session.revoked', { reason: 'revoked_by_operator' }));
          session.socket.close(4002, 'revoked');
          closed += 1;
        }
        return json(res, 200, { closed });
      }
      case '/__control/close': {
        // Any §9 close code on demand, so the client's policy table can be
        // exercised end to end (4004 queue overflow, 1001 shutdown, ...).
        const code = Number(body.code ?? 1001);
        let closed = 0;
        for (const session of [...sessions]) {
          session.socket.close(code, String(body.reason ?? 'control'));
          closed += 1;
        }
        return json(res, 200, { closed, code });
      }
      case '/__control/supersede': {
        let closed = 0;
        for (const session of [...sessions]) {
          session.socket.send(envelope('session.superseded', {}));
          session.socket.close(4003, 'superseded');
          closed += 1;
        }
        return json(res, 200, { closed });
      }
      case '/__control/media': {
        const id = String(body.id ?? '');
        const status = Number(body.status ?? 200);
        if (!id) return fail(res, 400, 'invalid_request', 'id is required');
        if (status === 200) mediaOverrides.delete(id);
        else {
          mediaOverrides.set(id, {
            status,
            times: body.times === undefined ? null : Number(body.times),
          });
        }
        return json(res, 200, { id, status, overrides: mediaOverrides.size });
      }
      case '/__control/state':
        return json(res, 200, { overrides: applyOverrides(body as Record<string, unknown>) });
      case '/__control/sessions':
        return json(res, 200, {
          sessions: [...sessions].map((s) => ({
            screen_id: s.screenId,
            last_heartbeat: s.lastHeartbeat,
            route: s.route,
            applied_sequence: s.appliedSequence,
          })),
        });
      default:
        return fail(res, 404, 'not_found');
    }
  }

  /* ---- pairing (no credential) ---- */
  if (path === '/api/v1/pair/start' && method === 'POST') {
    const pairing = startPairing();
    return json(res, 201, {
      pairing_id: pairing.pairing_id,
      code: pairing.code,
      expires_at: pairing.expires_at,
      poll_interval_ms: POLL_INTERVAL_MS,
    });
  }
  const pairMatch = /^\/api\/v1\/pair\/([^/]+)(\/claim)?$/.exec(path);
  if (pairMatch) {
    const pairing = pairings.get(decodeURIComponent(pairMatch[1] ?? ''));
    if (!pairing) return fail(res, 404, 'not_found', 'unknown pairing');
    if (Date.parse(pairing.expires_at) < Date.now() && pairing.status === 'pending') {
      pairing.status = 'expired';
    }
    if (!pairMatch[2]) {
      return json(res, 200, {
        pairing_id: pairing.pairing_id,
        status: pairing.status,
        expires_at: pairing.expires_at,
      });
    }
    if (method !== 'POST') return fail(res, 405, 'invalid_request');
    if (pairing.status === 'claimed') return fail(res, 410, 'pairing_claimed');
    if (pairing.status !== 'approved') return fail(res, 409, 'conflict', 'not approved yet');
    const screen = claim(pairing);
    return json(
      res,
      200,
      { screen_id: screen.id, name: screen.name, token: screen.token },
      [
        [
          'Set-Cookie',
          `atrium_screen=${screen.token}; HttpOnly; SameSite=Strict; Path=/; Max-Age=31536000`,
        ],
      ],
    );
  }

  /* ---- authenticated routes ---- */
  if (path.startsWith('/api/v1/')) {
    const screen = authenticate(req);
    if (!screen) return fail(res, 401, 'unauthorized', 'no screen credential');

    if (path === '/api/v1/home') return json(res, 200, homeSnapshot());
    if (path === '/api/v1/nas/status') return json(res, 200, nasStatus());
    if (path === '/api/v1/screens/me') {
      return json(res, 200, { id: screen.id, name: screen.name, status: 'active' });
    }
    if (path === '/api/v1/photos') {
      const collection = url.searchParams.get('collection') ?? 'recent';
      const limit = Math.min(100, Number(url.searchParams.get('limit') ?? 50) || 50);
      const page = collectionPage(
        collection,
        url.searchParams.get('cursor'),
        limit,
        url.searchParams.get('seed'),
      );
      return json(res, 200, {
        items: page.items.map(toDto),
        next_cursor: page.nextCursor,
        meta: page.meta,
      });
    }
    const mediaMatch = /^\/api\/v1\/media\/photos\/([^/]+)$/.exec(path);
    if (mediaMatch) {
      const id = decodeURIComponent(mediaMatch[1] ?? '');
      const photo = PHOTOS.find((p) => p.id === id);
      if (!photo) return fail(res, 404, 'not_found');
      const override = takeOverride(id);
      if (override === 202) {
        return json(res, 202, { error: { code: 'preview_processing', message: 'processing' } }, [
          ['Retry-After', '5'],
        ]);
      }
      if (override === 404) return fail(res, 404, 'not_found');
      if (override === 410) return fail(res, 410, 'not_found', 'photo removed');
      if (override === 503) return fail(res, 503, 'preview_unavailable');
      if (photo.preview_status !== 'ready') {
        return json(res, 202, { error: { code: 'preview_processing', message: 'processing' } }, [
          ['Retry-After', '5'],
        ]);
      }
      const body = placeholderSvg(photo, url.searchParams.get('variant') ?? 'preview');
      res.writeHead(200, {
        'Content-Type': 'image/svg+xml',
        'Content-Length': body.byteLength,
        ETag: `"${photo.id}"`,
      });
      return void res.end(body);
    }
    const photoMatch = /^\/api\/v1\/photos\/([^/]+)$/.exec(path);
    if (photoMatch) {
      const id = decodeURIComponent(photoMatch[1] ?? '');
      const photo = PHOTOS.find((p) => p.id === id);
      if (!photo) return fail(res, 404, 'not_found');
      const neighbors = url.searchParams.get('neighbors');
      return json(res, 200, {
        item: toDto(photo),
        ...(neighbors ? { neighbors: neighborsIn(neighbors, id) } : {}),
      });
    }
    return fail(res, 404, 'not_found');
  }

  return fail(res, 404, 'not_found');
}

function toDto(photo: (typeof PHOTOS)[number]): unknown {
  return {
    id: photo.id,
    source_id: 'family_photos',
    captured_at: photo.captured_at,
    captured_confidence: photo.captured_confidence,
    first_seen_at: photo.first_seen_at,
    is_baseline: photo.is_baseline,
    width: photo.width,
    height: photo.height,
    preview: { status: photo.preview_status, width: 1280, height: 960 },
    urls: {
      preview: `/api/v1/media/photos/${photo.id}?variant=preview`,
      thumb: `/api/v1/media/photos/${photo.id}?variant=thumb`,
    },
  };
}

/* -------------------------------------------------------------- bootstrap */

const server = createServer((req, res) => {
  handle(req, res).catch((error: unknown) => {
    fail(res, 500, 'internal', error instanceof Error ? error.message : 'error');
  });
});

const wss = new WebSocketServer({ noServer: true });

server.on('upgrade', (req, socket, head) => {
  const url = new URL(req.url ?? '/', 'http://localhost');
  if (url.pathname !== '/api/v1/screens/connect') {
    socket.destroy();
    return;
  }
  const screen = authenticate(req);
  if (!screen) {
    // 4001 must be delivered on the socket, so complete the handshake first.
    wss.handleUpgrade(req, socket, head, (ws) => ws.close(4001, 'unauthorized'));
    return;
  }
  wss.handleUpgrade(req, socket, head, (ws) => {
    // One session per screen (§6.5): supersede the previous one.
    for (const existing of sessions) {
      if (existing.screenId === screen.id) {
        existing.socket.send(envelope('session.superseded', {}));
        existing.socket.close(4003, 'superseded');
      }
    }
    const session: Session = {
      socket: ws,
      screenId: screen.id,
      lastHeartbeat: null,
      route: null,
      appliedSequence: 0,
    };
    sessions.add(session);
    // The newest undelivered command rides along with `session.ready` (§9).
    const carried = pendingCommand;
    pendingCommand = null;
    ws.send(
      envelope('session.ready', {
        screen: { id: screen.id, name: screen.name },
        server_time: new Date().toISOString(),
        home_version: 1,
        ...(carried ? { pending_command: carried } : {}),
      }),
    );
    ws.on('message', (raw) => {
      const text = String(raw);
      // §9: frames larger than 64 KiB, or anything that is not an envelope,
      // are a protocol error.
      if (text.length > 64 * 1024) {
        ws.close(4005, 'message too large');
        return;
      }
      let message: { type?: string; payload?: Record<string, unknown> };
      try {
        message = JSON.parse(text) as { type?: string; payload?: Record<string, unknown> };
      } catch {
        ws.close(4005, 'protocol error');
        return;
      }
      if (typeof message !== 'object' || message === null || typeof message.type !== 'string') {
        ws.close(4005, 'protocol error');
        return;
      }
      if (message.type === 'heartbeat') {
        session.lastHeartbeat = new Date().toISOString();
        session.route = message.payload?.route ?? session.route;
        session.appliedSequence = Number(message.payload?.applied_sequence ?? 0);
        ws.send(envelope('heartbeat.ack', { server_time: new Date().toISOString() }));
        return;
      }
      if (message.type === 'state') {
        session.route = message.payload?.route ?? session.route;
        return;
      }
      if (message.type === 'command.ack') {
        acks.push(message.payload);
      }
    });
    ws.on('close', () => sessions.delete(session));
  });
});

server.listen(PORT, HOST, () => {
  process.stdout.write(`mock core listening on http://${HOST}:${PORT}\n`);
});
