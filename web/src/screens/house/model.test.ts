import { expect, it } from 'vitest';
import { houseDashboardFromHome } from './model';
import { buildHouseChart } from './chart';
import type { HouseSeries } from './model';

const series = (samples: HouseSeries['samples']): HouseSeries => ({ samples, endAt: '2026-11-04T20:00:00Z', maxGapMs: 2 * 3600000, current: { value: 23, state: 'available' } });
it('localizes the actual provider weather wire in the House outdoor summary', () => {
  const model = houseDashboardFromHome({ schema_version: 1, server_time: '2026-11-04T20:00:00Z', version: 1,
    home: { name: 'Synthetic', timezone: 'UTC' }, widgets: [{ type: 'weather', payload: {
      provider: 'open-meteo', location_label: 'Synthetic', temperature_c: 18, condition_text: 'overcast', condition_code: 3,
      fetched_at: '2026-11-04T19:59:00Z', stale: false,
    } }] });
  expect(model.weather?.text).toBe('阴');
});
it('never fills missing room telemetry or device control from the design; uses only current real weather', () => {
  const value = houseDashboardFromHome({ schema_version: 1, server_time: '2026-11-04T20:00:00Z', version: 1, home: { name: 'Synthetic', timezone: 'UTC' }, widgets: [{ type: 'weather', payload: { provider: 'synthetic', location_label: 'Demo', temperature_c: 14, condition_text: '晴', condition_code: 'clear', fetched_at: '2026-11-04T19:59:00Z', stale: true } }] });
  expect(value.rooms).toEqual([]); expect(value.devices).toEqual([]); expect(value.aquarium).toBeNull(); expect(value.temperature).toBeNull();
  expect(value.weather).toMatchObject({ temperature: 14, text: '晴', updatedAt: '2026-11-04T19:59:00Z', stale: true });
  expect(houseDashboardFromHome(null).weather).toBeNull();
});

it('preserves the provider attribution and observation instant separately from the fetch instant', () => {
  const value = houseDashboardFromHome({ schema_version: 1, server_time: '2026-11-04T20:00:00Z', version: 1,
    home: { name: 'Synthetic', timezone: 'UTC' }, widgets: [{ type: 'weather', payload: {
      provider: 'qweather', location_label: 'Synthetic', temperature_c: 18, condition_text: '阴', condition_code: 3,
      observed_at: '2026-11-04T19:35:00Z', fetched_at: '2026-11-04T19:59:00Z', stale: false,
      attributions: ['示例气象机构'],
    } }] });
  expect(value.weather).toMatchObject({ provider: 'qweather', observedAt: '2026-11-04T19:35:00Z', updatedAt: '2026-11-04T19:59:00Z', attributions: ['示例气象机构'] });
});
it('breaks measured chart paths at explicit missing values and long sampling gaps', () => {
  const value = buildHouseChart(series([
    { at: '2026-11-04T00:00:00Z', value: 22 }, { at: '2026-11-04T01:00:00Z', value: 23 },
    { at: '2026-11-04T02:00:00Z', value: null }, { at: '2026-11-04T03:00:00Z', value: 24 },
    { at: '2026-11-04T04:00:00Z', value: 25 }, { at: '2026-11-04T10:00:00Z', value: 23 },
  ]));
  expect(value.paths).toHaveLength(3); expect(value.min).toBe(22); expect(value.max).toBe(25);
  expect(value.last?.x).toBeCloseTo(520 * 14 / 24);
  expect(value.paths[0]?.line).toMatch(/^M/); expect(value.paths[0]?.area).toMatch(/Z$/);
});
it('uses true timestamps, drops invalid/out-of-window values and keeps reference curves independent', () => {
  const value = buildHouseChart({ ...series([
    { at: '2026-11-03T19:00:00Z', value: 1000 }, { at: 'invalid', value: 1000 },
    { at: '2026-11-04T20:00:00Z', value: 25 }, { at: '2026-11-04T19:00:00Z', value: Number.NaN },
    { at: '2026-11-04T18:00:00Z', value: 22 },
  ]), reference: { label: '室外', samples: [{ at: '2026-11-04T18:00:00Z', value: 12 }, { at: '2026-11-04T20:00:00Z', value: 14 }] } });
  expect(value.min).toBe(22); expect(value.max).toBe(25); expect(value.reference).toHaveLength(1);
  expect(value.paths).toHaveLength(2); expect(value.last?.x).toBe(520); expect(value.low).toBeLessThan(12);
});
