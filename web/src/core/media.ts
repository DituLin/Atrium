/**
 * Media response classification (design §7.3, §8).
 *
 * `GET /api/v1/media/photos/{id}` answers with bytes or with one of three
 * "not now" states, and each one has a different rule:
 *
 *   200 / 206        → the image can be decoded
 *   202 processing   → retry after 5 s, at most 3 times, then skip the item
 *   404 / 410        → the photo is gone; drop it from the list entirely
 *   503 unavailable  → preview cannot be produced now; skip for this round
 *
 * Everything else (network error, 5xx) is treated as a transient error and the
 * caller skips the item for the round rather than hammering the core.
 */

export type MediaOutcome = 'ready' | 'processing' | 'gone' | 'unavailable' | 'error';

/** Retry delay for `202 processing`; the server also sends `Retry-After: 5`. */
export const MEDIA_RETRY_MS = 5000;

/** How many times a `202` is retried before the item is skipped. */
export const MEDIA_MAX_RETRIES = 3;

export function classifyMediaStatus(status: number): MediaOutcome {
  if (status === 200 || status === 206 || status === 304) return 'ready';
  if (status === 202) return 'processing';
  if (status === 404 || status === 410) return 'gone';
  if (status === 503) return 'unavailable';
  return 'error';
}

/**
 * Retry delay honouring `Retry-After` when the server sent one, clamped so a
 * hostile or broken value cannot stall the slideshow for minutes.
 */
export function mediaRetryDelayMs(retryAfterMs: number | null): number {
  if (retryAfterMs === null || !Number.isFinite(retryAfterMs)) return MEDIA_RETRY_MS;
  return Math.min(Math.max(retryAfterMs, 1000), 30_000);
}
