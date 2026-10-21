import { describe, expect, it } from 'vitest';
import type { GrowthCurveSeries } from './growthCurveData';
import { ageMonthsAt, buildWhoReference, type GrowthReferenceProfile } from './whoGrowth';
import { whoLmsAt } from './whoGrowthData';

// All profiles and observations here are synthetic.
const profile: GrowthReferenceProfile = { entity_id: 'synthetic-child', birth_date: '2025-01-01', sex: 'male', height_measurement: 'length' };
function series(overrides: Partial<GrowthCurveSeries> = {}): GrowthCurveSeries {
    return { key: 'height-cm', metric: 'height', unit: 'cm', points: [{ date: '2026-01-01', value: '75.7391' }], latestDate: '2026-01-01', latestValue: '75.7391', previousChange: null, recentChange: null, recentFrom: null, recentTo: null, count: 1, complete: true, ...overrides };
}
const fixedNow = new Date('2025-08-01T12:00:00Z');

describe('WHO age and daily reference curves', () => {
    it('calculates exact household calendar age without shifting literal birthdays through timezone offsets', () => {
        expect(ageMonthsAt('2025-01-01', '2025-01-01', 'America/Los_Angeles')).toBe(0);
        expect(ageMonthsAt('2025-01-01T18:00:00Z', '2025-01-01', 'Asia/Singapore')).toBeCloseTo(1 / 30.4375, 12);
        expect(ageMonthsAt('2025-01-01T02:00:00Z', '2025-01-01', 'America/Los_Angeles')).toBeNull();
        expect(ageMonthsAt('2024-03-01', '2024-02-29', 'UTC')).toBeCloseTo(1 / 30.4375, 12);
    });
    it('refuses partial, invalid or unzoned dates and invalid timezones', () => {
        for (const date of ['2025', '2025-02-30', '2025-01-01T12:00:00']) expect(ageMonthsAt(date, '2025-01-01', 'UTC')).toBeNull();
        expect(ageMonthsAt('2025-03-01', '2025-02-30', 'UTC')).toBeNull();
        expect(ageMonthsAt('2025-03-01', '2025-01-01', 'not-a-zone')).toBeNull();
    });
    it('uses sex-specific birth and twelve-month anchors without rounding age to months', () => {
        for (const sex of ['male', 'female'] as const) for (const metric of ['height', 'weight'] as const) {
            const median = whoLmsAt(metric, sex, 365)![1];
            const ref = buildWhoReference(series({ metric, unit: metric === 'height' ? 'cm' : 'kg', latestValue: String(median), points: [{ date: '2026-01-01', value: String(median) }] }), metric, { ...profile, sex }, 'UTC');
            expect(ref.latestAgeDays).toBe(365);
            expect(ref.latestAgeMonths).toBeCloseTo(365 / 30.4375, 12);
            expect(ref.latestMedian).toBe(median);
            expect(ref.medianDifference).toBeCloseTo(0, 12);
            expect(ref.percentile).toBeCloseTo(50, 6);
            expect(ref.points[0]?.ageDays).toBe(0);
            expect(ref.points[0]?.p50).toBe(whoLmsAt(metric, sex, 0)![1]);
        }
    });
    it('matches P3 and P97 numerical anchors for both sexes at day 365', () => {
        const anchors = [
            ['male', 'height', 71.2704555722, 80.2077444278], ['female', 'height', 69.1625514668, 78.8472485332],
            ['male', 'weight', 7.8435603046, 11.8303743167], ['female', 'weight', 7.1401356061, 11.3305299922],
        ] as const;
        for (const [sex, metric, p3, p97] of anchors) {
            const ref = buildWhoReference(series({ metric, unit: metric === 'height' ? 'cm' : 'kg' }), metric, { ...profile, sex }, 'UTC');
            const point = ref.points.find(value => value.ageDays === 365);
            expect(point?.p3).toBeCloseTo(p3, 5); expect(point?.p97).toBeCloseTo(p97, 5);
        }
    });
    it('returns profile-based references without fabricating measurements when history is empty', () => {
        const ref = buildWhoReference(series({ points: [], latestValue: null, latestDate: null, count: 0 }), 'height', profile, 'UTC', fixedNow);
        expect(ref.points.length).toBeGreaterThan(200);
        expect(ref).toMatchObject({ latestAgeDays: 212, ageIsCurrent: true, latestMedian: null, medianDifference: null, percentile: null, recentMedianChange: null, comparisonAllowed: false });
        expect(ref.points[0]?.ageDays).toBe(0);
    });
    it('keeps a reference-only three-month boundary within the intended month window', () => {
        const data = series({ points: [], latestValue: null, latestDate: null, count: 0 });
        const ref = buildWhoReference(data, 'height', profile, 'UTC', new Date('2025-12-23T12:00:00Z'));
        expect(ref.latestAgeDays).toBe(356);
        expect(ref.latestAgeMonths).toBeCloseTo(356 / 30.4375, 12);
        expect(ref.points[ref.points.length - 1]?.ageDays).toBe(365);
        expect(ref.points[ref.points.length - 1]?.ageMonths).toBeLessThanOrEqual(12);
        expect(ref).toMatchObject({ ageIsCurrent: true, comparisonAllowed: false, latestMedian: null, percentile: null });
        expect(data.points).toEqual([]);
    });
    it('keeps missing or invalid profile data unavailable without inferring sex or birth date', () => {
        for (const invalid of [null, { ...profile, birth_date: '2025-02' }, { ...profile, birth_date: '2025-02-30' }, { ...profile, sex: undefined }, { ...profile, entity_id: '' }]) {
            const ref = buildWhoReference(series(), 'height', invalid as GrowthReferenceProfile | null, 'UTC');
            expect(ref).toMatchObject({ points: [], medianDifference: null, percentile: null, comparisonAllowed: false, referenceReason: 'profile_required' });
        }
    });
    it('bounds reference ages to 0–1856 and retains separate curve bases across 730–731 days', () => {
        const date = (age: number) => new Date(Date.parse('2025-01-01T00:00:00Z') + age * 86400000).toISOString().slice(0, 10);
        const latest = date(731), ref = buildWhoReference(series({ points: [{ date: latest, value: '88' }], latestDate: latest, latestValue: '88' }), 'height', { ...profile, height_measurement: undefined }, 'UTC');
        expect(ref.points.find(point => point.ageDays === 730)?.basis).toBe('length');
        expect(ref.points.find(point => point.ageDays === 731)?.basis).toBe('height');
        for (const age of [-1, 1857]) {
            const observed = date(age);
            expect(buildWhoReference(series({ points: [{ date: observed, value: '88' }], latestDate: observed, latestValue: '88' }), 'height', profile, 'UTC')).toMatchObject({ points: [], medianDifference: null, percentile: null, referenceReason: 'age_out_of_range' });
        }
    });
    it('withholds all WHO comparisons when an earlier record precedes birth in the household calendar', () => {
        const data = series({ points: [{ date: '2025-01-01T02:00:00Z', value: '49' }, { date: '2026-01-01', value: '75.7391' }], count: 2 });
        const ref = buildWhoReference(data, 'height', profile, 'America/Los_Angeles');
        expect(ref.points).toHaveLength(0);
        expect(ref).toMatchObject({ comparisonAllowed: false, medianDifference: null, percentile: null, referenceReason: 'record_before_birth' });
        expect(ref.note).toContain('早于出生日期');
        expect(buildWhoReference(data, 'height', profile, 'Asia/Singapore').points.length).toBeGreaterThan(0);
        expect(data.points).toHaveLength(2);
    });
});

