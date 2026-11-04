import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { WeatherWidgetPayload } from '../../types/api';
import { WeatherCard } from './WeatherCard';

const now = Date.parse('2026-10-20T12:00:00Z');
const options = { timezone: 'Asia/Singapore', utcOffsetSeconds: 28800 };
const payload: WeatherWidgetPayload = { provider: 'open-meteo', location_label: '示例城区', temperature_c: 18,
  condition_code: '2', condition_text: '多云', fetched_at: '2026-10-20T12:00:00Z', stale: false,
  temperature_min_c: 14, temperature_max_c: 22, humidity_pct: 62, wind_speed_kmh: 8, wind_direction_deg: 45,
  sunset: '2026-10-20T09:24:00Z', hourly: [
    { time: '2026-10-20T15:00:00Z', temperature_c: 16 }, { time: '2026-10-20T18:00:00Z', temperature_c: 15 },
  ], daily: [{ date: '2026-10-20', min_c: 14, max_c: 22, condition_code: 2 }, { date: '2026-10-21', min_c: 13, max_c: 23, condition_code: 0 }] };

it('renders genuine weather detail, hourly trend and future temperature ranges', () => {
  render(<WeatherCard weather={payload} options={options} now={now} />);
  const card = within(screen.getByRole('button', { name: '今日天气' }));
  expect(card.getByLabelText('当前温度').textContent).toBe('18°'); expect(card.getByText('最高 22° · 最低 14°')).toBeDefined();
  expect(card.getByText('示例城区 · 20:00 更新')).toBeDefined();
  expect(card.getByLabelText('小时温度变化').getAttribute('role')).toBe('img');
  expect(card.getByRole('list', { name: '未来天气' }).querySelectorAll('[role="listitem"]')).toHaveLength(2);
  expect(card.getByText('17:24')).toBeDefined(); expect(card.queryByText('优 38')).toBeNull();
});

it('keeps an explicit empty weather card with honest metric and forecast placeholders', () => {
  render(<WeatherCard weather={null} options={options} now={now} />);
  expect(screen.getByText('暂无天气数据')).toBeDefined();
  expect(screen.getByText('暂无小时预报')).toBeDefined(); expect(screen.getByText('暂无未来天气预报')).toBeDefined();
  expect(screen.queryByText('18°')).toBeNull(); expect(screen.queryByText(/杭州/)).toBeNull();
});

it('marks stale observations and provides a genuine refresh action for remote OK', () => {
  const refresh = vi.fn(); render(<WeatherCard weather={{ ...payload, stale: true }} options={options} now={now} onClick={refresh} />);
  expect(screen.getByText('示例城区 · 旧数据 · 20:00 更新')).toBeDefined();
  fireEvent.keyDown(screen.getByRole('button', { name: '今日天气' }), { key: 'Enter' });
  expect(refresh).toHaveBeenCalledTimes(1);
});

it.each([
  [0, 'clear', '晴'], [2, 'partly cloudy', '多云'], [3, 'overcast', '阴'],
  [45, 'fog', '雾'], [61, 'rain', '雨'], [95, 'thunderstorm', '雷雨'],
])('displays Chinese for genuine numeric WMO code %s and English provider text', (code, text, expected) => {
  render(<WeatherCard weather={{ ...payload, condition_code: code, condition_text: text }} options={options} now={now} />);
  expect(screen.getByText(expected)).toBeDefined(); expect(screen.queryByText(text)).toBeNull();
});

it.each([
  [null, 'partly_cloudy', '多云'], ['rain', null, '雨'], [null, 'cloudy', '多云'],
  [null, 'freezing rain', '冻雨'],
])('localizes legacy English weather fallback %s / %s', (code, text, expected) => {
  render(<WeatherCard weather={{ ...payload, condition_code: code, condition_text: text }} options={options} now={now} />);
  expect(screen.getByText(expected)).toBeDefined();
});

it('preserves a user-provided Chinese condition description', () => {
  render(<WeatherCard weather={{ ...payload, condition_code: 2, condition_text: ' 多云转晴 ' }} options={options} now={now} />);
  expect(screen.getByText('多云转晴')).toBeDefined();
});

it('does not turn unknown weather into sunshine', () => {
  render(<WeatherCard weather={{ ...payload, condition_code: 1234, condition_text: 'unknown' }} options={options} now={now} />);
  expect(screen.getByText('天气待确认')).toBeDefined(); expect(screen.queryByText('晴')).toBeNull();
  expect(screen.getByRole('button', { name: '今日天气' }).querySelector('.today-weather__condition .today-weather__icon--unknown')).not.toBeNull();
});
