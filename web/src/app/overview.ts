import type { ApiClient } from '../core/api';
import type { FamilySourceSnapshot, OverviewResponse, OverviewSources, OverviewEntry } from '../types/api';
import type { AppAction } from './state';

export interface OverviewState {
  snapshot: OverviewResponse | null;
  status: 'loading' | 'available' | 'stale' | 'failed';
  refreshing: boolean;
  request: number | null;
}
export const initialOverviewState: OverviewState = { snapshot: null, status: 'loading', refreshing: false, request: null };
export type OverviewAction =
  | { type: 'overview.reset' }
  | { type: 'overview.loading'; request: number }
  | { type: 'overview.loaded'; request: number; snapshot: OverviewResponse; receivedAt: number }
  | { type: 'overview.failed'; request: number };
export function overviewReducer(state: OverviewState, action: OverviewAction): OverviewState {
  if (action.type === 'overview.reset') return initialOverviewState;
  if (action.type === 'overview.loading') return { ...state, status: state.snapshot ? state.status : 'loading', refreshing: true, request: action.request };
  if (action.request !== state.request) return state;
  if (action.type === 'overview.loaded') return { snapshot: action.snapshot, status: 'available', refreshing: false, request: null };
  return { ...state, status: state.snapshot ? 'stale' : 'failed', refreshing: false, request: null };
}
export interface OverviewLoader {
  load(fresh?: boolean): Promise<void>;
  /** Called synchronously before throttling source notifications or clearing authorization. */
  invalidate(): void;
}
export function createOverviewLoader(api: Pick<ApiClient, 'getOverview'>, dispatch: (action: AppAction) => void, now = Date.now): OverviewLoader {
  let sequence = 0;
  let pending: Promise<void> | null = null;
  let controller: AbortController | null = null;
  return {
    invalidate() { sequence += 1; controller?.abort(); controller = null; pending = null; dispatch({ type: 'overview.reset' }); },
    load(fresh = false) {
      if (pending && !fresh) return pending;
      const request = ++sequence;
      controller?.abort();
      controller = new AbortController();
      const signal = controller.signal;
      dispatch({ type: 'overview.loading', request });
      const task = (async () => {
        try {
          const snapshot = await api.getOverview(signal);
          if (request !== sequence) throw new DOMException('Overview request superseded', 'AbortError');
          dispatch({ type: 'overview.loaded', request, snapshot, receivedAt: now() });
        } catch (error) {
          if (request === sequence) dispatch({ type: 'overview.failed', request });
          throw error;
        } finally { if (request === sequence) { pending = null; controller = null; } }
      })();
      pending = task;
      return task;
    },
  };
}

/** Rebuild references from current authorized sources, never renew an entry's timestamps. */
export function projectOverview(sources: OverviewSources, now: number, transportFailed: boolean): { sources: OverviewSources; entries: OverviewEntry[] } {
  const project = <T>(source: FamilySourceSnapshot<T>): FamilySourceSnapshot<T> => {
    const availability = source.availability === 'available' && (transportFailed || (source.expires_at !== null && now >= Date.parse(source.expires_at))) ? 'stale' : source.availability;
    const items = availability === 'available' || availability === 'stale'
      ? source.items.filter(item => (!item.valid_from || now >= Date.parse(item.valid_from)) && (!item.valid_until || now < Date.parse(item.valid_until))) : [];
    return { ...source, availability, items };
  };
  const current = { ...sources, nas: sources.nas.map(project), notice: project(sources.notice) };
  const ranked: { entry: OverviewEntry; priority: number }[] = [];
  for (const source of current.nas) {
    const health = source.items[0]?.health ?? 'unknown';
    const availability = source.availability;
    const priority = availability === 'failed' ? 0 : availability === 'stale' ? 1 : availability === 'loading' ? 2
      : availability === 'available' ? health === 'offline' || health === 'degraded' ? 0 : health === 'unknown' ? 2 : -1 : -1;
    if (priority >= 0) ranked.push({ priority, entry: { id: `nas:${source.source_id}:source_status`, kind: 'source_status', module: 'nas', source_id: source.source_id, item_id: null } });
  }
  ranked.sort((a, b) => a.priority - b.priority);
  const entries = ranked.map(row => row.entry);
  for (const item of current.notice.items) entries.push({ id: `notice:${current.notice.source_id}:${item.id}`, kind: 'notice', module: 'notice', source_id: current.notice.source_id, item_id: item.id });
  return { sources: current, entries };
}
