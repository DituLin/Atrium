/**
 * WebSocket transport for `/api/v1/screens/connect` (design §9).
 *
 * Responsibilities kept here (transport only; the command executor is V0.3):
 *  - envelope codec `{schema_version, type, id, sent_at, payload}`
 *  - `GET /api/v1/home` before every socket open (auth + snapshot, design §7.2)
 *  - reconnect with exponential backoff + full jitter, 1 s → 30 s
 *  - close-code policy: 4001 → pair, 4002 → clear + pair, 4003 → stop,
 *    4004/4005 → reconnect
 *  - heartbeat every 15 s with `{route, applied_sequence, client_version}`
 *  - `state` on route change and `command.ack` passthrough
 *
 * Every timer, socket and clock is injectable so the whole lifecycle is
 * testable without a real network or real time.
 */

import type {
  ClientMessage,
  CommandAckPayload,
  Envelope,
  RouteState,
  ServerMessage,
} from '../types/ws';
import { WS_CLOSE, WS_SCHEMA_VERSION } from '../types/ws';
import { backoffDelay } from './backoff';
import { newMessageId } from './ids';

export const WS_PATH = '/api/v1/screens/connect';
export const HEARTBEAT_INTERVAL_MS = 15_000;
/** §9: messages larger than 64 KiB are a protocol error. */
export const MAX_MESSAGE_BYTES = 64 * 1024;

/* -------------------------------------------------------------- close codes */

/**
 * What the connection state machine must do after a socket closed.
 * `stop` means: do not reconnect, show "another session is active".
 */
export type CloseAction = 'reconnect' | 'pair' | 'clear-and-pair' | 'stop' | 'idle';

export function closeCodeAction(code: number): CloseAction {
  switch (code) {
    case WS_CLOSE.unauthorized: // 4001
      return 'pair';
    case WS_CLOSE.revoked: // 4002
      return 'clear-and-pair';
    case WS_CLOSE.superseded: // 4003
      return 'stop';
    case WS_CLOSE.queueOverflow: // 4004
    case WS_CLOSE.protocolError: // 4005
      return 'reconnect';
    case WS_CLOSE.normal: // 1000 — only we close normally, on stop()
      return 'idle';
    default:
      // 1001 server shutdown, 1006 abnormal, anything else: keep trying.
      return 'reconnect';
  }
}

/* ------------------------------------------------------------------ codec */

export function encodeEnvelope<T extends ClientMessage>(
  type: T['type'],
  payload: T['payload'],
  sentAtMs: number,
): string {
  const envelope: Envelope<T['type'], T['payload']> = {
    schema_version: WS_SCHEMA_VERSION,
    type,
    id: newMessageId(),
    sent_at: new Date(sentAtMs).toISOString(),
    payload,
  };
  return JSON.stringify(envelope);
}

export class ProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ProtocolError';
  }
}

const SERVER_MESSAGE_TYPES: ReadonlySet<string> = new Set([
  'session.ready',
  'screen.command',
  'data.changed',
  'heartbeat.ack',
  'session.revoked',
  'session.superseded',
]);

/**
 * Parses one inbound frame. Throws `ProtocolError` for anything that does not
 * match the envelope; unknown `type` values are returned as `null` so a future
 * server can add message types without breaking old screens.
 */
export function decodeEnvelope(raw: string): ServerMessage | null {
  if (raw.length > MAX_MESSAGE_BYTES) throw new ProtocolError('message too large');
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new ProtocolError('invalid JSON');
  }
  if (typeof parsed !== 'object' || parsed === null) throw new ProtocolError('not an object');
  const candidate = parsed as Partial<Envelope<string, unknown>>;
  if (candidate.schema_version !== WS_SCHEMA_VERSION) {
    throw new ProtocolError(`unsupported schema_version ${String(candidate.schema_version)}`);
  }
  if (typeof candidate.type !== 'string' || typeof candidate.id !== 'string') {
    throw new ProtocolError('missing type or id');
  }
  if (!SERVER_MESSAGE_TYPES.has(candidate.type)) return null;
  return candidate as ServerMessage;
}