describe('WHO comparisons', () => {
    it('shows reference lines but withholds height comparisons until measurement posture is confirmed', () => {
        const ref = buildWhoReference(series(), 'height', { ...profile, height_measurement: undefined }, 'UTC');
        expect(ref.points.length).toBeGreaterThan(0);
        expect(ref).toMatchObject({ latestAgeDays: 365, comparisonAllowed: false, medianDifference: null, percentile: null, latestWithinRange: null, recentMedianChange: null, referenceReason: 'height_measurement_required' });
    });
    it('converts standing measurements before day 731 into the WHO recumbent-length basis explicitly', () => {
        const value = 75.7391 - 0.7;
        const ref = buildWhoReference(series({ latestValue: String(value), points: [{ date: '2026-01-01', value: String(value) }] }), 'height', { ...profile, height_measurement: 'height' }, 'UTC');
        expect(ref.latestMedian).toBeCloseTo(value, 10); expect(ref.medianDifference).toBeCloseTo(0, 10);
        expect(ref.percentile).toBeCloseTo(50, 6); expect(ref.note).toContain('0.7 cm');
        expect(ref.points.find(point => point.ageDays === 365)?.p50).toBeCloseTo(value, 10);
    });
    it('converts recumbent length from day 731 into the WHO standing-height basis explicitly', () => {
        const observed = new Date(Date.parse('2025-01-01T00:00:00Z') + 731 * 86400000).toISOString().slice(0, 10);
        const value = whoLmsAt('height', 'male', 731)![1] + 0.7;
        const ref = buildWhoReference(series({ latestDate: observed, latestValue: String(value), points: [{ date: observed, value: String(value) }] }), 'height', profile, 'UTC');
        expect(ref.medianDifference).toBeCloseTo(0, 10); expect(ref.percentile).toBeCloseTo(50, 6); expect(ref.note).toContain('0.7 cm');
    });
    it('keeps standard values in the observed unit for supported height and weight units', () => {
        const grams = buildWhoReference(series({ metric: 'weight', unit: 'g', latestValue: '9646', points: [{ date: '2026-01-01', value: '9646' }] }), 'weight', profile, 'UTC');
        expect(grams.latestMedian).toBeCloseTo(9646, 8); expect(grams.percentile).toBeCloseTo(50, 6);
        const metres = buildWhoReference(series({ unit: 'm', latestValue: '0.757391', points: [{ date: '2026-01-01', value: '0.757391' }] }), 'height', profile, 'UTC');
        expect(metres.latestMedian).toBeCloseTo(0.757391, 10); expect(metres.percentile).toBeCloseTo(50, 6);
        expect(buildWhoReference(series({ unit: 'in' }), 'height', profile, 'UTC')).toMatchObject({ points: [], referenceReason: 'unit_unsupported' });
    });
    it('returns the normal-CDF percentile at P3/P50/P97 and withholds precision beyond ±3 SD', () => {
        const [l, m, s] = whoLmsAt('weight', 'male', 365)!;
        for (const [z, expected] of [[-1.8807936081512509, 3], [0, 50], [1.8807936081512509, 97]] as const) {
            const value = (m * Math.pow(1 + l * s * z, 1 / l)).toFixed(10);
            const ref = buildWhoReference(series({ metric: 'weight', unit: 'kg', latestValue: String(value), points: [{ date: '2026-01-01', value: String(value) }] }), 'weight', profile, 'UTC');
            expect(ref.percentile).toBeCloseTo(expected, 5);
        }
        const extreme = (m * Math.pow(1 + l * s * 4, 1 / l)).toFixed(10);
        expect(buildWhoReference(series({ metric: 'weight', unit: 'kg', latestValue: String(extreme), points: [{ date: '2026-01-01', value: String(extreme) }] }), 'weight', profile, 'UTC')).toMatchObject({ percentile: null, latestWithinRange: false, referenceReason: 'percentile_out_of_range' });
    });
    it('compares median growth across exactly the same observed dates as recent recorded growth', () => {
        const data = series({ latestDate: '2026-01-01', recentChange: '2', recentFrom: '2025-10-15', recentTo: '2026-01-01' });
        const ref = buildWhoReference(data, 'height', profile, 'UTC');
        const ageFrom = (Date.parse('2025-10-15') - Date.parse('2025-01-01')) / 86400000;
        expect(ref.recentMedianChange).toBeCloseTo(whoLmsAt('height', 'male', 365)![1] - whoLmsAt('height', 'male', ageFrom)![1], 10);
        expect(buildWhoReference(series(), 'height', profile, 'UTC').recentMedianChange).toBeNull();
    });
    it('does not claim comparison statistics for incomplete or nonpositive observations', () => {
        for (const data of [series({ complete: false }), series({ latestValue: '0' }), series({ latestValue: '-1' }), series({ latestValue: null })]) {
            expect(buildWhoReference(data, 'height', profile, 'UTC')).toMatchObject({ medianDifference: null, percentile: null, recentMedianChange: null, comparisonAllowed: false });
        }
    });
    it('rejects inherited unit names and does not treat an unknown runtime posture as confirmed', () => {
        for (const unit of ['constructor', 'toString', '__proto__']) expect(buildWhoReference(series({ unit }), 'height', profile, 'UTC')).toMatchObject({ points: [], comparisonAllowed: false, referenceReason: 'unit_unsupported' });
        const ref = buildWhoReference(series(), 'height', { ...profile, height_measurement: 'unconfirmed' } as unknown as GrowthReferenceProfile, 'UTC');
        expect(ref).toMatchObject({ comparisonAllowed: false, medianDifference: null, percentile: null, referenceReason: 'height_measurement_required' });
        expect(ref.points.find(point => point.ageDays === 365)?.p50).toBe(75.7391);
    });
});
