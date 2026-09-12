/**
 * Collection browser state (W-202, design §6.4/§7.1).
 *
 * The grid is a roving-focus list over one collection's pages. Focus arithmetic
 * is shared with the dashboard through `ui/focusNav`; everything else — which
 * page to ask for next, when the list is empty and why — lives here so the
 * screen component stays a projection.
 */

import type { PhotoCollection, PhotoItem, PhotoListMeta } from '../types/api';
import type { RemoteKey } from '../ui/keys';
import { nextFocusIndex } from '../ui/focusNav';

export type PhotoListStatus = 'idle' | 'loading' | 'ready' | 'error';

/** Thumbs per row on the browser grid; also the "near the end" prefetch step. */
export const GRID_COLUMNS = 4;

/** Photos per request (design §6.4: default 50, max 100). */
export const COLLECTION_PAGE_SIZE = 50;

export interface PhotoListState {
  returnFocus: { id: string; index: number; scrollTop: number } | null;
  restorePending: boolean;
  returnNotice: string | null;
  collection: PhotoCollection | null;
  items: readonly PhotoItem[];
  cursor: string | null;
  focusIndex: number;
  status: PhotoListStatus;
  /** True while a further page is in flight (the first page uses `status`). */
  loadingMore: boolean;
  meta: PhotoListMeta | null;
  /** Requests are tagged so a page for an abandoned collection is ignored. */
  generation: number;
}

export const initialPhotoListState: PhotoListState = {
  returnFocus: null,
  restorePending: false,
  returnNotice: null,
  collection: null,
  items: [],
  cursor: null,
  focusIndex: 0,
  status: 'idle',
  loadingMore: false,
  meta: null,
  generation: 0,
};

export type PhotoListAction =
  | { type: 'photos.open'; collection: PhotoCollection }
  | {
      type: 'photos.pageLoaded';
      collection: PhotoCollection;
      generation: number;
      items: readonly PhotoItem[];
      nextCursor: string | null;
      meta: PhotoListMeta | null;
      append: boolean;
    }
  | { type: 'photos.pageRequested' }
  | { type: 'photos.loadFailed'; generation: number }
  | { type: 'photos.move'; direction: RemoteKey; columns?: number }
  | { type: 'photos.focus'; index: number }
  | { type: 'photos.itemGone'; id: string }
  | { type: 'photos.remember'; scrollTop: number }
  | { type: 'photos.restore' }
  | { type: 'photos.restored' }
  | { type: 'photos.refreshDeferred'; generation: number }
  | { type: 'photos.reset' };

export function photoListReducer(
  state: PhotoListState,
  action: PhotoListAction,
): PhotoListState {
  switch (action.type) {
    case 'photos.open':
      if (state.collection === action.collection && state.status !== 'idle') return state;
      return {
        ...initialPhotoListState,
        collection: action.collection,
        status: 'loading',
        generation: state.generation + 1,
      };
    case 'photos.pageRequested':
      return state.cursor === null || state.loadingMore ? state : { ...state, loadingMore: true };
    case 'photos.pageLoaded': {
      if (action.generation !== state.generation || action.collection !== state.collection) {
        return state;
      }
      const known = new Set(state.items.map((item) => item.id));
      const added = action.items.filter((item) => !known.has(item.id));
      const items = action.append ? [...state.items, ...added] : action.items;
      return {
        ...state,
        items,
        cursor: action.nextCursor,
        meta: action.meta,
        status: 'ready',
        loadingMore: false,
        focusIndex: restoredIndex(items, state.items[state.focusIndex]?.id, state.focusIndex),
      };
    }
    case 'photos.loadFailed':
      if (action.generation !== state.generation) return state;
      return { ...state, loadingMore: false, status: state.items.length > 0 ? 'ready' : 'error' };
    case 'photos.move': {
      const next = nextFocusIndex(state.focusIndex, action.direction, {
        count: state.items.length,
        columns: action.columns ?? GRID_COLUMNS,
      });
      return next < 0 || next === state.focusIndex ? state : { ...state, focusIndex: next };
    }
    case 'photos.focus': {
      const clamped = Math.min(Math.max(action.index, 0), Math.max(0, state.items.length - 1));
      return clamped === state.focusIndex ? state : { ...state, focusIndex: clamped };
    }
    case 'photos.itemGone': {
      const items = state.items.filter((item) => item.id !== action.id);
      if (items.length === state.items.length) return state;
      const removedIndex = state.items.findIndex(item => item.id === action.id);
      return {
        ...state,
        items,
        returnFocus: state.returnFocus ? { ...state.returnFocus,
          index: restoredIndex(items, state.returnFocus.id, state.returnFocus.index - (removedIndex < state.returnFocus.index ? 1 : 0)) } : null,
        focusIndex: restoredIndex(items, state.items[state.focusIndex]?.id, state.focusIndex),
      };
    }
    case 'photos.remember': {
      const item = focusedItem(state);
      return item ? { ...state, returnFocus: { id: item.id, index: state.focusIndex, scrollTop: action.scrollTop }, returnNotice: null } : state;
    }
    case 'photos.restore': {
      if (!state.returnFocus) return state;
      const { id, index } = state.returnFocus;
      return { ...state, restorePending: true, focusIndex: restoredIndex(state.items, id, index),
        returnNotice: state.items.some(item => item.id === id) ? state.returnNotice : '原照片已移除，已选择相邻照片。' };
    }
    case 'photos.refreshDeferred':
      return action.generation === state.generation ? { ...state, returnNotice: '照片列表已有更新，暂保留原浏览位置。' } : state;
    case 'photos.restored':
      return { ...state, restorePending: false, returnFocus: null };
    case 'photos.reset':
      return { ...initialPhotoListState, generation: state.generation + 1 };
    default:
      return state;
  }
}

export function focusedItem(state: PhotoListState): PhotoItem | null {
  return state.items[state.focusIndex] ?? null;
}

/**
 * Load the next page once the focus is within two rows of the end, so the
 * remote never reaches a wall while the request is still travelling.
 */
export function shouldLoadMore(state: PhotoListState): boolean {
  if (state.cursor === null || state.loadingMore || state.status === 'loading') return false;
  return state.focusIndex >= state.items.length - GRID_COLUMNS * 2;
}

/** Empty states differ per collection (PRD 5.2); this names the case. */
export type PhotoListEmptyKind = 'none' | 'recent_baseline_only' | 'captured_today' | 'generic';

export function emptyKind(state: PhotoListState): PhotoListEmptyKind {
  if (state.status !== 'ready' || state.items.length > 0) return 'none';
  switch (state.collection) {
    case 'recent':
      return 'recent_baseline_only';
    case 'captured_today':
      return 'captured_today';
    default:
      return 'generic';
  }
}

function restoredIndex(items: readonly PhotoItem[], id: string | undefined, fallback: number): number {
  const found = items.findIndex(item => item.id === id);
  return found >= 0 ? found : Math.min(fallback, Math.max(0, items.length - 1));
}
