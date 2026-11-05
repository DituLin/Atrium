import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { KOI_SOURCE, koiAlmanac, koiFromLunar, koiLunar, koiSolarTerms } from './index';
import type { CivilDate, KoiHour, KoiSolarTerm } from './index';
import lunarFixtures from './fixtures/lunar.json';
import ganzhiFixtures from './fixtures/ganzhi.json';
import solarFixtures from './fixtures/solar_terms.json';
import almanacFixtures from './fixtures/almanac.json';
import provenance from './provenance.json';

function civil(iso: string): CivilDate {
  const [year = 0, month = 0, day = 0] = iso.split('-').map(Number);
  return { year, month, day };
}

describe('unchanged Koi conformance fixtures', () => {
  it.each(lunarFixtures.cases)('lunar $input $note', ({ input, expected }) => {
    if (input.solar !== undefined) {
      const result = koiLunar(civil(input.solar));
      expect(result).not.toBeNull();
      expect(result && { lunarYear: result.year, month: result.month, day: result.day, isLeapMonth: result.leap }).toEqual(expected);
      expect([29, 30]).toContain(result?.monthLength);
    } else if (input.lunar !== undefined) {
      const { lunarYear, month, day, isLeapMonth } = input.lunar;
      expect(koiFromLunar({ year: lunarYear, month, day, leap: isLeapMonth })).toEqual(expected.solar ? civil(expected.solar) : null);
    }
  });

  it.each(ganzhiFixtures.cases)('ganzhi $input.solar $note', ({ input, expected }) => {
    const result = koiAlmanac(civil(input.solar));
    expect(result && { yearPillar: result.yearPillar, monthPillar: result.monthPillar, dayPillar: result.dayPillar, zodiac: result.zodiac }).toEqual(expected);
  });

  it.each(solarFixtures.cases)('solar terms $input.year', ({ input, expected }) => {
    const result = koiSolarTerms(input.year);
    expect(result.map(({ name, at }) => ({ name, at }))).toEqual(expected.terms);
    for (const item of result) expect({ year: item.year, month: item.month, day: item.day }).toEqual(civil(item.at.slice(0, 10)));
  });

  it.each(almanacFixtures.cases)('almanac $input.solar', ({ input, expected }) => {
    const result = koiAlmanac(civil(input.solar));
    expect(result).not.toBeNull();
    if (result) {
      const { lunar, ...fields } = result;
      expect(fields).toEqual(expected);
      expect(lunar).toEqual(koiLunar(civil(input.solar)));
    }
  });

  it('retains source JSON bytes, source revision and data versions', () => {
    expect(KOI_SOURCE.commit).toBe('f973da1b07a8bc3a387c202fd1a134f24924a3fc');
    expect(provenance.source.commit).toBe(KOI_SOURCE.commit);
    expect(provenance.files).toHaveLength(10);
    for (const file of provenance.files) {
      const bytes = readFileSync(new URL(file.path, import.meta.url));
      expect(bytes.length, file.path).toBe(file.bytes);
      expect(createHash('sha256').update(bytes).digest('hex'), file.path).toBe(file.sha256);
    }
  });
});

