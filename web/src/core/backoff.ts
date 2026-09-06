/**
 * Exponential backoff with full jitter (PRD 5.3: base 1 s, cap 30 s).
 *
 *   delay = random(0, min(cap, base * 2^attempt))
 */

export const BACKOFF_BASE_MS = 1_000;
export const BACKOFF_MAX_MS = 30_000;

export interface BackoffOptions {
  baseMs?: number;
  maxMs?: number;
  random?: () => number;
}

/** Deterministic ceiling for an attempt (attempt 0 is the first retry). */
export function backoffCeiling(attempt: number, opts: BackoffOptions = {}): number {
  const base = opts.baseMs ?? BACKOFF_BASE_MS;
  const max = opts.maxMs ?? BACKOFF_MAX_MS;
  const safeAttempt = Math.max(0, Math.min(attempt, 31));
  return Math.min(max, base * 2 ** safeAttempt);
}

/** Actual delay to wait before retry number `attempt`. */
export function backoffDelay(attempt: number, opts: BackoffOptions = {}): number {
  const random = opts.random ?? Math.random;
  return Math.floor(random() * backoffCeiling(attempt, opts));
}
