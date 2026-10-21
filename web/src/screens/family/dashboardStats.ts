import type { ArchiveAnalysis } from '../../types/archives';
import type { MilkPeriod, MilkTrendData } from './feedingTrend';
import type { FinanceTrendData } from './financeTrend';
import type { GrowthData } from './growthTrend';

export interface DashboardSeries {
    key: string;
    metric: string;
    unit: string;
    currency?: string;
    sample?: string;
    points: { date: string; value: string }[];
    headline: string | null;
    change: string | null;
    changePercent: string | null;
    latestDate?: string;
    latestValue?: string;
    count: number;
    recorded: number;
    expected: number;
    complete: boolean;
}

type Group = ArchiveAnalysis['groups'][number];
interface Decimal { integer: number; scale: number }

// Chrome 66 has no BigInt. Every integer intermediate is checked before use;
// unsupported precision makes the statistic unavailable instead of rounding
// a large Core decimal through binary floating point.
function decimal(value: string): Decimal | null {
    if (!/^[+-]?\d+(?:\.\d+)?$/.test(value)) return null;
    const fraction = value.split('.')[1] ?? '';
    if (fraction.length > 15) return null;
    const integer = Number(value.replace('.', ''));
    return Number.isSafeInteger(integer) ? { integer, scale: fraction.length } : null;
}
function aligned(a: Decimal, b: Decimal): [number, number, number] | null {
    const scale = Math.max(a.scale, b.scale);
    const left = a.integer * Math.pow(10, scale - a.scale), right = b.integer * Math.pow(10, scale - b.scale);
    return Number.isSafeInteger(left) && Number.isSafeInteger(right) ? [left, right, scale] : null;
}
function add(a: Decimal, b: Decimal, subtract = false): Decimal | null {
    const pair = aligned(a, b);
    if (!pair) return null;
    const integer = pair[0] + (subtract ? -pair[1] : pair[1]);
    return Number.isSafeInteger(integer) ? { integer, scale: pair[2] } : null;
}
function text(value: Decimal): string {
    const digits = String(Math.abs(value.integer)).padStart(value.scale + 1, '0');
    const number = value.scale ? `${digits.slice(0, -value.scale)}.${digits.slice(-value.scale)}`.replace(/0+$/, '').replace(/\.$/, '') : digits;
    return `${value.integer < 0 ? '-' : ''}${number}`;
}
/** Derived means and percentages use decimal half-up rounding to 2 places. */
function roundedRatio(numerator: number, denominator: number, scale = 2): string | null {
    if (!Number.isSafeInteger(numerator) || !Number.isSafeInteger(denominator) || denominator <= 0) return null;
    const magnitude = Math.abs(numerator);
    let rounded = Math.floor(magnitude / denominator);
    if (magnitude % denominator >= Math.ceil(denominator / 2)) rounded++;
    return Number.isSafeInteger(rounded) ? text({ integer: numerator < 0 ? -rounded : rounded, scale }) : null;
}
function mean(values: string[]): string | null {
    if (values.length === 1) return decimal(values[0]!) ? values[0]! : null;
    let total: Decimal = { integer: 0, scale: 0 };
    for (const value of values) {
        const parsed = decimal(value);
        const sum = parsed ? add(total, parsed) : null;
        if (!sum) return null;
        total = sum;
    }
    const numerator = total.integer * Math.pow(10, Math.max(0, 2 - total.scale));
    const denominator = values.length * Math.pow(10, Math.max(0, total.scale - 2));
    return roundedRatio(numerator, denominator);
}
function difference(current: string, previous: string): { change: string | null; percent: string | null; safe: boolean } {
    const a = decimal(current), b = decimal(previous);
    const delta = a && b ? add(a, b, true) : null;
    if (!a || !b || !delta) return { change: null, percent: null, safe: false };
    const pair = aligned(delta, b);
    if (!pair) return { change: text(delta), percent: null, safe: false };
    if (pair[1] === 0) return { change: text(delta), percent: null, safe: true };
    const percent = roundedRatio(pair[0] * 10000, Math.abs(pair[1]));
    return { change: text(delta), percent, safe: percent !== null };
}
function dateTime(value: string | undefined): number | null {
    if (!value || !/^\d{4}-\d{2}-\d{2}(?:$|T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$)/.test(value)) return null;
    const day = value.slice(0, 10), midnight = Date.parse(`${day}T00:00:00Z`);
    if (!Number.isFinite(midnight) || new Date(midnight).toISOString().slice(0, 10) !== day) return null;
    const time = Date.parse(value);
    return Number.isFinite(time) ? time : null;
}
function calendarTime(value: string, timezone: string): number {
    const time = dateTime(value)!;
    if (value.length === 10) return time;
    const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(time);
    const part = (kind: string) => parts.find(p => p.type === kind)!.value;
    return dateTime(`${part('year')}-${part('month')}-${part('day')}`)!;
}
function key(group: Group): string { return JSON.stringify([group.metric, group.unit, group.currency, group.sample]); }
function split(groups: Group[], complete: boolean, expected: number): { series: DashboardSeries; groups: Group[]; valid: boolean }[] {
    const sets = new Map<string, Group[]>();
    for (const group of groups) { const groupKey = key(group); const values = sets.get(groupKey) ?? []; values.push(group); sets.set(groupKey, values); }
    return [...sets].map(([groupKey, values]) => {
        const first = values[0]!;
        const valid = values.every(group => decimal(group.value) !== null && dateTime(group.date) !== null);
        const points = values.filter(group => decimal(group.value) !== null && dateTime(group.date) !== null).map(group => ({ date: group.date!, value: group.value })).sort((a, b) => dateTime(a.date)! - dateTime(b.date)!);
        const latest = points[points.length - 1];
        return { groups: values, valid, series: { key: groupKey, metric: first.metric, unit: first.unit ?? '', currency: first.currency, sample: first.sample, points, headline: null, change: null, changePercent: null, latestDate: latest?.date, latestValue: latest?.value, count: values.length, recorded: new Set(points.map(point => point.date.slice(0, 10))).size, expected, complete: complete && valid } };
    });
}
export function summarizeMilk(data: MilkTrendData | null, period: MilkPeriod): DashboardSeries[] {
    if (!data) return [];
    const buckets = split(data.groups, data.complete, period === 'day' ? 1 : data.dates.length);
    if (period === 'day') {
        // A sum can exist with no drawable timestamp; retain its proven value.
        for (const total of data.totals ?? []) if (!buckets.some(bucket => bucket.series.key === key(total))) {
            buckets.push({ groups: [], valid: true, series: { key: key(total), metric: total.metric, unit: total.unit ?? '', currency: total.currency, sample: total.sample, points: [], headline: null, change: null, changePercent: null, count: 0, recorded: 0, expected: 1, complete: data.complete } });
        }
    }
    return buckets.map(({ series, groups, valid }) => {
        if (period === 'day') {
            const totals = (data.totals ?? []).filter(total => key(total) === series.key);
            const total = totals[0];
            series.headline = valid && totals.length === 1 && total && decimal(total.value) ? total.value : null;
            series.recorded = groups.length ? 1 : 0;
        } else {
            const unique = new Set(groups.map(group => group.date));
            const daysValid = valid && unique.size === groups.length && groups.every(group => data.dates.includes(group.date!));
            series.headline = daysValid ? mean(groups.map(group => group.value)) : null;
        }
        series.complete = series.complete && series.headline !== null;
        return series;
    });
}
export function summarizeFinance(data: FinanceTrendData | null): DashboardSeries[] {
    if (!data) return [];
    const currentMonth = data.months[data.months.length - 1], previousMonth = data.months[data.months.length - 2];
    return split(data.groups, data.complete, data.months.length).map(({ series, groups, valid }) => {
        const unique = new Set(groups.map(group => group.date));
        const monthsValid = valid && unique.size === groups.length && groups.every(group => data.months.includes(group.date!.slice(0, 7)) && group.date!.slice(7) === '-01');
        const current = groups.find(group => group.date === `${currentMonth}-01`), previous = groups.find(group => group.date === `${previousMonth}-01`);
        series.complete = series.complete && monthsValid;
        if (!monthsValid || !current) return series;
        series.headline = current.value;
        if (previous) {
            const change = difference(current.value, previous.value);
            series.change = change.change; series.changePercent = change.percent; series.complete = series.complete && change.safe;
        }
        return series;
    });
}
export function summarizeGrowth(data: GrowthData | null, metric: 'height' | 'weight', timezone = 'UTC'): DashboardSeries[] {
    if (!data) return [];
    const complete = !data.limited.includes(metric) && data[metric].every(result => result.coverage.complete);
    return split(data[metric].reduce<Group[]>((groups, result) => groups.concat(result.groups), []).filter(group => group.metric === metric), complete, 0).map(({ series, valid }) => {
        series.points.sort((a, b) => calendarTime(a.date, timezone) - calendarTime(b.date, timezone) || dateTime(a.date)! - dateTime(b.date)!);
        const newest = series.points[series.points.length - 1];
        series.latestDate = newest?.date; series.latestValue = newest?.value;
        series.expected = series.count;
        const unique = new Set(series.points.map(point => dateTime(point.date)));
        const datesValid = valid && unique.size === series.points.length;
        series.complete = series.complete && datesValid;
        if (!datesValid) return series;
        const latest = series.points[series.points.length - 1], previous = series.points[series.points.length - 2];
        series.headline = latest?.value ?? null;
        if (latest && previous) {
            const a = decimal(latest.value), b = decimal(previous.value), delta = a && b ? add(a, b, true) : null;
            series.change = delta ? text(delta) : null;
            series.complete = series.complete && delta !== null;
        }
        return series;
    });
}
