import { describe, expect, it } from 'vitest';

import { MEDIA_MAX_RETRIES } from '../core/media';
import type { PhotoItem, PhotoNeighbors, PreviewStatus } from '../types/api';
import {
  initialPhotoViewerState,
  isWaiting,
  neighborId,
  photoViewerReducer,
  shouldLeave,
  skipTarget,
} from './photoViewer';
import type { PhotoViewerAction, PhotoViewerState } from './photoViewer';

function photo(id: string, status: PreviewStatus = 'ready'): PhotoItem {
  return {
    id,
    source_id: 'family_photos',
    captured_at: null,
    captured_confidence: 'unknown',
    first_seen_at: '2026-09-05T02:00:00Z',
    is_baseline: false,
    width: 1600,
    height: 1200,
    preview: { status, width: 1280, height: 960 },
    urls: { preview: '', thumb: '' },
  };
}

function run(state: PhotoViewerState, ...actions: PhotoViewerAction[]): PhotoViewerState {
  return actions.reduce(photoViewerReducer, state);
}

function opened(
  id = 'p2',
  neighbors: PhotoNeighbors | null = { previous_id: 'p1', next_id: 'p3' },
  status: PreviewStatus = 'ready',
): PhotoViewerState {
  const state = photoViewerReducer(initialPhotoViewerState, {
    type: 'viewer.open',
    photoId: id,
    collection: 'recent',
  });
  return photoViewerReducer(state, {
    type: 'viewer.loaded',
    generation: state.generation,
    item: photo(id, status),
    neighbors,
  });
}

describe('photo viewer: loading', () => {
  it('shows the spinner until the image reports onload', () => {
    const state = opened();
    expect(isWaiting(state)).toBe(true);
    const rendered = photoViewerReducer(state, { type: 'viewer.rendered', id: 'p2' });
    expect(rendered.status).toBe('ready');
    expect(rendered.renderedId).toBe('p2');
  });

  it('ignores a render signal for a photo the viewer left', () => {
    const state = photoViewerReducer(opened(), { type: 'viewer.rendered', id: 'p9' });
    expect(state.renderedId).toBeNull();
  });

  it('ignores a response for an abandoned request', () => {
    const state = opened();
    const reopened = photoViewerReducer(state, {
      type: 'viewer.open',
      photoId: 'p7',
      collection: 'recent',
    });
    const stale = photoViewerReducer(reopened, {
      type: 'viewer.loaded',
      generation: state.generation,
      item: photo('p2'),
      neighbors: null,
    });
    expect(stale.item).toBeNull();
    expect(stale.photoId).toBe('p7');
  });
});

describe('photo viewer: neighbours', () => {
  it('exposes previous and next inside the collection', () => {
    const state = opened();
    expect(neighborId(state, 'previous')).toBe('p1');
    expect(neighborId(state, 'next')).toBe('p3');
    expect(skipTarget(state)).toBe('p3');
  });

  it('falls back to the previous photo at the end of the collection', () => {
    const state = opened('p9', { previous_id: 'p8', next_id: null });
    expect(skipTarget(state)).toBe('p8');
  });
});

describe('photo viewer: media rules', () => {
  it('drops a 404 photo and returns when there is nowhere to skip', () => {
    const state = photoViewerReducer(opened('p1', { previous_id: null, next_id: null }), {
      type: 'viewer.mediaGone',
      id: 'p1',
    });
    expect(state.status).toBe('missing');
    expect(state.item).toBeNull();
    expect(shouldLeave(state)).toBe(true);
  });

  it('drops a 404 photo but skips to a neighbour when one exists', () => {
    const state = photoViewerReducer(opened(), { type: 'viewer.mediaGone', id: 'p2' });
    expect(shouldLeave(state)).toBe(false);
    expect(skipTarget(state)).toBe('p3');
  });

  it('spins on 202 and skips after the retry budget', () => {
    let state = opened();
    for (let attempt = 1; attempt <= MEDIA_MAX_RETRIES; attempt += 1) {
      state = photoViewerReducer(state, { type: 'viewer.mediaProcessing', id: 'p2' });
      expect(state.status).toBe('processing');
      expect(isWaiting(state)).toBe(true);
    }
    state = photoViewerReducer(state, { type: 'viewer.mediaProcessing', id: 'p2' });
    expect(state.status).toBe('missing');
    expect(skipTarget(state)).toBe('p3');
  });

  it('treats a 503 as an immediate skip', () => {
    const state = photoViewerReducer(opened(), { type: 'viewer.mediaUnavailable', id: 'p2' });
    expect(state.status).toBe('missing');
  });

  it('starts in processing when the item itself is not ready yet', () => {
    expect(opened('p2', null, 'pending').status).toBe('processing');
  });

  it('reports a load failure that is not a 404 as an error, not as missing', () => {
    const state = photoViewerReducer(initialPhotoViewerState, {
      type: 'viewer.open',
      photoId: 'p4',
      collection: null,
    });
    const failed = run(state, { type: 'viewer.loadFailed', generation: state.generation, gone: false });
    expect(failed.status).toBe('error');
    expect(shouldLeave(failed)).toBe(false);
  });

  it('clears everything on close', () => {
    const state = photoViewerReducer(opened(), { type: 'viewer.close' });
    expect(state.photoId).toBeNull();
    expect(state.status).toBe('idle');
  });
});
