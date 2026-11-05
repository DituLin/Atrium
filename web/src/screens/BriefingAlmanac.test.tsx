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

function card() { return within(screen.getByRole('button', { name: '万年历' })); }

it('shows the Gregorian and lunar date of the family day', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-09-26T04:00:00Z'));
  render(briefing('2026-09-26T04:00:00Z'));
  expect(screen.getByText('万年历 · 天气')).toBeDefined();
  expect(card().getByTestId('almanac-date').textContent).toBe('26');
  expect(card().getByLabelText('农历八月十六')).toBeDefined();
  expect(card().getByText('第 39 周 · 第 269 天')).toBeDefined();
  expect(card().getByText('丙午年')).toBeDefined();
  expect(card().getByText('丁酉月')).toBeDefined();
  expect(card().getByText('癸卯日')).toBeDefined();
  expect(card().getByText('距寒露还有 12 天')).toBeDefined();
  expect(card().queryByRole('list', { name: '今日节日与节气' })).toBeNull();
});

it('rolls the almanac over at family midnight, not UTC midnight', () => {
  vi.useFakeTimers();
  // 23:59:59 in Singapore is still 26 September for the family.
  vi.setSystemTime(Date.parse('2026-09-26T15:59:59Z'));
  const { rerender } = render(briefing('2026-09-26T15:59:59Z'));
  expect(card().getByLabelText('农历八月十六')).toBeDefined();
  vi.setSystemTime(Date.parse('2026-09-26T16:00:01Z'));
  rerender(briefing('2026-09-26T16:00:01Z'));
  expect(card().getByLabelText('农历八月十七')).toBeDefined();
  expect(card().getByTestId('almanac-date').textContent).toBe('27');
  expect(card().getByText('星期日')).toBeDefined();
});

it('marks festivals of the day as chips', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-09-25T02:00:00Z'));
  render(briefing('2026-09-25T02:00:00Z'));
  const chips = card().getByRole('list', { name: '今日节日与节气' });
  expect(within(chips).getByText('中秋节')).toBeDefined();
  expect(card().getByLabelText('农历八月十五')).toBeDefined();
});

it('does not invent a family date before any timezone is known', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-09-26T04:00:00Z'));
  const element = briefing('2026-09-26T04:00:00Z');
  element.props.value.state.overview = { snapshot: null, status: 'loading', refreshing: false, request: null };
  render(element);
  expect(card().getByText('等待家庭时间')).toBeDefined();
  expect(screen.queryByText(/农历/)).toBeNull();
});

it('shows the current Monday-to-Sunday week across a year boundary', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2027-01-01T04:00:00Z'));
  render(briefing('2027-01-01T04:00:00Z'));
  expect(card().getByText('第 53 周 · 第 1 天')).toBeDefined();
  expect(card().getByText('丙午年')).toBeDefined();
  const week = within(card().getByRole('list', { name: '本周日历' }));
  expect(week.getAllByRole('listitem').map(item => item.getAttribute('data-date')))
    .toEqual(['2026-12-28', '2026-12-29', '2026-12-30', '2026-12-31', '2027-01-01', '2027-01-02', '2027-01-03']);
  expect(week.getAllByRole('listitem').filter(item => item.getAttribute('aria-current') === 'date')).toHaveLength(1);
});

it('fills the customary columns from the pinned KOI rules and retains the actual term progress', () => {
  vi.useFakeTimers(); vi.setSystemTime(Date.parse('2026-10-20T04:00:00Z'));
  render(briefing('2026-10-20T04:00:00Z'));
  // Fixed outputs from Koi f973da1 tools/almanac_ref.py, rather than the UI engine.
  const activities = (name: string) => within(card().getByRole('list', { name })).getAllByRole('listitem').map(item => item.textContent);
  expect(activities('宜')).toEqual(['交易', '修造', '嫁娶', '捕捉', '立券', '纳畜']);
  expect(activities('忌')).toEqual(['开市', '搬家']);
  expect(card().queryByText(/KOI|koi-almanac/)).toBeNull();
  expect(card().queryByText('暂无资料')).toBeNull();
  const progress = card().getByRole('progressbar', { name: '节气进度' });
  expect(progress.getAttribute('aria-valuenow')).toBe('12');
  expect(progress.getAttribute('aria-valuemax')).toBe('15');
  expect(card().getByText('寒露 10/8')).toBeDefined();
  expect(card().getByText('霜降 10/23')).toBeDefined();
});
