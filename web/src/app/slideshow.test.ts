import { describe, expect, it } from 'vitest';

import type { PhotoItem, PreviewStatus } from '../types/api';
import {
  DEFAULT_SLIDESHOW_INTERVAL_MS,
  MIN_SLIDESHOW_INTERVAL_MS,
  currentSlide,
  initialSlideshowState,
  isFixedDisplay,
  nextSlide,
  playableCount,
  slideshowReducer,
} from './slideshow';
import type { SlideshowAction, SlideshowState } from './slideshow';

function photo(id: string, status: PreviewStatus = 'ready'): PhotoItem {
  return {
    id,
    source_id: 'family_photos',
    captured_at: '2026-09-05T10:00:00+08:00',
    captured_confidence: 'exact',
    first_seen_at: '2026-09-05T02:00:00Z',
    is_baseline: false,
    width: 1600,
    height: 1200,
    preview: { status, width: 1280, height: 960 },
    urls: { preview: `/api/v1/media/photos/${id}?variant=preview`, thumb: '' },
  };
}

function run(state: SlideshowState, ...actions: SlideshowAction[]): SlideshowState {
  return actions.reduce(slideshowReducer, state);
}

/** A loaded round with the given ids and no further pages. */
function round(ids: string[], nextCursor: string | null = null): SlideshowState {
  return run(
    initialSlideshowState,
    { type: 'slideshow.roundStarted', seed: 'seed-1' },
    { type: 'slideshow.pageLoaded', seed: 'seed-1', items: ids.map((id) => photo(id)), nextCursor },
  );
}

describe('slideshow: rounds and seeds', () => {
  it('only accepts pages for the current seed', () => {
    const state = run(
      initialSlideshowState,
      { type: 'slideshow.roundStarted', seed: 'seed-2' },
      { type: 'slideshow.pageLoaded', seed: 'seed-1', items: [photo('a')], nextCursor: null },
    );
    expect(state.round).toHaveLength(0);
    expect(state.status).toBe('loading');
  });

  it('appends pages of the same round without duplicates', () => {
    const first = round(['a', 'b'], 'seed-1:2');
    const state = slideshowReducer(first, {
      type: 'slideshow.pageLoaded',
      seed: 'seed-1',
      items: [photo('b'), photo('c')],
      nextCursor: null,
    });
    expect(state.round.map((item) => item.id)).toEqual(['a', 'b', 'c']);
    expect(state.cursor).toBeNull();
  });

  it('drops items whose preview is not ready', () => {
    const state = run(
      initialSlideshowState,
      { type: 'slideshow.roundStarted', seed: 's' },
      {
        type: 'slideshow.pageLoaded',
        seed: 's',
        items: [photo('a'), photo('b', 'pending'), photo('c', 'failed')],
        nextCursor: null,
      },
    );
    expect(state.round.map((item) => item.id)).toEqual(['a']);
  });

  it('marks the round complete at the end and a new seed restarts it', () => {
    const played = run(round(['a', 'b']), { type: 'slideshow.advance' });
    expect(currentSlide(played)?.id).toBe('b');
    const ended = slideshowReducer(played, { type: 'slideshow.advance' });
    expect(ended.roundComplete).toBe(true);
    const restarted = slideshowReducer(ended, { type: 'slideshow.roundStarted', seed: 'seed-2' });
    expect(restarted.seed).toBe('seed-2');
    expect(restarted.round).toHaveLength(0);
    expect(restarted.roundComplete).toBe(false);
    expect(restarted.generation).toBe(played.generation + 1);
  });

  it('waits for the next page instead of reseeding while a cursor is open', () => {
    const state = slideshowReducer(round(['a'], 'seed-1:1'), { type: 'slideshow.advance' });
    expect(state.roundComplete).toBe(false);
    expect(state.index).toBe(0);
  });
});

