import type { ApiClient } from '../core/api';
import type { FamilyAvailability, FamilySourceSnapshot, HouseResponse } from '../types/api';
import type { AppAction } from './state';

export interface HouseState {
  snapshot: HouseResponse | null;
  status: 'loading' | 'available' | 'stale' | 'failed';
  refreshing: boolean;
  request: number | null;
}
export const initialHouseState: HouseState = { snapshot: null, status: 'loading', refreshing: false, request: null };
export type HouseAction =
  | { type: 'house.reset' }
  | { type: 'house.loading'; request: number }
  | { type: 'house.loaded'; request: number; snapshot: HouseResponse; receivedAt: number }
  | { type: 'house.failed'; request: number };
export function houseReducer(state: HouseState, action: HouseAction): HouseState {
  if (action.type === 'house.reset') return initialHouseState;
  if (action.type === 'house.loading') return { ...state, status: state.snapshot ? state.status : 'loading', refreshing: true, request: action.request };
  if (action.request !== state.request) return state;
  if (action.type === 'house.loaded') return { snapshot: action.snapshot, status: 'available', refreshing: false, request: null };
  return { ...state, status: state.snapshot ? 'stale' : 'failed', refreshing: false, request: null };
}
export function houseAvailability<T>(source: FamilySourceSnapshot<T>, now: number, transportFailed: boolean): FamilyAvailability {
  if (source.availability !== 'available') return source.availability;
  return transportFailed || (source.expires_at !== null && now >= Date.parse(source.expires_at)) ? 'stale' : 'available';
}
export interface HouseLoader {
  load(fresh?: boolean): Promise<void>;
  /** Called synchronously before throttling source notifications or clearing authorization. */
  invalidate(): void;
}
export function createHouseLoader(api: Pick<ApiClient, 'getHouse'>, dispatch: (action: AppAction) => void, now = Date.now): HouseLoader {
  let sequence = 0;
  let pending: Promise<void> | null = null;
  let controller: AbortController | null = null;
  return {
    invalidate() { sequence += 1; controller?.abort(); controller = null; pending = null; dispatch({ type: 'house.reset' }); },
    load(fresh = false) {
      if (pending && !fresh) return pending;
      const request = ++sequence;
      controller?.abort();
      controller = new AbortController();
      const signal = controller.signal;
      dispatch({ type: 'house.loading', request });
      const task = (async () => {
        try {
          const snapshot = await api.getHouse(signal);
          if (request !== sequence) throw new DOMException('House request superseded', 'AbortError');
          dispatch({ type: 'house.loaded', request, snapshot, receivedAt: now() });
        } catch (error) {
          if (request === sequence) dispatch({ type: 'house.failed', request });
          throw error;
        } finally { if (request === sequence) { pending = null; controller = null; } }
      })();
      pending = task;
      return task;
    },
  };
}