describe('civil date boundaries and public value isolation', () => {
  it.each(['1901-01-01', '1901-01-05', '1901-01-06', '2099-12-31'])('supports declared range boundary %s', iso => {
    expect(koiAlmanac(civil(iso))).not.toBeNull();
    expect(koiLunar(civil(iso))).not.toBeNull();
  });

  it('uses the preceding 大雪 month for early 1901 without inventing an absent term instant', () => {
    expect(koiAlmanac(civil('1901-01-01'))).toMatchObject({ yearPillar: '庚子', monthPillar: '戊子', dayPillar: '己卯', zodiac: '鼠' });
    expect(koiAlmanac(civil('1901-01-05'))?.monthPillar).toBe('戊子');
    expect(koiAlmanac(civil('1901-01-06'))?.monthPillar).toBe('己丑');
    expect(koiSolarTerms(1900)).toEqual([]);
  });

  // Independently verified with sxtwl 2.0.7, rather than the upstream reference
  // (which crashes before the first bundled 小寒 and has no fixtures there).
  it.each([
    ['1901-01-01', '戊子', '己卯'], ['1901-01-02', '戊子', '庚辰'],
    ['1901-01-03', '戊子', '辛巳'], ['1901-01-04', '戊子', '壬午'],
    ['1901-01-05', '戊子', '癸未'], ['1901-01-06', '己丑', '甲申'],
  ])('matches independent range-start pillars %s', (date, monthPillar, dayPillar) => {
    expect(koiAlmanac(civil(date!))).toMatchObject({ yearPillar: '庚子', monthPillar, dayPillar, zodiac: '鼠' });
  });

  it.each([
    { year: 1900, month: 12, day: 31 }, { year: 2100, month: 1, day: 1 },
    { year: 2026, month: 2, day: 29 }, { year: 2026, month: 13, day: 1 },
    { year: 2026, month: 0, day: 1 }, { year: 2026, month: 1, day: 0 },
    { year: 2026, month: 1, day: 32 }, { year: 2026, month: 1.5, day: 1 },
    { year: Number.NaN, month: 1, day: 1 }, { year: Number.POSITIVE_INFINITY, month: 1, day: 1 },
  ])('rejects invalid or out of range civil date $year-$month-$day', date => {
    expect(koiLunar(date)).toBeNull();
    expect(koiAlmanac(date)).toBeNull();
  });

  it.each([1900, 2100, 2026.5, Number.NaN, Number.POSITIVE_INFINITY])('has no solar terms outside bundled year %s', year => {
    expect(koiSolarTerms(year)).toEqual([]);
  });

  it.each([
    { year: 2026, month: 6, day: 1, leap: true },
    { year: 2026, month: 1, day: 0, leap: false },
    { year: 2026, month: 1, day: 31, leap: false },
    { year: 2026, month: 13, day: 1, leap: false },
    { year: 2026, month: 1, day: 1.5, leap: false },
    { year: 1900, month: 1, day: 1, leap: false },
    { year: Number.NaN, month: 1, day: 1, leap: false },
  ])('rejects nonexistent lunar date $year-$month-$day leap=$leap', lunar => {
    expect(koiFromLunar(lunar)).toBeNull();
  });

  it('keeps table-backed term days near midnight instead of approximating solar longitude', () => {
    expect(koiSolarTerms(2014).find(term => term.name === '惊蛰')).toEqual({ name: '惊蛰', year: 2014, month: 3, day: 6, at: '2014-03-06T00:02+08:00' });
    expect(koiSolarTerms(1923).find(term => term.name === '雨水')?.day).toBe(19);
  });

  it('separates the lunar new year from the 立春 pillar boundary', () => {
    const before = koiAlmanac(civil('2026-02-03'));
    const termDay = koiAlmanac(civil('2026-02-04'));
    const lunarNewYear = koiAlmanac(civil('2026-02-17'));
    expect(before).toMatchObject({ yearPillar: '乙巳', monthPillar: '己丑', zodiac: '蛇', lunar: { year: 2025 } });
    expect(termDay).toMatchObject({ yearPillar: '丙午', monthPillar: '庚寅', zodiac: '马', lunar: { year: 2025 } });
    expect(lunarNewYear).toMatchObject({ yearPillar: '丙午', lunar: { year: 2026, month: 1, day: 1 } });
  });

  it('returns fresh public arrays and records so callers cannot change subsequent dates', () => {
    const terms = koiSolarTerms(2026) as KoiSolarTerm[];
    const originalTerm = { ...terms[0] };
    if (terms[0]) terms[0].name = 'changed';
    terms.length = 0;
    expect(koiSolarTerms(2026)[0]).toMatchObject(originalTerm);
    const result = koiAlmanac(civil('2026-11-05'));
    expect(result).not.toBeNull();
    if (result) {
      (result.yi as string[]).push('changed');
      (result.hours as KoiHour[])[0]!.zhi = 'changed';
      result.lunar.day = 99;
    }
    expect(koiAlmanac(civil('2026-11-05'))).toMatchObject({ yi: ['收纳', '栽种', '立券', '纳畜', '纳财'], lunar: { day: 27 } });
    expect(koiAlmanac(civil('2026-11-05'))?.hours[0]?.zhi).toBe('子');
  });

  it('round-trips every civil day in the declared range, including partial 1900 and final 2099 lunar months', () => {
    const mismatches: string[] = [];
    let count = 0;
    for (let ms = Date.UTC(1901, 0, 1); ms <= Date.UTC(2099, 11, 31); ms += 86400000) {
      const date = new Date(ms);
      const expected = { year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate() };
      const lunar = koiLunar(expected);
      const result = lunar && koiFromLunar(lunar);
      if (!result || result.year !== expected.year || result.month !== expected.month || result.day !== expected.day) {
        mismatches.push(date.toISOString().slice(0, 10));
      }
      count += 1;
    }
    expect(count).toBe(72684);
    expect(mismatches).toEqual([]);
  });
});
