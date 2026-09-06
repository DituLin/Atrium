/**
 * 24 h authorization cache rule (PRD 8.2, design §7.2).
 *
 * `lastAuthOkAt` is written on every successful authenticated response. When it
 * is older than 24 h — checked on boot, on `visibilitychange`, and every 60 s —
 * the client must purge in-memory photo lists, stop rendering images, and go to
 * the connect screen until a successful authenticated response arrives.
 */

import { readNumber, removeKey, writeNumber } from './storage';

export const AUTH_CACHE_MAX_AGE_MS = 24 * 60 * 60 * 1000;
export const AUTH_CHECK_INTERVAL_MS = 60 * 1000;

const KEY = 'last_auth_ok_at';

/** `null` (never authenticated) counts as expired: nothing may be rendered. */
export function isAuthCacheExpired(
  lastAuthOkAt: number | null,
  now: number,
  maxAgeMs: number = AUTH_CACHE_MAX_AGE_MS,
): boolean {
  if (lastAuthOkAt === null) return true;
  return now - lastAuthOkAt > maxAgeMs;
}

export function loadLastAuthOkAt(): number | null {
  return readNumber(KEY);
}

export function saveLastAuthOkAt(now: number): void {
  writeNumber(KEY, now);
}

export function clearLastAuthOkAt(): void {
  removeKey(KEY);
}

export interface AuthExpiryWatcherOptions {
  now?: () => number;
  intervalMs?: number;
  load?: () => number | null;
  onExpired: () => void;
  onValid?: () => void;
}

export interface AuthExpiryWatcher {
  check(): boolean;
  start(): void;
  stop(): void;
}

/**
 * Wires the three trigger points from the PRD. Kept out of React so the rule is
 * testable without a DOM tree.
 */
export function createAuthExpiryWatcher(options: AuthExpiryWatcherOptions): AuthExpiryWatcher {
  const now = options.now ?? (() => Date.now());
  const load = options.load ?? loadLastAuthOkAt;
  const intervalMs = options.intervalMs ?? AUTH_CHECK_INTERVAL_MS;
  let timer: number | null = null;
  let visibilityHandler: (() => void) | null = null;

  const check = (): boolean => {
    const expired = isAuthCacheExpired(load(), now());
    if (expired) options.onExpired();
    else options.onValid?.();
    return expired;
  };

  return {
    check,
    start() {
      check();
      if (timer === null && typeof window !== 'undefined') {
        timer = window.setInterval(check, intervalMs);
      }
      if (!visibilityHandler && typeof document !== 'undefined') {
        visibilityHandler = () => {
          if (document.visibilityState === 'visible') check();
        };
        document.addEventListener('visibilitychange', visibilityHandler);
      }
    },
    stop() {
      if (timer !== null && typeof window !== 'undefined') window.clearInterval(timer);
      timer = null;
      if (visibilityHandler && typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', visibilityHandler);
      }
      visibilityHandler = null;
    },
  };
}
