/**
 * Leading + trailing throttle used for `data.changed` refetches (design §7.3:
 * a burst of change notifications must collapse into at most one refetch per
 * second per topic set).
 */

export interface ThrottleClock {
  now: () => number;
  setTimeout: (fn: () => void, ms: number) => number;
  clearTimeout: (handle: number) => void;
}

export const defaultThrottleClock: ThrottleClock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => window.setTimeout(fn, ms),
  clearTimeout: (handle) => window.clearTimeout(handle),
};

export interface Throttled<A extends unknown[]> {
  (...args: A): void;
  cancel(): void;
  pending(): boolean;
}

export function createThrottle<A extends unknown[]>(
  intervalMs: number,
  fn: (...args: A) => void,
  clock: ThrottleClock = defaultThrottleClock,
): Throttled<A> {
  let lastRun = Number.NEGATIVE_INFINITY;
  let timer: number | null = null;
  let queued: A | null = null;

  const run = (args: A): void => {
    lastRun = clock.now();
    fn(...args);
  };

  const throttled = ((...args: A): void => {
    const elapsed = clock.now() - lastRun;
    if (elapsed >= intervalMs) {
      run(args);
      return;
    }
    queued = args;
    if (timer !== null) return;
    timer = clock.setTimeout(() => {
      timer = null;
      const next = queued;
      queued = null;
      if (next) run(next);
    }, intervalMs - elapsed);
  }) as Throttled<A>;

  throttled.cancel = () => {
    if (timer !== null) clock.clearTimeout(timer);
    timer = null;
    queued = null;
  };
  throttled.pending = () => timer !== null;

  return throttled;
}
