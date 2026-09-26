import { describe, expect, it } from 'vitest';
import { almanac, ganzhiDay, ganzhiYear, lunarDayName, lunarMonthName, solarTerms, toLunar } from './lunar';

const DAY_MS = 86400000;

describe('toLunar', () => {
  it('matches the ICU Chinese calendar on every day from 1900-01-31 to 2101-01-28', () => {
    const icu = new Intl.DateTimeFormat('en-u-ca-chinese', { timeZone: 'UTC', year: 'numeric', month: 'numeric', day: 'numeric' });
    const mismatches: string[] = [];
    for (let t = Date.UTC(1900, 0, 31); t <= Date.UTC(2101, 0, 28); t += DAY_MS) {
      const date = new Date(t);
      const parts = Object.fromEntries(icu.formatToParts(date).map(part => [part.type, part.value]));
      const month = parts.month ?? '';
      const expected = { year: Number(parts.relatedYear), month: parseInt(month, 10), day: Number(parts.day), leap: month.endsWith('bis') };
      const actual = toLunar(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate());
      if (!actual || actual.year !== expected.year || actual.month !== expected.month || actual.day !== expected.day || actual.leap !== expected.leap) {
        mismatches.push(date.toISOString().slice(0, 10));
        if (mismatches.length > 5) break;
      }
    }
    expect(mismatches).toEqual([]);
  });

  it('returns null outside the table', () => {
    expect(toLunar(1900, 1, 30)).toBeNull();
    expect(toLunar(2101, 1, 29)).toBeNull();
  });
});

describe('names', () => {
  it('writes months and days the traditional way', () => {
    expect(lunarMonthName({ year: 2026, month: 1, day: 1, leap: false, monthLength: 30 })).toBe('正月');
    expect(lunarMonthName({ year: 2025, month: 6, day: 1, leap: true, monthLength: 29 })).toBe('闰六月');
    expect(lunarMonthName({ year: 2026, month: 11, day: 1, leap: false, monthLength: 30 })).toBe('冬月');
    expect(lunarMonthName({ year: 2026, month: 12, day: 1, leap: false, monthLength: 30 })).toBe('腊月');
    expect([1, 10, 11, 19, 20, 21, 29, 30].map(lunarDayName)).toEqual(['初一', '初十', '十一', '十九', '二十', '廿一', '廿九', '三十']);
  });

  it('names stem-branch years and days', () => {
    expect(ganzhiYear(2026)).toEqual({ ganzhi: '丙午', zodiac: '马' });
    expect(ganzhiYear(1984)).toEqual({ ganzhi: '甲子', zodiac: '鼠' });
    expect(ganzhiDay(1949, 10, 1)).toBe('甲子');
    expect(ganzhiDay(1949, 10, 2)).toBe('乙丑');
    expect(ganzhiDay(1949, 9, 30)).toBe('癸亥');
  });
});

describe('solarTerms', () => {
  const find = (year: number, name: string) => solarTerms(year).find(term => term.name === name);

  it('places well-known terms on their published Beijing dates', () => {
    expect(find(2025, '冬至')).toMatchObject({ year: 2025, month: 12, day: 21 });
    expect(find(2026, '立春')).toMatchObject({ month: 2, day: 4 });
    expect(find(2026, '夏至')).toMatchObject({ month: 6, day: 21 });
    expect(find(2026, '秋分')).toMatchObject({ month: 9, day: 23 });
    expect(find(2024, '清明')).toMatchObject({ month: 4, day: 4 });
    expect(find(2023, '清明')).toMatchObject({ month: 4, day: 5 });
  });

  it('returns 24 terms in date order within the year', () => {
    for (const year of [1901, 1950, 2000, 2026, 2099]) {
      const terms = solarTerms(year);
      expect(terms).toHaveLength(24);
      expect(terms[0]?.name).toBe('小寒');
      expect(terms.every(term => term.year === year)).toBe(true);
    }
  });

  // Independent cross-check against ICU: a leap month is one without a
  // principal term (中气). A term and a new moon on the same Beijing day are
  // decided by their exact times, so only day 1 of a leap month may disagree.
  it('keeps principal terms out of leap months except same-day new-moon boundaries (1901–2099)', () => {
    const principal = new Set(['雨水', '春分', '谷雨', '小满', '夏至', '大暑', '处暑', '秋分', '霜降', '小雪', '冬至', '大寒']);
    const conflicts: string[] = [];
    for (let year = 1901; year <= 2099; year++) {
      for (const term of solarTerms(year)) {
        if (!principal.has(term.name)) continue;
        const lunar = toLunar(term.year, term.month, term.day);
        if (lunar?.leap && lunar.day !== 1) conflicts.push(`${year} ${term.name}`);
      }
    }
    expect(conflicts).toEqual([]);
  });
});

describe('almanac', () => {
  it('describes 2026-09-26', () => {
    expect(almanac(2026, 9, 26)).toEqual({
      lunar: '八月十六', year: '丙午马年', day: expect.stringMatching(/^..日$/),
      today: [], nextTerm: { name: '寒露', inDays: expect.any(Number) },
    });
  });

  it('marks festivals and the day of a solar term', () => {
    expect(almanac(2026, 2, 17)?.today).toContain('春节');
    expect(almanac(2026, 2, 16)?.today).toContain('除夕');
    expect(almanac(2026, 9, 25)?.today).toContain('中秋节');
    expect(almanac(2026, 10, 1)?.today).toContain('国庆节');
    expect(almanac(2026, 9, 23)?.today).toContain('秋分');
  });

  it('counts days to the next term across the year end', () => {
    const late = almanac(2026, 12, 30);
    expect(late?.nextTerm?.name).toBe('小寒');
    expect(late?.nextTerm?.inDays).toBeGreaterThan(0);
  });

  it('does not treat a leap month day as a festival', () => {
    // 2025 had 闰六月; 闰六月初七 is not 七夕.
    const leap7 = [...Array(40)].map((_, i) => new Date(Date.UTC(2025, 6, 25) + i * DAY_MS))
      .find(date => { const l = toLunar(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate()); return l?.leap && l.day === 7; })!;
    expect(almanac(leap7.getUTCFullYear(), leap7.getUTCMonth() + 1, leap7.getUTCDate())?.today).toEqual([]);
  });
});
