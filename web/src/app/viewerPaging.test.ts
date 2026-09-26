import { describe, expect, it } from 'vitest';
import type { PhotoItem } from '../types/api';
import { neighborId } from './photoViewer';
import { appReducer, createInitialState } from './state';
import type { AppAction, AppState } from './state';

function photo(id: string): PhotoItem {
  return {
    id, source_id: 'family_photos', captured_at: '2026-09-05T10:00:00+08:00', captured_confidence: 'exact',
    first_seen_at: '2026-09-05T02:00:00Z', is_baseline: false, width: 1600, height: 1200,
    preview: { status: 'ready', width: 1280, height: 960 }, urls: { preview: '', thumb: '' },
  };
}
const page = (from: number, count: number) => Array.from({ length: count }, (_, i) => photo(`p${from + i}`));
const run = (state: AppState, ...actions: AppAction[]) => actions.reduce(appReducer, state);

describe('viewer keeps going past the first page', () => {
  it('extends a library visit when the next page of the same collection loads', () => {
    let state = run(createInitialState(0),
      { type: 'router.navigate', route: { name: 'photos', collection: 'all' } },
      { type: 'photos.open', collection: 'all' });
    const generation = state.collection.generation;
    state = run(state,
      { type: 'photos.pageLoaded', collection: 'all', generation, items: page(0, 50), nextCursor: 'c1', meta: null, append: false },
      { type: 'router.navigate', route: { name: 'photo', photoId: 'p49', collection: 'all' } });
    expect(neighborId(state.viewer, 'next')).toBeNull();
    state = run(state, { type: 'photos.pageLoaded', collection: 'all', generation, items: page(50, 50), nextCursor: 'c2', meta: null, append: true });
    expect(state.viewer.sequence).toHaveLength(100);
    expect(neighborId(state.viewer, 'next')).toBe('p50');
    expect(state.viewer.sequence.slice(0, 50)).toEqual(page(0, 50).map(p => p.id));
  });

  it('extends a visit opened from home with the rest of the random round', () => {
    let state = run(createInitialState(0),
      { type: 'slideshow.roundStarted', seed: 's1' },
      { type: 'slideshow.pageLoaded', seed: 's1', items: page(0, 50), nextCursor: 'r1' },
      { type: 'router.navigate', route: { name: 'photo', photoId: 'p49' } });
    expect(state.viewer.origin).toBe('slideshow');
    expect(neighborId(state.viewer, 'next')).toBeNull();
    state = run(state, { type: 'slideshow.pageLoaded', seed: 's1', items: page(50, 10), nextCursor: null });
    expect(neighborId(state.viewer, 'next')).toBe('p50');
  });

  it('never grows a visit started by a server show command or from another collection', () => {
    let state = run(createInitialState(0),
      { type: 'router.navigate', route: { name: 'photos', collection: 'recent' } },
      { type: 'photos.open', collection: 'recent' });
    const generation = state.collection.generation;
    state = run(state,
      { type: 'photos.pageLoaded', collection: 'recent', generation, items: page(0, 50), nextCursor: 'c1', meta: null, append: false },
      { type: 'viewer.open', photoId: 'p10', collection: null, sequence: ['p10'], freshRender: true, commandId: 'cmd_1' });
    expect(state.viewer.origin).toBeNull();
    state = run(state, { type: 'viewer.extend', ids: ['p11', 'p12'] });
    expect(state.viewer.sequence).toEqual(['p10']);
  });
});
