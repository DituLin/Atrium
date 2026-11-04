import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { MediaPreparation } from './mediaPreparation';

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

it('deduplicates one ID and gives processing previews only three delayed retries', async () => {
  const probe = vi.fn(async () => 202);
  const session = new MediaPreparation(probe);
  const first = session.prepare('a');
  expect(session.prepare('a')).toBe(first);
  await flush();
  for (let attempt = 0; attempt < 3; attempt++) { await vi.advanceTimersByTimeAsync(5000); }
  expect(await first).toBe('processing');
  expect(probe).toHaveBeenCalledTimes(4);
  await vi.advanceTimersByTimeAsync(60_000);
  expect(await session.prepare('a')).toBe('processing');
  expect(probe).toHaveBeenCalledTimes(4);
});

it('caps concurrent probes and cancels future retries for IDs outside the retained window', async () => {
  const finish: Array<(status: number) => void> = [];
  const probe = vi.fn(() => new Promise<number>(resolve => { finish.push(resolve); }));
  const session = new MediaPreparation(probe, 3);
  const tasks = ['a', 'b', 'c', 'd', 'e'].map(id => session.prepare(id));
  expect(probe).toHaveBeenCalledTimes(3);
  session.retain(['d', 'e']);
  for (const complete of finish.splice(0)) complete(202);
  await flush();
  expect(probe).toHaveBeenCalledTimes(5);
  session.cancel();
  for (const complete of finish.splice(0)) complete(202);
  await vi.advanceTimersByTimeAsync(20_000);
  expect(await Promise.all(tasks)).toEqual(['cancelled', 'cancelled', 'cancelled', 'cancelled', 'cancelled']);
  expect(probe).toHaveBeenCalledTimes(5);
});

it('releases cancelled stalled probe slots and ignores late fulfillment without exceeding concurrency', async () => {
  const completions = new Map<string, (status: number) => void>();
  const signals = new Map<string, AbortSignal | undefined>();
  const probe = vi.fn((id: string, signal?: AbortSignal) => {
    signals.set(id, signal);
    return new Promise<number>(resolve => { completions.set(id, resolve); });
  });
  const session = new MediaPreparation(probe, 3);
  const old = ['a', 'b', 'c'].map(id => session.prepare(id));
  session.retain(['d', 'e', 'f', 'g']);
  for (const id of ['d', 'e', 'f', 'g']) void session.prepare(id);
  await flush();
  expect(await Promise.all(old)).toEqual(['cancelled', 'cancelled', 'cancelled']);
  expect(probe.mock.calls.map(([id]) => id)).toEqual(['a', 'b', 'c', 'd', 'e', 'f']);
  expect(['a', 'b', 'c'].every(id => signals.get(id)?.aborted)).toBe(true);
  for (const id of ['a', 'b', 'c']) completions.get(id)!(200);
  await flush();
  expect(probe).toHaveBeenCalledTimes(6);
  completions.get('d')!(200); await flush();
  expect(probe.mock.calls.map(([id]) => id)).toEqual(['a', 'b', 'c', 'd', 'e', 'f', 'g']);
  session.cancel();
});

it('times out unresponsive requests even when the transport ignores abort', async () => {
  const probe = vi.fn(() => new Promise<number>(() => {}));
  const session = new MediaPreparation(probe);
  const first = session.prepare('a');
  await vi.advanceTimersByTimeAsync(10_000);
  expect(await first).toBe('error');
  expect(await session.prepare('a')).toBe('error');
  expect(probe).toHaveBeenCalledTimes(1);
  session.cancel();
});
