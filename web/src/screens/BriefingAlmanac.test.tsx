import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import { ApiClient } from '../core/api';
import { overviewFixture } from '../test/overviewFixture';
import { BriefingScreen } from './BriefingScreen';

afterEach(() => { vi.useRealTimers(); });

/** Server time is `iso`; the family lives in Asia/Singapore (UTC+8). */
function briefing(iso: string, nowMs = Date.parse(iso)) {
  const state = createInitialState(nowMs);
  const snapshot = overviewFixture();
  state.clock = { offsetMs: 0, lastSyncAt: nowMs };
  state.overview = { snapshot, status: 'available', refreshing: false, request: null };
  state.router = { ...state.router, route: { name: 'briefing' } };
  const loader = { load: async () => {}, invalidate() {} };
  return <AppContext.Provider value={{ state, dispatch: vi.fn(), overview: loader, house: loader, api: new ApiClient(), clientVersion: 'test', goBack() {} }}><BriefingScreen /></AppContext.Provider>;
}

function card() { return within(screen.getByRole('region', { name: '万年历' })); }

it('shows the Gregorian and lunar date of the family day', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-09-26T04:00:00Z'));
  render(briefing('2026-09-26T04:00:00Z'));
  expect(screen.getByText('9月26日 · 星期六')).toBeDefined();
  expect(card().getByText('26')).toBeDefined();
  expect(card().getByText('农历八月十六')).toBeDefined();
  expect(card().getByText('丙午马年 · 癸卯日')).toBeDefined();
  expect(card().getByText('距寒露还有 12 天')).toBeDefined();
  expect(card().queryByRole('list', { name: '今日节日与节气' })).toBeNull();
});

it('rolls the almanac over at family midnight, not UTC midnight', () => {
  vi.useFakeTimers();
  // 23:59:59 in Singapore is still 26 September for the family.
  vi.setSystemTime(Date.parse('2026-09-26T15:59:59Z'));
  const { rerender } = render(briefing('2026-09-26T15:59:59Z'));
  expect(card().getByText('农历八月十六')).toBeDefined();
  vi.setSystemTime(Date.parse('2026-09-26T16:00:01Z'));
  rerender(briefing('2026-09-26T16:00:01Z'));
  expect(card().getByText('农历八月十七')).toBeDefined();
  expect(screen.getByText('9月27日 · 星期日')).toBeDefined();
});

it('marks festivals of the day as chips', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-09-25T02:00:00Z'));
  render(briefing('2026-09-25T02:00:00Z'));
  const chips = card().getByRole('list', { name: '今日节日与节气' });
  expect(within(chips).getByText('中秋节')).toBeDefined();
  expect(card().getByText('农历八月十五')).toBeDefined();
});

it('does not invent a family date before any timezone is known', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-09-26T04:00:00Z'));
  const element = briefing('2026-09-26T04:00:00Z');
  element.props.value.state.overview = { snapshot: null, status: 'loading', refreshing: false, request: null };
  render(element);
  expect(card().getByText('等待家庭时间')).toBeDefined();
  expect(screen.queryByText(/农历/)).toBeNull();
});
