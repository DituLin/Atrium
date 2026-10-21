import { describe, expect, it } from 'vitest';
import type { ArchiveAnalysis } from '../../types/archives';
import type { GrowthData } from './growthTrend';
import { buildGrowthCurve } from './growthCurveData';

const growth = (groups: ArchiveAnalysis['groups'], complete = true): GrowthData => ({
    height: [{ data_epoch: 1, generation: 1, groups, coverage: { complete } } as ArchiveAnalysis], weight: [], limited: [],
});
const height = (date: string, value: string, unit = 'cm', sample?: string) => ({ metric: 'height', value, unit, date, sample });

describe('growth curve measurements', () => {
    it('preserves exact measurements and derives previous and observed-window changes', () => {
        const data = growth([height('2026-10-21', '72.600'), height('2026-08-01', '70.2'), height('2026-10-10', '71.9'), height('2026-07-20', '69.8')]);
        expect(buildGrowthCurve(data, 'height', 'Asia/Singapore')[0]).toMatchObject({
            unit: 'cm', latestValue: '72.600', latestDate: '2026-10-21', previousChange: '0.7',
            recentChange: '2.4', recentFrom: '2026-08-01', recentTo: '2026-10-21', complete: true, count: 4,
            points: [{ date: '2026-07-20', value: '69.8' }, { date: '2026-08-01', value: '70.2' }, { date: '2026-10-10', value: '71.9' }, { date: '2026-10-21', value: '72.600' }],
        });
    });
    it('does not mix units or sample types when finding changes', () => {
        const data = growth([height('2026-10-21', '72.6'), height('2026-10-10', '0.7', 'm'), height('2026-10-10', '71.9', 'cm', 'standing')]);
        const result = buildGrowthCurve(data, 'height', 'UTC');
        expect(result).toHaveLength(3);
        expect(new Set(result.map(series => series.key)).size).toBe(3);
        expect(result.every(series => series.previousChange === null && series.recentChange === null)).toBe(true);
    });
    it('anchors the window to the latest measurement and clamps a calendar month end', () => {
        const data = growth([height('2024-05-31', '73'), height('2024-02-29', '70'), height('2024-02-28', '69')]);
        expect(buildGrowthCurve(data, 'height', 'UTC')[0]).toMatchObject({ recentChange: '3', recentFrom: '2024-02-29', recentTo: '2024-05-31' });
    });
    it('orders measurements and selects the three-month window using the household calendar', () => {
        const data = growth([height('2026-10-13', '70.00'), height('2026-10-12T23:30:00-07:00', '69.00'), height('2026-07-12T23:30:00-07:00', '68')]);
        expect(buildGrowthCurve(data, 'height', 'America/Los_Angeles')[0]).toMatchObject({ latestValue: '70.00', previousChange: '1', recentChange: '1', recentFrom: '2026-10-12T23:30:00-07:00', recentTo: '2026-10-13' });
        expect(buildGrowthCurve(data, 'height', 'Asia/Singapore')[0]).toMatchObject({ latestValue: '69.00', recentChange: '1', recentFrom: '2026-07-12T23:30:00-07:00' });
    });
    it('does not extrapolate a baseline from before the three-month window', () => {
        const data = growth([height('2026-10-21', '72.6'), height('2026-07-20', '70.2')]);
        expect(buildGrowthCurve(data, 'height', 'UTC')[0]).toMatchObject({ previousChange: '2.4', recentChange: null, recentFrom: null, recentTo: null });
    });
    it('does not calculate a growth interval from one measurement or unavailable data', () => {
        expect(buildGrowthCurve(growth([height('2026-10-21', '72.6')]), 'height', 'UTC')[0]).toMatchObject({ latestValue: '72.6', previousChange: null, recentChange: null, count: 1 });
        expect(buildGrowthCurve(null, 'height', 'UTC')).toEqual([]);
        expect(buildGrowthCurve(growth([]), 'weight', 'UTC')).toEqual([]);
    });
    it('retains only proven points while coverage is incomplete, without claiming latest or changes', () => {
        const limited = growth([height('2026-10-21', '72.6')]); limited.limited = ['height'];
        for (const data of [growth([height('2026-10-21', '72.6'), height('2026-08-01', '70.2')], false), limited]) {
            const result = buildGrowthCurve(data, 'height', 'UTC')[0];
            expect(result).toMatchObject({ complete: false, latestValue: null, latestDate: null, previousChange: null, recentChange: null, recentFrom: null, recentTo: null });
            expect(result?.points.length).toBeGreaterThan(0);
        }
    });
    it('does not draw or summarize malformed dates, ambiguous duplicates or unsafe decimal measurements', () => {
        for (const groups of [
            [height('2026-10-21', '72.6'), height('2026-10-21', '72.7')],
            [height('2026-10-21T00:00:00Z', '72.6'), height('2026-10-21T08:00:00+08:00', '72.7')],
            [height('2026-02-30', '72.6')],
            [height('2026-10-21T12:00:00', '72.6')],
            [height('2026-10-21', '9007199254740992')],
        ]) expect(buildGrowthCurve(growth(groups), 'height', 'UTC')[0]).toMatchObject({ points: [], latestValue: null, latestDate: null, previousChange: null, recentChange: null, complete: false, count: 0 });
    });
    it('refuses an unsafe derived interval even when each measurement is representable', () => {
        const data = growth([height('2026-10-21', '9007199254740991'), height('2026-10-10', '0'), height('2026-08-01', '-9007199254740991')]);
        expect(buildGrowthCurve(data, 'height', 'UTC')[0]).toMatchObject({ latestValue: '9007199254740991', previousChange: '9007199254740991', recentChange: null, recentFrom: null, recentTo: null, complete: false });
    });
});
