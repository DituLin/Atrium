import { expect, it } from 'vitest';
import { presentAlmanac } from './almanacPresentation';
function present(year: number, month: number, day: number) {
  return presentAlmanac({ year, month, day });
}

it('keeps leap-year day counts and ISO week years separate from Gregorian years', () => {
  expect(present(2024, 12, 31).dayOfYear).toBe(366);
  expect(present(2024, 12, 31).weekNumber).toBe(1);
  expect(present(2027, 1, 1).weekNumber).toBe(53);
  expect(present(2027, 1, 1).dayOfYear).toBe(1);
});

it('starts a new term interval on the actual solar-term date', () => {
  const before = present(2026, 10, 20);
  expect(before.term).toMatchObject({ previous: { name: '寒露', month: 10, day: 8 }, next: { name: '霜降', month: 10, day: 23 }, elapsed: 12, total: 15, remaining: 3, percent: 80 });
  const onTerm = present(2026, 10, 23);
  expect(onTerm.info?.today).toContain('霜降');
  expect(onTerm.term).toMatchObject({ previous: { name: '霜降' }, next: { name: '立冬' }, elapsed: 0, percent: 0 });
  expect(present(2027, 1, 1).term).toMatchObject({ previous: { name: '冬至', year: 2026 }, next: { name: '小寒', year: 2027 } });
});

it('marks the current day and festivals in a Monday-first week across months', () => {
  const result = present(2026, 10, 1);
  expect(result.week.map(day => day.key)).toEqual(['2026-09-28', '2026-09-29', '2026-09-30', '2026-10-01', '2026-10-02', '2026-10-03', '2026-10-04']);
  expect(result.week.filter(day => day.today)).toEqual([expect.objectContaining({ key: '2026-10-01', lunar: '国庆节', special: true })]);
  expect(result.week.filter(day => day.weekend).map(day => day.weekday)).toEqual(['六', '日']);
});

it('uses KOI pillars and full activity lists, changing year and month at the civil Lichun day', () => {
  const before = present(2026, 2, 3).info, after = present(2026, 2, 4).info;
  expect(before).toMatchObject({ year: '乙巳年', month: '己丑月', day: '戊申日' });
  expect(after).toMatchObject({ year: '丙午年', month: '庚寅月', day: '己酉日', ruleVersion: 'koi-almanac/1.0',
    yi: ['安床', '祭祀'], ji: ['冒险', '登高', '行船'] });
  expect(after?.today).not.toContain('春节');
});

it('uses the KOI table for a near-midnight solar term and the same week label', () => {
  const value = present(1923, 2, 19);
  expect(value.info?.today).toContain('雨水');
  expect(value.term).toMatchObject({ previous: { name: '雨水', day: 19 }, elapsed: 0 });
  expect(value.week.find(item => item.today)).toMatchObject({ lunar: '雨水', special: true });
});

it('handles KOI leap months and the lunar-year festival boundary without an older calendar fallback', () => {
  expect(present(2025, 7, 25).info?.lunar).toBe('闰六月初一');
  expect(present(2026, 2, 16).info?.today).toContain('除夕');
  expect(present(2026, 2, 17).info?.today).toContain('春节');
  expect(present(1900, 12, 31).info).toBeNull();
  expect(present(2100, 1, 1).info).toBeNull();
  expect(present(2100, 1, 1).week.every(day => day.lunar === '—')).toBe(true);
  expect(present(1901, 1, 1).term).toBeNull();
  expect(present(2099, 12, 31).term).toBeNull();
});
