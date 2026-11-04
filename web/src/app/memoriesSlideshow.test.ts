import { describe, expect, it } from 'vitest';
import type { MemoriesResponse, PhotoItem, PreviewStatus } from '../types/api';
import { appReducer, createInitialState } from './state';
import { currentSlide, initialSlideshowState, slideshowReducer } from './slideshow';
const item = (id: string) => ({ id, preview: { status: 'ready' }, resume_cursor: `anchor:${id}` }) as PhotoItem & { resume_cursor: string };
const page = (ids: string[], day = '2026-11-02', next: string | null = null): MemoriesResponse => ({ items: ids.map(item), next_cursor: next, meta: { collection: 'memories', day, screen_id: 'screen-a', total: ids.length } });
function started() { return slideshowReducer(initialSlideshowState, { type: 'slideshow.memoriesStarted', resumeCursor: 'anchor:b' }); }
describe('daily slideshow', () => {
  it('keeps uncached metadata-ready photos in the daily order instead of biasing it toward cached sources', () => {
    const start = started();
    const statuses: PreviewStatus[] = ['ready', 'pending', 'processing', 'evicted', 'unavailable', 'failed'];
    const cold = page(statuses);
    cold.items.forEach((photo, index) => { photo.preview.status = statuses[index]!; });
    const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: cold });
    expect(loaded.round.map(photo => photo.id)).toEqual(statuses.slice(0, -1));
    expect(loaded.displayedCursor).toBeNull();
  });
  it('accepts an inclusive resumed page and keeps the last actually displayed anchor', () => {
    const start = started();
    const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['b', 'c']) });
    expect(currentSlide(loaded)?.id).toBe('b');
    expect(loaded.memories?.day).toBe('2026-11-02');
    expect(loaded.displayedCursor).toBeNull();
    const shown = slideshowReducer(loaded, { type: 'slideshow.displayed', id: 'b', generation: loaded.generation });
    const advanced = slideshowReducer(shown, { type: 'slideshow.advance' });
    expect(currentSlide(advanced)?.id).toBe('c');
    expect(advanced.displayedCursor).toBe('anchor:b');
  });
  it('replaces the round across midnight and rejects late prior-generation pages and displays', () => {
    const start = started();
    const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['b', 'c']) });
    const tomorrow = slideshowReducer(loaded, { type: 'slideshow.memoriesStarted', resumeCursor: null });
    expect(slideshowReducer(tomorrow, { type: 'slideshow.memoriesLoaded', generation: loaded.generation, page: page(['old']) })).toBe(tomorrow);
    expect(slideshowReducer(tomorrow, { type: 'slideshow.displayed', generation: loaded.generation, id: 'b' })).toBe(tomorrow);
    const fresh = slideshowReducer(tomorrow, { type: 'slideshow.memoriesLoaded', generation: tomorrow.generation, page: page(['x'], '2026-11-03') });
    expect(fresh.round.map(p => p.id)).toEqual(['x']);
  });
  it('refreshes from the current anchor without retaining a removed or unauthorized image', () => {
    const start = started();
    const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['b', 'c']) });
    const refreshed = slideshowReducer(loaded, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['d', 'e']), replace: true });
    expect(refreshed.round.map(p => p.id)).toEqual(['d', 'e']);
    expect(currentSlide(refreshed)?.id).toBe('d');
  });
  it('purges all daily metadata and never accepts a pre-purge response after pairing', () => {
    let state = createInitialState(0);
    state = appReducer(state, { type: 'slideshow.memoriesStarted', resumeCursor: null });
    const generation = state.slideshow.generation;
    state = appReducer(state, { type: 'app.needsPairing' });
    state = appReducer(state, { type: 'pair.claimed', screenName: 'New TV' });
    state = appReducer(state, { type: 'slideshow.memoriesLoaded', generation, page: page(['old']) });
    expect(state.slideshow.round).toHaveLength(0);
    expect(state.slideshow.memories).toBeNull();
  });
});

it('finishes a resumed one-item tail when the whole daily library has more photos', () => {
  const start = started();
  const tail = page(['last']); tail.meta.total = 20;
  const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: tail });
  expect(slideshowReducer(loaded, { type: 'slideshow.advance' }).roundComplete).toBe(true);
});
it('rejects a decoded item that is no longer current even within the same day', () => {
  const start = started();
  const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['b', 'c']) });
  const advanced = slideshowReducer(loaded, { type: 'slideshow.advance' });
  expect(slideshowReducer(advanced, { type: 'slideshow.displayed', generation: advanced.generation, id: 'b' })).toBe(advanced);
});

it('invalidates pending yesterday pages as soon as a refresh supplies a new authoritative day', () => {
  const start = started();
  const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['b', 'c'], '2026-11-02', 'yesterday:next') });
  const fresh = slideshowReducer(loaded, { type: 'slideshow.memoriesLoaded', generation: loaded.generation, page: page(['today'], '2026-11-03'), replace: true });
  expect(fresh.generation).toBeGreaterThan(loaded.generation);
  expect(slideshowReducer(fresh, { type: 'slideshow.memoriesLoaded', generation: loaded.generation, page: page(['late-yesterday']) })).toBe(fresh);
});
it('rejects a continuation page whose cursor was superseded by a refresh', () => {
  const start = started();
  const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['b', 'c'], '2026-11-02', 'old:next') });
  const fresh = slideshowReducer(loaded, { type: 'slideshow.memoriesLoaded', generation: loaded.generation, page: page(['b', 'd'], '2026-11-02', 'fresh:next'), replace: true });
  expect(slideshowReducer(fresh, { type: 'slideshow.memoriesLoaded', generation: fresh.generation, page: page(['stale']), baseCursor: 'old:next' })).toBe(fresh);
});

it('keeps today when a duplicated first-window request returns yesterday late', () => {
  const start = started();
  const today = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['today'], '2026-11-03') });
  const stale = slideshowReducer(today, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['yesterday'], '2026-11-02') });
  expect(stale).toBe(today);
  expect(stale.memories?.day).toBe('2026-11-03');
});
it('invalidates pending continuation after a same-day refresh even when the next cursor is identical', () => {
  const start = started();
  const loaded = slideshowReducer(start, { type: 'slideshow.memoriesLoaded', generation: start.generation, page: page(['current', 'other'], '2026-11-02', 'C50') });
  const fresh = slideshowReducer(loaded, { type: 'slideshow.memoriesLoaded', generation: loaded.generation, page: page(['current', 'other'], '2026-11-02', 'C50'), replace: true });
  const stale = slideshowReducer(fresh, { type: 'slideshow.memoriesLoaded', generation: loaded.generation, page: page(['revoked-source-photo'], '2026-11-02', 'C100'), baseCursor: 'C50' });
  expect(fresh.generation).toBeGreaterThan(loaded.generation);
  expect(stale.round.map(item => item.id)).not.toContain('revoked-source-photo');
});
