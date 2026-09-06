/**
 * Slideshow state (W-201, design §7.3, PRD 5.2).
 *
 * Rules encoded here, all pure so they can be tested without a DOM:
 *  - the list is one `random` round: a seed produces a shuffle with no repeats,
 *    and when the round's `next_cursor` is null the round is over and the
 *    driver starts a new one with a fresh seed;
 *  - items whose `preview.status` is not `ready` never enter the round;
 *  - `202` counts a retry (max 3) and then skips for the round, `503` skips for
 *    the round, `404/410` drops the photo from the list entirely;
 *  - fewer than two playable photos means a fixed display: `advance` is a no-op
 *    and no new round is started.
 */

import type { PhotoItem } from '../types/api';
import { MEDIA_MAX_RETRIES } from '../core/media';

export type SlideshowStatus = 'idle' | 'loading' | 'playing' | 'empty' | 'error';

export const DEFAULT_SLIDESHOW_INTERVAL_MS = 30_000;
/** Photos per request (design §6.4: default 50, max 100). */
export const SLIDESHOW_PAGE_SIZE = 50;
/** Never let a server value make the TV flash. */
export const MIN_SLIDESHOW_INTERVAL_MS = 3000;

export interface SlideshowState {
  seed: string | null;
  round: readonly PhotoItem[];
  cursor: string | null;
  index: number;
  status: SlideshowStatus;
  intervalMs: number;
  paused: boolean;
  /** `202` retry counts, per photo id, reset with the round. */
  retries: Readonly<Record<string, number>>;
  /** Ids skipped for this round (503, or retries exhausted). */
  skipped: readonly string[];
  /** Id of the image the browser has actually decoded and shown. */
  displayedId: string | null;
  /** Set when the round ran out with no cursor left: the driver reseeds. */
  roundComplete: boolean;
  /** Bumped on every new round so in-flight loads can be discarded. */
  generation: number;
}

export const initialSlideshowState: SlideshowState = {
  seed: null,
  round: [],
  cursor: null,
  index: 0,
  status: 'idle',
  intervalMs: DEFAULT_SLIDESHOW_INTERVAL_MS,
  paused: false,
  retries: {},
  skipped: [],
  displayedId: null,
  roundComplete: false,
  generation: 0,
};

export type SlideshowAction =
  | { type: 'slideshow.configure'; intervalSeconds: number }
  | { type: 'slideshow.roundStarted'; seed: string }
  | {
      type: 'slideshow.pageLoaded';
      seed: string;
      items: readonly PhotoItem[];
      nextCursor: string | null;
    }
  | { type: 'slideshow.loadFailed' }
  | { type: 'slideshow.advance' }
  | { type: 'slideshow.displayed'; id: string }
  | { type: 'slideshow.mediaProcessing'; id: string }
  | { type: 'slideshow.mediaGone'; id: string }
  | { type: 'slideshow.mediaUnavailable'; id: string }
  | { type: 'slideshow.listRefreshed'; items: readonly PhotoItem[]; nextCursor: string | null }
  | { type: 'slideshow.pause' }
  | { type: 'slideshow.resume' }
  | { type: 'slideshow.reset' };

export function isReady(item: PhotoItem): boolean {
  return item.preview.status === 'ready';
}

/** First playable index at or after `from`, or -1. */
export function findPlayable(
  round: readonly PhotoItem[],
  skipped: readonly string[],
  from: number,
): number {
  for (let i = Math.max(0, from); i < round.length; i += 1) {
    const item = round[i];
    if (item && !skipped.includes(item.id)) return i;
  }
  return -1;
}

export function playableCount(state: SlideshowState): number {
  let count = 0;
  for (const item of state.round) {
    if (!state.skipped.includes(item.id)) count += 1;
  }
  return count;
}

export function currentSlide(state: SlideshowState): PhotoItem | null {
  return state.round[state.index] ?? null;
}

/**
 * The item to preload one interval ahead. It never wraps: a round shows every
 * photo at most once (PRD 5.2), and the item after the last one belongs to the
 * next seeded round, which does not exist yet.
 */
export function nextSlide(state: SlideshowState): PhotoItem | null {
  const next = findPlayable(state.round, state.skipped, state.index + 1);
  return next >= 0 ? (state.round[next] ?? null) : null;
}

/** True while a single photo (or none) is shown without advancing (PRD 5.2). */
export function isFixedDisplay(state: SlideshowState): boolean {
  return state.cursor === null && playableCount(state) < 2;
}

/* ---------------------------------------------------------------- reducer */

