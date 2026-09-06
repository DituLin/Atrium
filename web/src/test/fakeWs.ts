/**
 * Test doubles for the WS transport: a scriptable socket and a deterministic
 * timer queue, so heartbeat cadence, reconnect backoff and message ordering are
 * observable without a network or real time.
 */

import type { SocketLike, WsTimers } from '../core/ws';
import type { ClientMessage, Envelope, ServerMessage } from '../types/ws';

export class FakeSocket implements SocketLike {
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

  /** Push a server frame, wrapped in the §9 envelope. */
  deliver<T extends ServerMessage>(type: T['type'], payload: T['payload']): void {
    this.onmessage?.({
      data: JSON.stringify({
        schema_version: 1,
        type,
        id: `msg_${this.sent.length}_${type}`,
        sent_at: new Date(0).toISOString(),
        payload,
      }),
    });
  }

  /** Every frame the client sent, decoded. */
  frames(): Array<Envelope<ClientMessage['type'], unknown>> {
    return this.sent.map((raw) => JSON.parse(raw) as Envelope<ClientMessage['type'], unknown>);
  }

  /** Payloads of every frame of one client message type. */
  framesOf<P>(type: ClientMessage['type']): P[] {
    return this.frames()
      .filter((frame) => frame.type === type)
      .map((frame) => frame.payload as P);
  }
}

export interface FakeClock {
  timers: WsTimers;
  advance(ms: number): void;
  pending(): number;
}

export function fakeTimers(): FakeClock {
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
