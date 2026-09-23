import { afterEach, expect, it, vi } from 'vitest';
import { ApiClient } from './api';
import { newMessageId, newRandomSeed } from './ids';
import { defaultWsTimers } from './ws';

// Chrome 66 has window, fetch and crypto, but no globalThis. Restore the
// binding before assertions/test-runner code so only application code is probed.
function withoutGlobalThis<T>(run: () => T): T {
  const root = globalThis;
  const descriptor = Object.getOwnPropertyDescriptor(root, 'globalThis')!;
  Object.defineProperty(root, 'globalThis', { value: undefined, configurable: true });
  try { return run(); }
  finally { Object.defineProperty(root, 'globalThis', descriptor); }
}

afterEach(() => vi.restoreAllMocks());

it('fetches home with cookies without globalThis', async () => {
  const response = new Response(JSON.stringify({ widgets: [] }), { status: 200 });
  const fetch = vi.spyOn(window, 'fetch').mockResolvedValue(response);
  const result = withoutGlobalThis(() => new ApiClient().getHome());
  await expect(result).resolves.toEqual({ widgets: [] });
  expect(fetch).toHaveBeenCalledWith('/api/v1/home', expect.objectContaining({ credentials: 'same-origin' }));
});

it('creates message IDs and random seeds without globalThis', () => {
  const result = withoutGlobalThis(() => [newMessageId(), newRandomSeed()]);
  expect(result[0]).toMatch(/^msg_[a-z0-9]+$/);
  expect(result[1]).toMatch(/^[a-z0-9]{12}$/);
});

it('schedules and cancels reconnect timers without globalThis', () => {
  const schedule = vi.spyOn(window, 'setTimeout').mockReturnValue(42 as unknown as ReturnType<typeof window.setTimeout>);
  const cancel = vi.spyOn(window, 'clearTimeout').mockImplementation(() => {});
  const callback = () => {};
  withoutGlobalThis(() => {
    const handle = defaultWsTimers.setTimeout(callback, 500);
    defaultWsTimers.clearTimeout(handle);
  });
  expect(schedule).toHaveBeenCalledWith(callback, 500);
  expect(cancel).toHaveBeenCalledWith(42);
});