function withStatus(state: SlideshowState): SlideshowState {
  if (playableCount(state) > 0) return { ...state, status: 'playing' };
  return { ...state, status: state.cursor !== null ? 'loading' : 'empty' };
}

function startRound(state: SlideshowState, seed: string): SlideshowState {
  return {
    ...state,
    seed,
    round: [],
    cursor: null,
    index: 0,
    status: 'loading',
    retries: {},
    skipped: [],
    roundComplete: false,
    generation: state.generation + 1,
  };
}

function dropItem(state: SlideshowState, id: string): SlideshowState {
  const position = state.round.findIndex((item) => item.id === id);
  if (position < 0) return state;
  const round = state.round.filter((item) => item.id !== id);
  // Removing the current photo leaves the index on whatever followed it; at the
  // end of the round that is one past the last item, which reads as "no current
  // slide" and makes the next advance start a new round instead of repeating.
  const index = position < state.index ? state.index - 1 : state.index;
  const retries = { ...state.retries };
  delete retries[id];
  return withStatus({
    ...state,
    round,
    retries,
    skipped: state.skipped.filter((skippedId) => skippedId !== id),
    index: round.length === 0 ? 0 : Math.min(Math.max(index, 0), round.length),
  });
}

function skipItem(state: SlideshowState, id: string): SlideshowState {
  if (state.skipped.includes(id)) return state;
  return withStatus({ ...state, skipped: [...state.skipped, id] });
}

export function slideshowReducer(
  state: SlideshowState,
  action: SlideshowAction,
): SlideshowState {
  switch (action.type) {
    case 'slideshow.configure': {
      const seconds = action.intervalSeconds;
      const intervalMs =
        Number.isFinite(seconds) && seconds > 0
          ? Math.max(MIN_SLIDESHOW_INTERVAL_MS, Math.round(seconds * 1000))
          : DEFAULT_SLIDESHOW_INTERVAL_MS;
      return intervalMs === state.intervalMs ? state : { ...state, intervalMs };
    }
    case 'slideshow.roundStarted':
      return startRound(state, action.seed);
    case 'slideshow.pageLoaded': {
      if (action.seed !== state.seed) return state;
      const known = new Set(state.round.map((item) => item.id));
      const added = action.items.filter((item) => isReady(item) && !known.has(item.id));
      return withStatus({
        ...state,
        round: added.length > 0 ? [...state.round, ...added] : state.round,
        cursor: action.nextCursor,
        roundComplete: false,
      });
    }
    case 'slideshow.loadFailed':
      return state.round.length > 0 ? state : { ...state, status: 'error' };
    case 'slideshow.advance': {
      if (state.paused) return state;
      const next = findPlayable(state.round, state.skipped, state.index + 1);
      if (next >= 0) return { ...state, index: next };
      // Nothing playable left in this round.
      if (state.cursor !== null) return state; // the driver is fetching a page
      if (playableCount(state) < 2) return state; // fixed display (PRD 5.2)
      return { ...state, roundComplete: true };
    }
    case 'slideshow.displayed':
      return state.displayedId === action.id ? state : { ...state, displayedId: action.id };
    case 'slideshow.mediaProcessing': {
      const attempts = (state.retries[action.id] ?? 0) + 1;
      const retries = { ...state.retries, [action.id]: attempts };
      const next = { ...state, retries };
      return attempts > MEDIA_MAX_RETRIES ? skipItem(next, action.id) : next;
    }
    case 'slideshow.mediaGone':
      return dropItem(state, action.id);
    case 'slideshow.mediaUnavailable':
      return skipItem(state, action.id);
    case 'slideshow.listRefreshed': {
      const current = currentSlide(state);
      const ready = action.items.filter(isReady);
      let round: readonly PhotoItem[] = ready;
      let index = current ? ready.findIndex((item) => item.id === current.id) : -1;
      if (current && index < 0) {
        // The current photo left the collection: keep showing it until the next
        // advance instead of cutting the image mid-interval (W-205).
        round = [current, ...ready];
        index = 0;
      }
      return withStatus({
        ...state,
        round,
        index: index < 0 ? 0 : index,
        cursor: action.nextCursor,
        roundComplete: false,
      });
    }
    case 'slideshow.pause':
      return state.paused ? state : { ...state, paused: true };
    case 'slideshow.resume':
      return state.paused ? { ...state, paused: false } : state;
    case 'slideshow.reset':
      return { ...initialSlideshowState, intervalMs: state.intervalMs };
    default:
      return state;
  }
}
