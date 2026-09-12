import { useEffect } from 'react';
import type { OverviewResponse } from '../types/api';
import type { AppAction } from './state';

/** The second tick is not precise enough for sub-second authorization of content. */
export function useOverviewDeadline(snapshot: OverviewResponse | null, now: number, offsetMs: number, dispatch: (action: AppAction) => void): void {
  useEffect(() => {
    if (!snapshot) return;
    const times: number[] = [];
    for (const source of [snapshot.sources.notice, ...snapshot.sources.nas]) {
      if (source.expires_at) times.push(Date.parse(source.expires_at));
      for (const item of source.items) {
        if (item.valid_from) times.push(Date.parse(item.valid_from));
        if (item.valid_until) times.push(Date.parse(item.valid_until));
      }
    }
    const deadline = Math.min(...times.filter(time => time > now));
    if (!Number.isFinite(deadline)) return;
    // Long deadlines are re-evaluated by the ordinary clock tick; avoid timer overflow.
    const timer = setTimeout(() => dispatch({ type: 'app.tick', nowMs: Date.now() }), Math.max(0, Math.min(deadline - (Date.now() + offsetMs), 2_147_483_647)));
    return () => clearTimeout(timer);
  }, [snapshot, now, offsetMs, dispatch]);
}
