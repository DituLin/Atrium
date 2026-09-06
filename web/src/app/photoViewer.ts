/**
 * Single-photo viewer state (W-203, design §7.1/§7.3).
 *
 * The viewer owns one photo plus its neighbours inside a collection, so
 * Left/Right walk the same order the browser grid showed. Media rules:
 *  - `202 processing` shows a spinner and retries; after `MEDIA_MAX_RETRIES`
 *    the viewer skips to the next neighbour, or leaves when there is none;
 *  - `404/410` drops the photo and leaves immediately;
 *  - `503` is treated like an exhausted retry: skip rather than stare at a
 *    spinner forever.
 */

import { MEDIA_MAX_RETRIES } from '../core/media';
import type { PhotoCollection, PhotoItem, PhotoNeighbors } from '../types/api';

export type PhotoViewerStatus =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'processing'
  | 'missing'
  | 'error';

export interface PhotoViewerState {
  photoId: string | null;
  collection: PhotoCollection | null;
  item: PhotoItem | null;
  neighbors: PhotoNeighbors | null;
  status: PhotoViewerStatus;
  /** `202` attempts for the current photo. */
  retries: number;
  /** Id of the image the browser reported as decoded (`<img onload>`). */
  renderedId: string | null;
  generation: number;
}

export const initialPhotoViewerState: PhotoViewerState = {
  photoId: null,
  collection: null,
  item: null,
  neighbors: null,
  status: 'idle',
  retries: 0,
  renderedId: null,
  generation: 0,
};

export type PhotoViewerAction =
  | { type: 'viewer.open'; photoId: string; collection: PhotoCollection | null }
  | {
      type: 'viewer.loaded';
      generation: number;
      item: PhotoItem;
      neighbors: PhotoNeighbors | null;
    }
  | { type: 'viewer.loadFailed'; generation: number; gone: boolean }
  | { type: 'viewer.rendered'; id: string }
  | { type: 'viewer.mediaProcessing'; id: string }
  | { type: 'viewer.mediaGone'; id: string }
  | { type: 'viewer.mediaUnavailable'; id: string }
  | { type: 'viewer.close' };

export function photoViewerReducer(
  state: PhotoViewerState,
  action: PhotoViewerAction,
): PhotoViewerState {
  switch (action.type) {
    case 'viewer.open':
      if (state.photoId === action.photoId && state.status !== 'idle') return state;
      return {
        ...initialPhotoViewerState,
        photoId: action.photoId,
        collection: action.collection,
        status: 'loading',
        generation: state.generation + 1,
      };
    case 'viewer.loaded':
      if (action.generation !== state.generation) return state;
      return {
        ...state,
        item: action.item,
        neighbors: action.neighbors,
        status: action.item.preview.status === 'ready' ? 'loading' : 'processing',
      };
    case 'viewer.loadFailed':
      if (action.generation !== state.generation) return state;
      return { ...state, status: action.gone ? 'missing' : 'error' };
    case 'viewer.rendered':
      if (action.id !== state.photoId) return state;
      return { ...state, status: 'ready', renderedId: action.id };
    case 'viewer.mediaProcessing': {
      if (action.id !== state.photoId) return state;
      const retries = state.retries + 1;
      return { ...state, retries, status: retries > MEDIA_MAX_RETRIES ? 'missing' : 'processing' };
    }
    case 'viewer.mediaUnavailable':
      return action.id === state.photoId ? { ...state, status: 'missing' } : state;
    case 'viewer.mediaGone':
      return action.id === state.photoId
        ? { ...state, status: 'missing', item: null, renderedId: null }
        : state;
    case 'viewer.close':
      return { ...initialPhotoViewerState, generation: state.generation + 1 };
    default:
      return state;
  }
}

/** Neighbour id in one direction, or null at the end of the collection. */
export function neighborId(
  state: PhotoViewerState,
  direction: 'previous' | 'next',
): string | null {
  if (!state.neighbors) return null;
  return direction === 'next' ? state.neighbors.next_id : state.neighbors.previous_id;
}

/**
 * Where to go when the current photo cannot be shown: forward first, backward
 * second, and `null` when the viewer has to return to the previous route.
 */
export function skipTarget(state: PhotoViewerState): string | null {
  return neighborId(state, 'next') ?? neighborId(state, 'previous');
}

export function shouldLeave(state: PhotoViewerState): boolean {
  return state.status === 'missing' && skipTarget(state) === null;
}

/** True while the spinner is the right thing to show (design §7.3). */
export function isWaiting(state: PhotoViewerState): boolean {
  return state.status === 'loading' || state.status === 'processing';
}