describe('slideshow: media rules', () => {
  it('retries a 202 three times and then skips the item', () => {
    let state = round(['a', 'b']);
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      state = slideshowReducer(state, { type: 'slideshow.mediaProcessing', id: 'b' });
      expect(state.skipped).not.toContain('b');
      expect(state.retries.b).toBe(attempt);
    }
    state = slideshowReducer(state, { type: 'slideshow.mediaProcessing', id: 'b' });
    expect(state.skipped).toContain('b');
    expect(playableCount(state)).toBe(1);
  });

  it('drops a 404/410 photo from the list and keeps the index on the same item', () => {
    const state = run(round(['a', 'b', 'c']), { type: 'slideshow.advance' });
    expect(currentSlide(state)?.id).toBe('b');
    const dropped = slideshowReducer(state, { type: 'slideshow.mediaGone', id: 'a' });
    expect(dropped.round.map((item) => item.id)).toEqual(['b', 'c']);
    expect(currentSlide(dropped)?.id).toBe('b');
  });

  it('skips a 503 for the round only', () => {
    const state = slideshowReducer(round(['a', 'b']), {
      type: 'slideshow.mediaUnavailable',
      id: 'b',
    });
    expect(state.round).toHaveLength(2);
    expect(nextSlide(state)).toBeNull();
    expect(isFixedDisplay(state)).toBe(true);
  });
});

describe('slideshow: fixed display and pausing', () => {
  it('does not advance with fewer than two playable photos', () => {
    const state = round(['a']);
    expect(isFixedDisplay(state)).toBe(true);
    const advanced = slideshowReducer(state, { type: 'slideshow.advance' });
    expect(advanced).toBe(state);
    expect(advanced.roundComplete).toBe(false);
  });

  it('reports an empty round', () => {
    expect(round([]).status).toBe('empty');
  });

  it('ignores advance while paused', () => {
    const paused = slideshowReducer(round(['a', 'b']), { type: 'slideshow.pause' });
    expect(slideshowReducer(paused, { type: 'slideshow.advance' }).index).toBe(0);
    const resumed = slideshowReducer(paused, { type: 'slideshow.resume' });
    expect(slideshowReducer(resumed, { type: 'slideshow.advance' }).index).toBe(1);
  });
});

describe('slideshow: interval and refresh', () => {
  it('clamps the configured interval', () => {
    const fast = slideshowReducer(initialSlideshowState, {
      type: 'slideshow.configure',
      intervalSeconds: 1,
    });
    expect(fast.intervalMs).toBe(MIN_SLIDESHOW_INTERVAL_MS);
    const broken = slideshowReducer(initialSlideshowState, {
      type: 'slideshow.configure',
      intervalSeconds: 0,
    });
    expect(broken.intervalMs).toBe(DEFAULT_SLIDESHOW_INTERVAL_MS);
    expect(
      slideshowReducer(initialSlideshowState, { type: 'slideshow.configure', intervalSeconds: 45 })
        .intervalMs,
    ).toBe(45_000);
  });

  it('refreshes the list without restarting the current image', () => {
    const state = run(round(['a', 'b', 'c']), { type: 'slideshow.advance' });
    const refreshed = slideshowReducer(state, {
      type: 'slideshow.listRefreshed',
      items: [photo('x'), photo('b'), photo('y')],
      nextCursor: null,
    });
    expect(currentSlide(refreshed)?.id).toBe('b');
    expect(refreshed.generation).toBe(state.generation);
    expect(nextSlide(refreshed)?.id).toBe('y');
  });

  it('keeps showing a photo that left the collection until the next advance', () => {
    const state = round(['a', 'b']);
    const refreshed = slideshowReducer(state, {
      type: 'slideshow.listRefreshed',
      items: [photo('c')],
      nextCursor: null,
    });
    expect(currentSlide(refreshed)?.id).toBe('a');
    expect(refreshed.round.map((item) => item.id)).toEqual(['a', 'c']);
  });
});
