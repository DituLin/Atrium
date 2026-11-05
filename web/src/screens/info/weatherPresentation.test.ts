import { expect, it } from 'vitest';
import type { WeatherWidgetPayload } from '../../types/api';
import { presentWeather } from './weatherPresentation';

const now = Date.parse('2026-10-20T12:00:00Z');
const zone = { timezone: 'Asia/Singapore', utcOffsetSeconds: 28800 };
function weather(extra: Partial<WeatherWidgetPayload> = {}): WeatherWidgetPayload {
  return { provider: 'open-meteo', location_label: '示例城区', temperature_c: 18, condition_code: '2', condition_text: '多云',
    fetched_at: '2026-10-20T12:00:00Z', stale: false, ...extra };
}

it('does not turn unavailable optional values into plausible zeros or example measurements', () => {
  const result = presentWeather(weather({ temperature_c: null }), zone, now);
  expect(result.temperature).toBe('—'); expect(result.range).toBe('最高 — · 最低 —');
  expect(result.stats.map(stat => stat.value)).toEqual(['暂无', '—', '—', '—']);
  expect(result.hourly).toHaveLength(0); expect(result.days).toHaveLength(0);
});

it('uses the actual home timezone for sunset and hourly labels, including fixed-offset fallback', () => {
  const result = presentWeather(weather({ humidity_pct: 62, wind_speed_kmh: 8, wind_direction_deg: 45,
    sunset: '2026-10-20T09:24:00Z', hourly: [
      { time: '2026-10-20T11:00:00Z', temperature_c: 19 },
      { time: '2026-10-20T15:00:00Z', temperature_c: 16 },
      { time: '2026-10-20T18:00:00Z', temperature_c: 15 },
    ] }), { ...zone, timezone: 'Unsupported/Zone', timeZoneSupported: false }, now);
  expect(result.updated).toBe('20:00');
  expect(result.stats.map(stat => stat.value)).toEqual(['暂无', '62%', '东北 8 km/h', '17:24']);
  expect(result.hourly.map(point => point.time)).toEqual(['现在', '23时', '02时']);
  expect(result.hourly.map(point => point.temperature)).toEqual([18, 16, 15]);
});

it('discards invalid and duplicate hourly observations and never produces an invalid constant-temperature path', () => {
  const result = presentWeather(weather({ temperature_c: 18, hourly: [
    { time: 'invalid', temperature_c: 12 }, { time: '2026-10-20T15:00:00Z', temperature_c: NaN },
    { time: '2026-10-20T15:00:00Z', temperature_c: 18 }, { time: '2026-10-20T15:00:00Z', temperature_c: 18 },
    { time: '2026-10-20T18:00:00Z', temperature_c: 18 },
  ] }), zone, now);
  expect(result.hourly).toHaveLength(3);
  expect(result.line).not.toMatch(/NaN|Infinity|undefined/);
  expect(result.area).not.toMatch(/NaN|Infinity|undefined/);
  expect(result.hourly.every(point => point.x >= 0 && point.x <= 436 && point.y >= 0 && point.y <= 104)).toBe(true);
});

it('selects the next five actual provider dates and clamps the current marker to shared temperature bounds', () => {
  const result = presentWeather(weather({ forecast_timezone: 'America/Los_Angeles', temperature_c: 99, daily: [
    { date: '2026-10-19', min_c: 12, max_c: 18, condition_code: 2 },
    { date: '2026-10-20', min_c: 14, max_c: 22, condition_code: 2 },
    { date: '2026-10-21', min_c: 13, max_c: 23, condition_code: 0 },
    { date: '2026-10-22', min_c: 15, max_c: 21, condition_code: 3 },
    { date: '2026-10-23', min_c: 13, max_c: 17, condition_code: 61 },
    { date: '2026-10-24', min_c: 12, max_c: 19, condition_code: 2 },
    { date: '2026-10-25', min_c: 12, max_c: 18, condition_code: 2 },
    { date: '2026-10-21', min_c: 13, max_c: 23, condition_code: 0 },
    { date: '2026-10-26', min_c: 20, max_c: 10, condition_code: 0 },
  ] }), zone, now);
  expect(result.days.map(day => day.date)).toEqual(['2026-10-20', '2026-10-21', '2026-10-22', '2026-10-23', '2026-10-24']);
  expect(result.days[0]).toMatchObject({ label: '今天', low: '14°', high: '22°', current: 100 });
  expect(result.days.every(day => day.left >= 0 && day.left + day.width <= 100.001)).toBe(true);
});

it('matches provider calendar days even when home and forecast zones are on different dates', () => {
  const result = presentWeather(weather({ forecast_timezone: 'America/Los_Angeles', daily: [
    { date: '2026-10-19', min_c: 12, max_c: 18, condition_code: 2 },
    { date: '2026-10-20', min_c: 14, max_c: 22, condition_code: 2 },
  ] }), zone, Date.parse('2026-10-20T01:00:00Z'));
  expect(result.days[0]).toMatchObject({ date: '2026-10-19', label: '今天' });
});

it('does not label a retained stale current reading as a new observation now', () => {
  const result = presentWeather(weather({ stale: true, hourly: [
    { time: '2026-10-20T15:00:00Z', temperature_c: 16 }, { time: '2026-10-20T18:00:00Z', temperature_c: 15 },
  ], daily: [{ date: '2026-10-20', min_c: 14, max_c: 22, condition_code: 2 }] }), zone, now);
  expect(result.hourly.map(point => point.time)).toEqual(['23时', '02时']);
  expect(result.hourly.every(point => !point.current)).toBe(true);
  expect(result.days.every(day => day.current === null)).toBe(true);
});

it('uses a genuine observation instant for the current chart point rather than relabelling it as now', () => {
  const result = presentWeather(weather({ provider: 'qweather', observed_at: '2026-10-20T11:35:00Z', hourly: [
    { time: '2026-10-20T15:00:00Z', temperature_c: 16 }, { time: '2026-10-20T18:00:00Z', temperature_c: 15 },
  ] }), zone, now);
  expect(result.observed).toBe('19:35');
  expect(result.updated).toBe('20:00');
  expect(result.hourly[0]).toMatchObject({ timestamp: Date.parse('2026-10-20T11:35:00Z'), time: '实况', current: true });
  expect(result.hourly.some(point => point.time === '现在')).toBe(false);
});
