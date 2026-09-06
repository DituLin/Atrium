import { describe, expect, it } from 'vitest';

import type { PhotoItem } from '../types/api';
import {
  GRID_COLUMNS,
  emptyKind,
  focusedItem,
  initialPhotoListState,
  photoListReducer,
  shouldLoadMore,
} from './photoList';
import type { PhotoListAction, PhotoListState } from './photoList';

function photo(id: string): PhotoItem {
  return {
    id,
    source_id: 'family_photos',
    captured_at: '2026-09-05T10:00:00+08:00',
    captured_confidence: 'exact',
    first_seen_at: '2026-09-05T02:00:00Z',
    is_baseline: false,
    width: 1600,
    height: 1200,
    preview: { status: 'ready', width: 1280, height: 960 },
    urls: { preview: '', thumb: '' },
  };
}

function run(state: PhotoListState, ...actions: PhotoListAction[]): PhotoListState {
  return actions.reduce(photoListReducer, state);
}

function loaded(count: number, nextCursor: string | null = null): PhotoListState {
  const opened = photoListReducer(initialPhotoListState, {
    type: 'photos.open',
    collection: 'recent',
  });
  return photoListReducer(opened, {
    type: 'photos.pageLoaded',
    collection: 'recent',
    generation: opened.generation,
    items: Array.from({ length: count }, (_, i) => photo(`p${i}`)),
    nextCursor,
    meta: { total: count },
    append: false,
  });
}

describe('photo list: navigation', () => {
  it('moves right and left inside a row without wrapping', () => {
    let state = loaded(12);
    state = run(state, { type: 'photos.move', direction: 'right' });
    expect(state.focusIndex).toBe(1);
    state = run(state, { type: 'photos.move', direction: 'left' });
    expect(state.focusIndex).toBe(0);
    state = run(state, { type: 'photos.move', direction: 'left' });
    expect(state.focusIndex).toBe(0);
  });

  it('moves by a row on up and down and stops at the edges', () => {
    let state = loaded(12);
    state = run(state, { type: 'photos.move', direction: 'down' });
    expect(state.focusIndex).toBe(GRID_COLUMNS);
    state = run(state, { type: 'photos.move', direction: 'up' });
    expect(state.focusIndex).toBe(0);
    state = run(state, { type: 'photos.move', direction: 'up' });
    expect(state.focusIndex).toBe(0);
  });

  it('never moves past the last item on a short final row', () => {
    let state = loaded(GRID_COLUMNS + 2);
    state = run(state, { type: 'photos.move', direction: 'down' });
    expect(state.focusIndex).toBe(GRID_COLUMNS);
    state = run(state, { type: 'photos.move', direction: 'down' });
    expect(state.focusIndex).toBe(GRID_COLUMNS);
    expect(focusedItem(state)?.id).toBe(`p${GRID_COLUMNS}`);
  });

  it('clamps focus when items disappear', () => {
    let state = loaded(3);
    state = run(state, { type: 'photos.focus', index: 2 });
    state = run(state, { type: 'photos.itemGone', id: 'p2' });
    expect(state.items).toHaveLength(2);
    expect(state.focusIndex).toBe(1);
  });
});

describe('photo list: pagination', () => {
  it('asks for the next page when focus nears the end', () => {
    const state = loaded(GRID_COLUMNS * 4, 'cursor-2');
    expect(shouldLoadMore(state)).toBe(false);
    const near = photoListReducer(state, { type: 'photos.focus', index: GRID_COLUMNS * 2 });
    expect(shouldLoadMore(near)).toBe(true);
  });

  it('never asks twice for the same page and stops at the last one', () => {
    const state = photoListReducer(loaded(GRID_COLUMNS, 'cursor-2'), {
      type: 'photos.pageRequested',
    });
    expect(state.loadingMore).toBe(true);
    expect(shouldLoadMore(state)).toBe(false);
    const appended = photoListReducer(state, {
      type: 'photos.pageLoaded',
      collection: 'recent',
      generation: state.generation,
      items: [photo('p0'), photo('q1')],
      nextCursor: null,
      meta: null,
      append: true,
    });
    expect(appended.items.map((item) => item.id)).toEqual([
      ...Array.from({ length: GRID_COLUMNS }, (_, i) => `p${i}`),
      'q1',
    ]);
    expect(shouldLoadMore(appended)).toBe(false);
  });

  it('ignores a page that belongs to an abandoned collection', () => {
    const state = loaded(4, 'cursor-2');
    const switched = photoListReducer(state, { type: 'photos.open', collection: 'random' });
    const stale = photoListReducer(switched, {
      type: 'photos.pageLoaded',
      collection: 'recent',
      generation: state.generation,
      items: [photo('z')],
      nextCursor: null,
      meta: null,
      append: true,
    });
    expect(stale.items).toHaveLength(0);
    expect(stale.status).toBe('loading');
  });

  it('keeps rendering what it has when a later page fails', () => {
    const state = photoListReducer(loaded(4, 'c'), { type: 'photos.pageRequested' });
    const failed = photoListReducer(state, {
      type: 'photos.loadFailed',
      generation: state.generation,
    });
    expect(failed.status).toBe('ready');
    expect(failed.items).toHaveLength(4);
  });
});

describe('photo list: empty states', () => {
  it('names the baseline-only case for recent and the unknown case for today', () => {
    expect(emptyKind(loaded(0))).toBe('recent_baseline_only');
    const today = photoListReducer(
      photoListReducer(initialPhotoListState, {
        type: 'photos.open',
        collection: 'captured_today',
      }),
      {
        type: 'photos.pageLoaded',
        collection: 'captured_today',
        generation: 1,
        items: [],
        nextCursor: null,
        meta: { unknown_captured_count: 3 },
        append: false,
      },
    );
    expect(emptyKind(today)).toBe('captured_today');
    expect(today.meta?.unknown_captured_count).toBe(3);
    expect(emptyKind(loaded(2))).toBe('none');
  });
});
