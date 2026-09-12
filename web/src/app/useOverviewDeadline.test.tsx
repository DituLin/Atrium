import { act, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useOverviewDeadline } from './useOverviewDeadline';
import { overviewFixture } from '../test/overviewFixture';
import { projectOverview } from './overview';
afterEach(() => vi.useRealTimers());
it('wakes at a calibrated sub-second notice deadline and removes content offline', async () => {
 vi.useFakeTimers(); vi.setSystemTime(1000);
 const snapshot = overviewFixture(); const now = Date.parse(snapshot.generated_at);
 snapshot.sources.notice.items[0]!.valid_until = new Date(now + 250).toISOString();
 const dispatch = vi.fn(); const hook = renderHook(() => useOverviewDeadline(snapshot, now, now - 1000, dispatch));
 await act(() => vi.advanceTimersByTimeAsync(249)); expect(dispatch).not.toHaveBeenCalled();
 await act(() => vi.advanceTimersByTimeAsync(1)); expect(dispatch).toHaveBeenCalledWith({ type: 'app.tick', nowMs: 1250 });
 expect(projectOverview(snapshot.sources, now + 250, true).entries.some(entry => entry.kind === 'notice')).toBe(false);
 hook.unmount(); expect(vi.getTimerCount()).toBe(0);
});
it('cancelled snapshots cannot schedule a late deadline', async () => {
 vi.useFakeTimers(); const snapshot = overviewFixture(); const now = Date.parse(snapshot.generated_at);
 const dispatch = vi.fn(); const hook = renderHook(({ value }) => useOverviewDeadline(value, now, now - Date.now(), dispatch), { initialProps: { value: snapshot as typeof snapshot | null } });
 hook.rerender({ value: null }); await act(() => vi.advanceTimersByTimeAsync(60000)); expect(dispatch).not.toHaveBeenCalled();
});
