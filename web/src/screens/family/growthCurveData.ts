import type { GrowthData, GrowthMetric } from './growthTrend';
import { summarizeGrowth } from './dashboardStats';

export interface GrowthCurveSeries {
    key: string;
    metric: string;
    unit: string;
    currency?: string;
    sample?: string;
    points: { date: string; value: string }[];
    latestValue: string | null;
    latestDate: string | null;
    previousChange: string | null;
    recentChange: string | null;
    recentFrom: string | null;
    recentTo: string | null;
    complete: boolean;
    count: number;
}

function calendarDay(date: string, timezone: string): number {
    if (date.length === 10) return Date.parse(`${date}T00:00:00Z`);
    const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date(date));
    const part = (kind: string) => parts.find(value => value.type === kind)!.value;
    return Date.parse(`${part('year').padStart(4, '0')}-${part('month')}-${part('day')}T00:00:00Z`);
}

function threeMonthsBefore(day: number): number {
    const start = new Date(day), originalDay = start.getUTCDate();
    start.setUTCDate(1);
    start.setUTCMonth(start.getUTCMonth() - 3);
    const end = new Date(start);
    end.setUTCMonth(end.getUTCMonth() + 1);
    end.setUTCDate(0);
    start.setUTCDate(Math.min(originalDay, end.getUTCDate()));
    return start.getTime();
}

// Both operands were validated by summarizeGrowth. Recheck every scaled
// intermediate because a valid individual reading can still overflow a delta.
function difference(current: string, previous: string): string | null {
    const fraction = (value: string) => (value.split('.')[1] ?? '').length;
    const scale = Math.max(fraction(current), fraction(previous));
    const integer = (value: string) => Number(value.replace('.', '')) * Math.pow(10, scale - fraction(value));
    const a = integer(current), b = integer(previous), delta = a - b;
    if (!Number.isSafeInteger(a) || !Number.isSafeInteger(b) || !Number.isSafeInteger(delta)) return null;
    const digits = String(Math.abs(delta)).padStart(scale + 1, '0');
    const value = scale ? `${digits.slice(0, -scale)}.${digits.slice(-scale)}`.replace(/0+$/, '').replace(/\.$/, '') : digits;
    return `${delta < 0 ? '-' : ''}${value}`;
}

/**
 * The recent comparison uses actual observations within three calendar months
 * before the latest reading. Its returned dates must accompany the UI label:
 * there is no interpolated baseline, age inference or reference percentile.
 */
export function buildGrowthCurve(data: GrowthData | null, metric: GrowthMetric, timezone: string): GrowthCurveSeries[] {
    return summarizeGrowth(data, metric, timezone).map(series => {
        // Invalid values or ambiguous timestamps leave headline unavailable.
        // Such a group must not become a plausible-looking connected curve.
        const points = series.headline === null ? [] : series.points;
        const result: GrowthCurveSeries = {
            key: series.key, metric: series.metric, unit: series.unit, currency: series.currency, sample: series.sample,
            points, latestValue: series.complete ? series.headline : null, latestDate: series.complete ? series.latestDate ?? null : null,
            previousChange: series.complete ? series.change : null, recentChange: null, recentFrom: null, recentTo: null,
            complete: series.complete, count: points.length,
        };
        if (!series.complete || points.length < 2) return result;
        const latest = points[points.length - 1]!;
        const boundary = threeMonthsBefore(calendarDay(latest.date, timezone));
        const baseline = points.find(point => calendarDay(point.date, timezone) >= boundary);
        if (!baseline || baseline === latest) return result;
        const delta = difference(latest.value, baseline.value);
        if (delta === null) { result.complete = false; return result; }
        result.recentChange = delta; result.recentFrom = baseline.date; result.recentTo = latest.date;
        return result;
    });
}
