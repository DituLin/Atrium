import { describe, expect, it, vi } from 'vitest';

import {
  AUTH_CACHE_MAX_AGE_MS,
  clearLastAuthOkAt,
  createAuthExpiryWatcher,
  isAuthCacheExpired,
  loadLastAuthOkAt,
  saveLastAuthOkAt,
} from './authExpiry';

describe('24 h authorization cache (PRD 8.2)', () => {
  it('is not expired exactly at the boundary and is expired one ms later', () => {
    const t0 = 1_000_000;
    expect(isAuthCacheExpired(t0, t0 + AUTH_CACHE_MAX_AGE_MS)).toBe(false);
    expect(isAuthCacheExpired(t0, t0 + AUTH_CACHE_MAX_AGE_MS + 1)).toBe(true);
  });

  it('treats "never authenticated" as expired', () => {
    expect(isAuthCacheExpired(null, 0)).toBe(true);
  });

  it('round-trips through localStorage and clears', () => {
    saveLastAuthOkAt(12345);
    expect(loadLastAuthOkAt()).toBe(12345);
    clearLastAuthOkAt();
    expect(loadLastAuthOkAt()).toBeNull();
  });

  it('survives a storage that throws', () => {
    const spy = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    expect(loadLastAuthOkAt()).toBeNull();
    spy.mockRestore();
  });

  it('fires onExpired on boot and on the interval', () => {
    vi.useFakeTimers();
    const onExpired = vi.fn();
    const onValid = vi.fn();
    let now = 0;
    let last: number | null = 0;
    const watcher = createAuthExpiryWatcher({
      now: () => now,
      load: () => last,
      intervalMs: 60_000,
      onExpired,
      onValid,
    });
    watcher.start();
    expect(onValid).toHaveBeenCalledTimes(1);
    now = AUTH_CACHE_MAX_AGE_MS + 1;
    vi.advanceTimersByTime(60_000);
    expect(onExpired).toHaveBeenCalledTimes(1);
    last = null;
    vi.advanceTimersByTime(60_000);
    expect(onExpired).toHaveBeenCalledTimes(2);
    watcher.stop();
    vi.advanceTimersByTime(600_000);
    expect(onExpired).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });
});
