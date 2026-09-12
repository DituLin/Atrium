import { act, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import { ApiClient } from '../core/api';
import { overviewFixture } from '../test/overviewFixture';
import { BriefingScreen } from './BriefingScreen';
function visibility(value: string) { Object.defineProperty(document, 'visibilityState', { configurable: true, value }); document.dispatchEvent(new Event('visibilitychange')); }
afterEach(() => { vi.useRealTimers(); visibility('visible'); });
function retained() {
 const state = createInitialState(1000); const snapshot = overviewFixture();
 const generatedAt = Date.parse(snapshot.generated_at);
 snapshot.sources.notice.items[0]!.valid_until = new Date(generatedAt + 500).toISOString();
 state.clock = { offsetMs: generatedAt - 1000, lastSyncAt: 1000 };
 state.overview = { snapshot, status: 'stale', refreshing: true, request: 1 };
 state.router = { ...state.router, route: { name: 'briefing' } };
 const loader = { load: () => new Promise<void>(() => {}), invalidate() {} };
 return <AppContext.Provider value={{ state, dispatch: vi.fn(), overview: loader, house: loader, api: new ApiClient(), clientVersion: 'test', goBack() {} }}><BriefingScreen /></AppContext.Provider>;
}
it('never commits an already expired retained notice on entry while refresh is pending', () => {
 vi.useFakeTimers(); vi.setSystemTime(2000);
 render(retained());
 // No timer is advanced: the first committed DOM must already enforce the deadline.
 expect(screen.queryByText('测试提示正文')).toBeNull();
});
it('withdraws a notice synchronously on foreground after background timers were paused', () => {
 vi.useFakeTimers(); vi.setSystemTime(1000); render(retained());
 expect(screen.getByText('测试提示正文')).toBeDefined();
 act(() => visibility('hidden'));
 vi.setSystemTime(2000); // Simulate suspended timers, not an ordinary second tick.
 act(() => {
  visibility('visible');
  // Check before React's act() boundary can flush a deferred state update.
  expect(screen.queryByText('测试提示正文')).toBeNull();
 });
});
