import { describe, expect, it } from 'vitest';

import { BACKOFF_MAX_MS, backoffCeiling, backoffDelay } from './backoff';

describe('backoff', () => {
  it('doubles from 1 s and caps at 30 s', () => {
    expect(backoffCeiling(0)).toBe(1000);
    expect(backoffCeiling(1)).toBe(2000);
    expect(backoffCeiling(2)).toBe(4000);
    expect(backoffCeiling(3)).toBe(8000);
    expect(backoffCeiling(4)).toBe(16000);
    expect(backoffCeiling(5)).toBe(BACKOFF_MAX_MS);
    expect(backoffCeiling(50)).toBe(BACKOFF_MAX_MS);
  });

  it('applies full jitter: every delay is within [0, ceiling)', () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      const ceiling = backoffCeiling(attempt);
      for (const r of [0, 0.25, 0.5, 0.999999]) {
        const delay = backoffDelay(attempt, { random: () => r });
        expect(delay).toBeGreaterThanOrEqual(0);
        expect(delay).toBeLessThan(ceiling);
      }
    }
  });

  it('never returns a negative attempt or NaN', () => {
    expect(backoffDelay(-5, { random: () => 0.5 })).toBe(500);
  });
});
