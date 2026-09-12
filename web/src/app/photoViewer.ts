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
  /** Frozen client-known order for this visit; never server random neighbors. */
  sequence: readonly string[];
  /** Identifies the command-owned visit; manual visits never inherit it. */
  commandId: string | null;
  failedIds: readonly string[];
  photoId: string | null;
  collection: PhotoCollection | null;
  item: PhotoItem | null;
  /** Last current-generation onLoad, retained only within a manual visit. */
  shownItem: PhotoItem | null;
  neighbors: PhotoNeighbors | null;
  status: PhotoViewerStatus;
  /** `202` attempts for the current photo. */
  retries: number;
  mediaReady: boolean;
  imageFailures: number;
  /** Id of the image the browser reported as decoded (`<img onload>`). */
  renderedId: string | null;
  generation: number;
}

export const initialPhotoViewerState: PhotoViewerState = {
  sequence: [],
  commandId: null,
  failedIds: [],
  photoId: null,
  collection: null,
  item: null,
  shownItem: null,
  neighbors: null,
  status: 'idle',
  retries: 0,
  mediaReady: false,
  imageFailures: 0,
  renderedId: null,
  generation: 0,
};

export type PhotoViewerAction =
  | { type: 'viewer.open'; photoId: string; collection: PhotoCollection | null; freshRender?: boolean; sequence?: readonly string[]; commandId?: string }
  | {
      type: 'viewer.loaded';
      generation: number;
      item: PhotoItem;
      neighbors: PhotoNeighbors | null;
    }
  | { type: 'viewer.loadFailed'; generation: number; gone: boolean }
  | { type: 'viewer.rendered'; id: string; generation: number }
  | { type: 'viewer.mediaProcessing'; id: string; generation?: number }
  | { type: 'viewer.mediaGone'; id: string; generation?: number }
  | { type: 'viewer.mediaUnavailable'; id: string; generation?: number }
  | { type: 'viewer.mediaReady'; id: string; generation: number }
  | { type: 'viewer.imageFailed'; id: string; generation: number }
  | { type: 'viewer.close' };

export function photoViewerReducer(
  state: PhotoViewerState,
  action: PhotoViewerAction,
): PhotoViewerState {
  switch (action.type) {
    case 'viewer.open':
      if (!action.freshRender && state.photoId === action.photoId && state.status !== 'idle') return state;
      return {
        ...initialPhotoViewerState,
        sequence: action.sequence ? [...new Set(action.sequence)] : action.freshRender ? [action.photoId] : state.sequence.includes(action.photoId) ? state.sequence : [action.photoId],
        failedIds: action.sequence || action.freshRender ? [] : state.failedIds,
        shownItem: action.sequence || action.freshRender ? null : state.shownItem,
        commandId: action.commandId ?? null,
        photoId: action.photoId,
        collection: action.collection,
        status: 'loading',
        generation: state.generation + 1,
      };
    case 'viewer.loaded':
      if (action.generation !== state.generation || action.item.id !== state.photoId) return state;
      return {
        ...state,
        item: action.item,
        neighbors: action.neighbors,
        status: action.item.preview.status === 'ready' ? 'loading' : 'processing',
      };
    case 'viewer.loadFailed':
      if (action.generation !== state.generation) return state;
      return { ...state, status: action.gone ? 'missing' : 'error', failedIds: failedIds(state) };
    case 'viewer.rendered':
      if (action.id !== state.photoId || action.generation !== state.generation || state.failedIds.includes(action.id)) return state;
      return { ...state, status: 'ready', renderedId: action.id, shownItem: state.item };
    case 'viewer.mediaReady':
      return currentMedia(state, action) && !state.failedIds.includes(action.id)
        ? { ...state, mediaReady: true, status: 'loading' } : state;
    case 'viewer.imageFailed': {
      if (!currentMedia(state, action)) return state;
      const imageFailures = state.imageFailures + 1;
      return { ...state, imageFailures, mediaReady: false, renderedId: null,
        status: imageFailures > MEDIA_MAX_RETRIES ? 'missing' : 'loading',
        failedIds: imageFailures > MEDIA_MAX_RETRIES ? failedIds(state) : state.failedIds };
    }
    case 'viewer.mediaProcessing': {
      if (!currentMedia(state, action)) return state;
      const retries = state.retries + 1;
      return { ...state, retries, status: retries > MEDIA_MAX_RETRIES ? 'missing' : 'processing', failedIds: retries > MEDIA_MAX_RETRIES ? failedIds(state) : state.failedIds };
    }
    case 'viewer.mediaUnavailable':
      return currentMedia(state, action) ? { ...state, status: 'missing', failedIds: failedIds(state), renderedId: null } : state;
    case 'viewer.mediaGone':
      return currentMedia(state, action)
        ? { ...state, status: 'missing', item: null, renderedId: null, shownItem: state.shownItem?.id === action.id ? null : state.shownItem, failedIds: failedIds(state) }
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
  const step = direction === 'next' ? 1 : -1;
  const index = state.sequence.indexOf(state.photoId ?? '');
  if (index < 0) return null;
  for (let i = index + step; i >= 0 && i < state.sequence.length; i += step) {
    const id = state.sequence[i]!;
    if (!state.failedIds.includes(id)) return id;
  }
  return null;
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

function failedIds(state: PhotoViewerState): readonly string[] {
  return state.photoId && !state.failedIds.includes(state.photoId) ? [...state.failedIds, state.photoId] : state.failedIds;
}
function currentMedia(state: PhotoViewerState, action: { id: string; generation?: number }): boolean {
  return action.id === state.photoId && (action.generation === undefined || action.generation === state.generation);
}
