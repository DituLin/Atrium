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
 const lastTick = Date.parse('2026-10-20T15:59:59Z');
 const state = createInitialState(lastTick); const snapshot = overviewFixture();
 state.clock = { offsetMs: 0, lastSyncAt: lastTick };
 state.overview = { snapshot, status: 'stale', refreshing: true, request: 1 };
 state.router = { ...state.router, route: { name: 'briefing' } };
 const loader = { load: () => new Promise<void>(() => {}), invalidate() {} };
 return <AppContext.Provider value={{ state, dispatch: vi.fn(), overview: loader, house: loader, api: new ApiClient(), clientVersion: 'test', goBack() {} }}><BriefingScreen /></AppContext.Provider>;
}
it('renders the current family day on entry before the next global tick or pending refresh', () => {
 vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-10-20T16:00:01Z'));
 render(retained());
 expect(screen.getByTestId('almanac-date').textContent).toBe('21');
});
it('rolls the calendar over synchronously on foreground after background timers were paused', () => {
 vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-10-20T15:59:59Z')); render(retained());
 expect(screen.getByTestId('almanac-date').textContent).toBe('20');
 act(() => visibility('hidden'));
 vi.setSystemTime(Date.parse('2026-10-20T16:00:01Z'));
 act(() => {
  visibility('visible');
  expect(screen.getByTestId('almanac-date').textContent).toBe('21');
 });
});
