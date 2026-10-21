import type { GrowthCurveSeries } from './growthCurveData';
import type { GrowthMetric } from './growthTrend';
import { whoLmsAt } from './whoGrowthData';
export interface GrowthReferenceProfile { entity_id: string; birth_date: string; sex: 'male' | 'female'; height_measurement?: 'length' | 'height' }
export type WhoMeasurementBasis = 'length' | 'height' | 'weight';
export interface WhoReferencePoint { date: string; ageDays: number; ageMonths: number; p3: number; p50: number; p97: number; basis: WhoMeasurementBasis }
export interface WhoReference {
    points: WhoReferencePoint[];
    latestAgeDays: number | null;
    latestAgeMonths: number | null;
    ageIsCurrent: boolean;
    latestMedian: number | null;
    medianDifference: number | null;
    percentile: number | null;
    recentMedianChange: number | null;
    latestWithinRange: boolean | null;
    comparisonAllowed: boolean;
    basis: WhoMeasurementBasis | null;
    note: string;
    referenceReason: string | null;
}
const DAY = 86400000, MONTH_DAYS = 30.4375, P97_Z = 1.8807936081512509;

function literalDay(date: string): number | null {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return null;
    const value = Date.parse(`${date}T00:00:00Z`);
    return Number.isFinite(value) && new Date(value).toISOString().slice(0, 10) === date ? value : null;
}
function householdDay(date: string, timezone: string): number | null {
    try {
        const format = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' });
        if (date.length === 10) return literalDay(date);
        if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(date) || literalDay(date.slice(0, 10)) === null) return null;
        const time = Date.parse(date);
        if (!Number.isFinite(time)) return null;
        const parts = format.formatToParts(time), part = (kind: string) => parts.find(value => value.type === kind)!.value;
        return literalDay(`${part('year').padStart(4, '0')}-${part('month')}-${part('day')}`);
    } catch { return null; }
}
function ageDaysAt(date: string, birthDate: string, timezone: string): number | null {
    const birth = literalDay(birthDate), measured = householdDay(date, timezone);
    if (birth === null || measured === null) return null;
    const age = (measured - birth) / DAY;
    return Number.isInteger(age) && age >= 0 ? age : null;
}
export function ageMonthsAt(date: string, birthDate: string, timezone: string): number | null {
    const age = ageDaysAt(date, birthDate, timezone);
    return age === null ? null : age / MONTH_DAYS;
}
function basisAt(metric: GrowthMetric, day: number): WhoMeasurementBasis { return metric === 'weight' ? 'weight' : day < 731 ? 'length' : 'height'; }
function unitFactor(metric: GrowthMetric, unit: string): number | null {
    const factors: Record<string, number> = metric === 'height' ? { cm: 1, m: 100, mm: 0.1, '厘米': 1, '米': 100, '毫米': 0.1 } : { kg: 1, g: 0.001, '公斤': 1, '千克': 1, '克': 0.001 };
    return Object.prototype.hasOwnProperty.call(factors, unit) ? factors[unit]! : null;
}
function referenceOffset(metric: GrowthMetric, day: number, profile: GrowthReferenceProfile): number {
    if (metric !== 'height' || !profile.height_measurement || profile.height_measurement === basisAt(metric, day)) return 0;
    // Shift the standard to the recorded posture so the original observations
    // can stay unmodified on the chart. These are WHO's 0.7 cm corrections.
    return profile.height_measurement === 'length' ? 0.7 : -0.7;
}
function centile(l: number, m: number, s: number, z: number): number { return l === 0 ? m * Math.exp(s * z) : m * Math.pow(1 + l * s * z, 1 / l); }
function normalCdf(z: number): number {
    // Independently integrate the standard normal density with composite
    // Simpson quadrature. Calls are restricted to the supported [-3, 3] range.
    if (z === 0) return 0.5;
    const end = Math.abs(z), steps = 256, width = end / steps;
    let sum = 1 + Math.exp(-end * end / 2);
    for (let index = 1; index < steps; index++) {
        const x = index * width;
        sum += (index % 2 === 0 ? 2 : 4) * Math.exp(-x * x / 2);
    }
    return 0.5 + (z < 0 ? -1 : 1) * width * sum / (3 * Math.sqrt(2 * Math.PI));
}
function positiveDecimal(value: string | null): number | null {
    if (value === null || !/^[+]?\d+(?:\.\d+)?$/.test(value)) return null;
    const result = Number(value), scaled = Number(value.replace('.', ''));
    return Number.isFinite(result) && result > 0 && Number.isSafeInteger(scaled) ? result : null;
}
function unavailable(reason: string, note: string): WhoReference {
    return { points: [], latestAgeDays: null, latestAgeMonths: null, ageIsCurrent: false, latestMedian: null, medianDifference: null, percentile: null, recentMedianChange: null, latestWithinRange: null, comparisonAllowed: false, basis: null, note, referenceReason: reason };
}

