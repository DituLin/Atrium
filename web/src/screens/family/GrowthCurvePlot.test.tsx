import { render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { GrowthCurvePlot } from './GrowthCurvePlot';
import type { WhoReference } from './whoGrowth';
import { buildWhoReference } from './whoGrowth';

it('plots every proven measurement with a highlighted latest point and right-side value axis', () => {
    const { container } = render(<GrowthCurvePlot points={[{ date: '2026-07-13', value: '70.2' }, { date: '2026-08-13', value: '71.0' }, { date: '2026-10-13', value: '72.6' }]} label="合成宝宝身高生长曲线" unit="cm" timezone="Asia/Singapore"/>);
    expect(screen.getByRole('img', { name: '合成宝宝身高生长曲线' })).toBeTruthy();
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(3);
    expect(container.querySelectorAll('[data-latest]')).toHaveLength(1);
    expect(container.querySelector('[data-curve]')?.getAttribute('d')).toContain('C');
    expect(container.querySelector('[data-area]')).toBeTruthy();
    expect(container.querySelector('[data-axis="y"] text')?.getAttribute('x')).toBe('918');
    expect(container.querySelector('[data-axis="x"]')?.textContent).toContain('7月13日');
    expect(container.querySelector('[data-axis="x"]')?.textContent).toContain('10月13日');
});

it('retains a single measurement without inventing a historical curve', () => {
    const { container } = render(<GrowthCurvePlot points={[{ date: '2026-10-13', value: '72.6' }]} label="单次身高" unit="cm" timezone="UTC"/>);
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(1);
    expect(container.querySelector('[data-area]')).toBeNull();
    expect(container.querySelector('[data-axis="x"]')?.textContent).toBe('10月13日');
    expect(container.innerHTML).not.toMatch(/NaN|Infinity/);
});

it('rejects malformed or unsafe chart coordinates and preserves household date labels', () => {
    const { container } = render(<GrowthCurvePlot points={[{ date: '2026-02-30', value: '70' }, { date: '2026-10-12T17:00:00Z', value: '72.6' }, { date: '2026-10-14', value: '9007199254740992' }, { date: '2026-10-15', value: '' }]} label="按家庭日期绘图" unit="cm" timezone="Asia/Singapore"/>);
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(1);
    expect(container.querySelector('[data-axis="x"]')?.textContent).toBe('10月13日');
    expect(container.innerHTML).not.toMatch(/NaN|Infinity/);
});

it('draws verified WHO bands in separate posture segments and labels age instead of dates', () => {
    const reference: WhoReference = {
        points: [
            { date: '2025-12-21', ageDays: 720, ageMonths: 720 / 30.4375, p3: 76, p50: 81, p97: 86, basis: 'length' },
            { date: '2025-12-31', ageDays: 730, ageMonths: 730 / 30.4375, p3: 76.5, p50: 81.5, p97: 86.5, basis: 'length' },
            { date: '2026-01-01', ageDays: 731, ageMonths: 731 / 30.4375, p3: 75.8, p50: 80.8, p97: 85.8, basis: 'height' },
            { date: '2026-01-10', ageDays: 740, ageMonths: 740 / 30.4375, p3: 76, p50: 81, p97: 86, basis: 'height' },
        ],
        latestAgeDays: 740, latestAgeMonths: 740 / 30.4375, latestMedian: 81, medianDifference: -1,
        percentile: 40, recentMedianChange: 0, latestWithinRange: true, comparisonAllowed: true,
        basis: 'height', note: '合成绘图夹具', referenceReason: null, ageIsCurrent: false,
    };
    const { container } = render(<GrowthCurvePlot points={[{ date: '2025-12-21', value: '79' }, { date: '2026-01-10', value: '80' }]} label="有 WHO 参考的合成曲线" unit="cm" timezone="UTC" reference={reference} birthDate="2024-01-01"/>);
    expect(container.querySelectorAll('[data-who-band]')).toHaveLength(2);
    expect(container.querySelectorAll('[data-who-median]')).toHaveLength(2);
    expect(container.querySelector('[data-who-median][data-basis="length"]')).toBeTruthy();
    expect(container.querySelector('[data-who-median][data-basis="height"]')).toBeTruthy();
    expect(container.querySelector('[data-axis="x"]')?.textContent).toContain('月龄');
    expect(container.querySelector('[data-who-difference]')?.textContent).toBe('-1 cm');
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(2);
    expect(container.innerHTML).not.toMatch(/NaN|Infinity/);
});

it('aligns an 11.7-month reference axis to three-month ticks without adding a future measurement', () => {
    const points = [{ date: '2026-12-23', value: '72.6' }];
    const reference = buildWhoReference({ key: 'synthetic-height', metric: 'height', unit: 'cm', points, latestValue: '72.6', latestDate: '2026-12-23', previousChange: null, recentChange: null, recentFrom: null, recentTo: null, complete: true, count: 1 }, 'height', { entity_id: 'synthetic-person', birth_date: '2026-01-01', sex: 'female', height_measurement: 'length' }, 'UTC');
    const { container } = render(<GrowthCurvePlot points={points} label="整齐的月龄坐标" unit="cm" timezone="UTC" reference={reference} birthDate="2026-01-01"/>);
    expect([...container.querySelectorAll('[data-axis="x"] text')].map(tick => tick.textContent)).toEqual(['0 月龄', '3 月龄', '6 月龄', '9 月龄', '12 月龄']);
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(1);
    expect(Number(container.querySelector('[data-latest]')?.getAttribute('cx'))).toBeLessThan(840);
    expect(reference.points[reference.points.length - 1]!.date).toBe('2026-12-23');
});