/* ------------------------------------------------------------ socket seam */

export interface SocketLike {
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((event: unknown) => void) | null;
  onclose: ((event: { code: number; reason?: string }) => void) | null;
  onerror: ((event: unknown) => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
}

export type SocketFactory = (url: string) => SocketLike;

export function defaultSocketFactory(url: string): SocketLike {
  return new WebSocket(url) as unknown as SocketLike;
}

/** Absolute wss:// / ws:// URL for the current origin. No credential in it. */
export function wsUrl(location: { protocol: string; host: string } , path: string = WS_PATH): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${scheme}//${location.host}${path}`;
}

/* ------------------------------------------------------------------ client */

export type WsStatus = 'idle' | 'connecting' | 'open' | 'waiting' | 'stopped';

export interface WsTimers {
  now(): number;
  setTimeout(fn: () => void, ms: number): number;
  clearTimeout(handle: number): void;
  random(): number;
}

export const defaultWsTimers: WsTimers = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms) as unknown as number,
  clearTimeout: (handle) => {
    globalThis.clearTimeout(handle);
  },
  random: () => Math.random(),
};

export interface WsClientOptions {
  /** Resolves the socket URL lazily so tests need no `window.location`. */
  url: () => string;
  clientVersion: string;
  /** Current route, read on every heartbeat. */
  getRoute: () => RouteState;
  /** Highest applied command sequence, read on every heartbeat. */
  getAppliedSequence: () => number;
  /**
   * Called before every socket open. Must resolve only after a successful
   * `GET /api/v1/home` (design §7.2: snapshot before socket).
   * Reject with an `ApiError`-like `{status}` so 401 routes to pairing.
   */
  preflight: () => Promise<void>;
  onMessage: (message: ServerMessage) => void;
  onStatus?: (status: WsStatus) => void;
  /** Close code plus the policy decision from §7.2. */
  onClosed?: (code: number, action: CloseAction) => void;
  /** Preflight failure: `status` is the HTTP status when there was one. */
  onPreflightError?: (error: unknown, status: number | null) => void;
  socketFactory?: SocketFactory;
  timers?: WsTimers;
  heartbeatIntervalMs?: number;
}

export interface WsClient {
  start(): void;
  stop(): void;
  status(): WsStatus;
  /** Attempts since the last successful open; exposed for diagnostics/tests. */
  attempt(): number;
  sendState(route: RouteState): void;
  sendAck(payload: CommandAckPayload): void;
  /** Forces a heartbeat now (used by tests; the timer calls it normally). */
  sendHeartbeat(): void;
}

function errorStatus(error: unknown): number | null {
  if (typeof error === 'object' && error !== null && 'status' in error) {
    const status = (error as { status: unknown }).status;
    if (typeof status === 'number') return status;
  }
  return null;
}