/** WHO 2006 daily standards, independently computed offline from factual LMS parameters. */
export function buildWhoReference(series: GrowthCurveSeries, metric: GrowthMetric, profile: GrowthReferenceProfile | null, timezone: string, now = new Date()): WhoReference {
    if (!profile || !profile.entity_id || literalDay(profile.birth_date) === null || (profile.sex !== 'male' && profile.sex !== 'female')) return unavailable('profile_required', '需确认出生日期和性别后展示 WHO 参考线');
    profile = { ...profile, height_measurement: profile.height_measurement === 'length' || profile.height_measurement === 'height' ? profile.height_measurement : undefined };
    const factor = unitFactor(metric, series.unit);
    if (factor === null) return unavailable('unit_unsupported', '当前测量单位不支持 WHO 对照');
    const observed = series.points.length ? series.latestDate ?? series.points[series.points.length - 1]!.date : null;
    const date = observed ?? (Number.isFinite(now.getTime()) ? now.toISOString() : '');
    const age = ageDaysAt(date, profile.birth_date, timezone);
    if (age === null || age > 1856) return unavailable('age_out_of_range', 'WHO 此参考表适用日龄为 0–1856 天');
    const birth = literalDay(profile.birth_date)!;
    if (series.points.some(point => { const day = householdDay(point.date, timezone); return day !== null && day < birth; })) return unavailable('record_before_birth', '存在早于出生日期的记录，暂不对照 WHO');
    const result = unavailable('measurements_required', '尚无可对照实测记录，仅展示 WHO 参考线');
    result.latestAgeDays = age; result.latestAgeMonths = age / MONTH_DAYS; result.ageIsCurrent = observed === null; result.basis = basisAt(metric, age);
    // Reference-only views show a useful three-month grid without adding any
    // measured value. The standard may extend beyond the current age.
    const end = observed === null ? Math.min(1856, Math.max(91, Math.floor(Math.ceil(age / (3 * MONTH_DAYS)) * 3 * MONTH_DAYS))) : age;
    let corrected = false;
    for (let day = 0; day <= Math.ceil(end); day++) {
        if (day > 1856) break;
        const [l, m, s] = whoLmsAt(metric, profile.sex, day)!, offset = referenceOffset(metric, day, profile);
        corrected = corrected || offset !== 0;
        result.points.push({ date: new Date(birth + day * DAY).toISOString().slice(0, 10), ageDays: day, ageMonths: day / MONTH_DAYS, p3: (centile(l, m, s, -P97_Z) + offset) / factor, p50: (m + offset) / factor, p97: (centile(l, m, s, P97_Z) + offset) / factor, basis: basisAt(metric, day) });
    }
    const basisNote = metric === 'weight' ? '体重' : profile.height_measurement === 'length' ? '卧位身长' : profile.height_measurement === 'height' ? '站立身高' : '身长 / 身高';
    const notes = [`WHO 2006 · ${profile.sex === 'male' ? '男童' : '女童'} · ${basisNote} · P3–P97 参考区间`];
    if (corrected) notes.push('参考线已按 WHO 0.7 cm 规则换算至记录的测量姿势');
    const value = positiveDecimal(series.latestValue);
    if (metric === 'height' && !profile.height_measurement) {
        result.referenceReason = 'height_measurement_required'; notes.push('测量姿势待确认，仅展示参考线');
    } else if (!observed || !series.complete || value === null) {
        result.referenceReason = series.complete ? 'measurements_required' : 'measurements_incomplete'; notes.push('尚无完整实测数据可供对照');
    } else {
        const [l, m, s] = whoLmsAt(metric, profile.sex, age)!, offset = referenceOffset(metric, age, profile);
        const normalized = value * factor - offset;
        if (normalized > 0 && Number.isFinite(normalized)) {
            result.comparisonAllowed = true; result.referenceReason = null;
            result.latestMedian = (m + offset) / factor; result.medianDifference = value - result.latestMedian;
            result.latestWithinRange = normalized >= centile(l, m, s, -P97_Z) && normalized <= centile(l, m, s, P97_Z);
            const z = l === 0 ? Math.log(normalized / m) / s : (Math.pow(normalized / m, l) - 1) / (l * s);
            if (Number.isFinite(z) && Math.abs(z) <= 3 + 1e-12) result.percentile = normalCdf(Math.max(-3, Math.min(3, z))) * 100;
            else { result.referenceReason = 'percentile_out_of_range'; notes.push('超出 ±3 SD，百分位不作精确外推'); }
            if (series.recentChange !== null && series.recentFrom && series.recentTo) {
                const from = ageDaysAt(series.recentFrom, profile.birth_date, timezone), to = ageDaysAt(series.recentTo, profile.birth_date, timezone);
                if (from !== null && to !== null && from < to && to === age && from <= 1856) {
                    const first = whoLmsAt(metric, profile.sex, from)![1] + referenceOffset(metric, from, profile);
                    result.recentMedianChange = (m + offset - first) / factor;
                }
            }
        }
    }
    result.note = notes.join('；');
    return result;
}