export function createWsClient(options: WsClientOptions): WsClient {
  const timers = options.timers ?? defaultWsTimers;
  const socketFactory = options.socketFactory ?? defaultSocketFactory;
  const heartbeatMs = options.heartbeatIntervalMs ?? HEARTBEAT_INTERVAL_MS;

  let status: WsStatus = 'idle';
  let socket: SocketLike | null = null;
  let attempt = 0;
  let reconnectTimer: number | null = null;
  let heartbeatTimer: number | null = null;
  let running = false;
  /** Guards against a stale preflight resolving after stop() or a newer open. */
  let generation = 0;

  const setStatus = (next: WsStatus): void => {
    if (status === next) return;
    status = next;
    options.onStatus?.(next);
  };

  const clearReconnect = (): void => {
    if (reconnectTimer !== null) timers.clearTimeout(reconnectTimer);
    reconnectTimer = null;
  };

  const stopHeartbeat = (): void => {
    if (heartbeatTimer !== null) timers.clearTimeout(heartbeatTimer);
    heartbeatTimer = null;
  };

  const send = (message: string): void => {
    if (!socket) return;
    try {
      socket.send(message);
    } catch {
      // A dead socket surfaces through onclose; dropping the frame is correct.
    }
  };

  const sendHeartbeat = (): void => {
    send(
      encodeEnvelope<Extract<ClientMessage, { type: 'heartbeat' }>>(
        'heartbeat',
        {
          route: options.getRoute(),
          applied_sequence: options.getAppliedSequence(),
          client_version: options.clientVersion,
        },
        timers.now(),
      ),
    );
  };

  const scheduleHeartbeat = (): void => {
    stopHeartbeat();
    heartbeatTimer = timers.setTimeout(() => {
      heartbeatTimer = null;
      if (status !== 'open') return;
      sendHeartbeat();
      scheduleHeartbeat();
    }, heartbeatMs);
  };

  const scheduleReconnect = (): void => {
    if (!running) return;
    setStatus('waiting');
    const delay = backoffDelay(attempt, { random: timers.random });
    attempt += 1;
    clearReconnect();
    reconnectTimer = timers.setTimeout(() => {
      reconnectTimer = null;
      void connect();
    }, delay);
  };

  const teardownSocket = (): void => {
    if (!socket) return;
    socket.onopen = null;
    socket.onclose = null;
    socket.onerror = null;
    socket.onmessage = null;
    socket = null;
  };

  const handleClose = (code: number): void => {
    stopHeartbeat();
    teardownSocket();
    const action = closeCodeAction(code);
    options.onClosed?.(code, action);
    if (!running) {
      setStatus('idle');
      return;
    }
    if (action === 'stop' || action === 'pair' || action === 'clear-and-pair') {
      running = false;
      clearReconnect();
      setStatus('stopped');
      return;
    }
    scheduleReconnect();
  };

  const connect = async (): Promise<void> => {
    if (!running) return;
    const myGeneration = ++generation;
    setStatus('connecting');
    try {
      await options.preflight();
    } catch (error) {
      if (!running || myGeneration !== generation) return;
      const httpStatus = errorStatus(error);
      options.onPreflightError?.(error, httpStatus);
      if (httpStatus === 401) {
        // Credential is gone: the app routes to `pair`; stop burning retries.
        running = false;
        setStatus('stopped');
        return;
      }
      scheduleReconnect();
      return;
    }
    if (!running || myGeneration !== generation) return;

    let next: SocketLike;
    try {
      next = socketFactory(options.url());
    } catch {
      scheduleReconnect();
      return;
    }
    socket = next;
    next.onopen = () => {
      if (myGeneration !== generation) return;
      attempt = 0;
      setStatus('open');
      sendHeartbeat();
      scheduleHeartbeat();
    };
    next.onclose = (event) => {
      if (myGeneration !== generation) return;
      handleClose(typeof event.code === 'number' ? event.code : 1006);
    };
    next.onerror = () => {
      // `onclose` always follows; nothing to do but keep the handler defined so
      // engines that require it do not log an unhandled error.
    };
    next.onmessage = (event) => {
      if (myGeneration !== generation) return;
      if (typeof event.data !== 'string') return;
      let message: ServerMessage | null;
      try {
        message = decodeEnvelope(event.data);
      } catch {
        next.close(WS_CLOSE.protocolError, 'protocol error');
        return;
      }
      if (message) options.onMessage(message);
    };
  };

  return {
    start() {
      if (running) return;
      running = true;
      attempt = 0;
      void connect();
    },
    stop() {
      running = false;
      generation += 1;
      clearReconnect();
      stopHeartbeat();
      const current = socket;
      teardownSocket();
      current?.close(WS_CLOSE.normal, 'client stop');
      setStatus('idle');
    },
    status: () => status,
    attempt: () => attempt,
    sendState(route) {
      send(
        encodeEnvelope<Extract<ClientMessage, { type: 'state' }>>(
          'state',
          { route },
          timers.now(),
        ),
      );
    },
    sendAck(payload) {
      send(
        encodeEnvelope<Extract<ClientMessage, { type: 'command.ack' }>>(
          'command.ack',
          payload,
          timers.now(),
        ),
      );
    },
    sendHeartbeat,
  };
}
