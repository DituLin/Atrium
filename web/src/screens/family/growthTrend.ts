import { ApiError, type ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveAnalysisQuery, ArchiveFilter } from '../../types/archives';
export type GrowthPeriod = 'quarter' | 'year' | 'all';
export type GrowthMetric = 'height' | 'weight';
export interface GrowthData { height: ArchiveAnalysis[]; weight: ArchiveAnalysis[]; limited: GrowthMetric[] }
export function growthRange(period: GrowthPeriod, timezone: string, now = new Date()): Pick<ArchiveAnalysisQuery, 'date_from' | 'date_to'> {
    if (period === 'all') return {};
    const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now);
    const part = (kind: string) => Number(parts.find(p => p.type === kind)!.value);
    const year = part('year'), month = part('month'), day = part('day');
    const start = new Date(Date.UTC(year, month - 1 - (period === 'quarter' ? 3 : 12), 1));
    const last = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth() + 1, 0)).getUTCDate();
    start.setUTCDate(Math.min(day, last));
    return { date_from: start.toISOString().slice(0, 10), date_to: `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}` };
}
/** Each literal metric is queried once. No sums, estimates or page-derived history. */
export async function readGrowthTrend(api: ApiClient, filter: ArchiveFilter, period: GrowthPeriod, timezone: string, signal: AbortSignal): Promise<GrowthData> {
    const data: GrowthData = { height: [], weight: [], limited: [] };
    const aliases: [GrowthMetric, string][] = [['height', 'height'], ['height', '身高'], ['height', '身长'], ['weight', 'weight'], ['weight', '体重']];
    const range = growthRange(period, timezone);
    const results = await Promise.all(aliases.map(async ([kind, metric]) => {
        try {
            const result = await api.analyzeArchives({ operation: 'series', metric, domain: 'health', entity_id: filter.entity_id, query: filter.query, ...range }, signal);
            return { kind, result: { ...result, groups: result.groups.filter(g => g.metric === metric).map(g => ({ ...g, metric: kind })) } };
        } catch (error) {
            if (error instanceof ApiError && error.status === 409 && error.code === 'conflict' && error.details?.reason === 'analysis_response_limit') return { kind, result: null };
            throw error;
        }
    }));
    for (const { kind, result } of results) {
        if (result) data[kind].push(result);
        else if (!data.limited.includes(kind)) data.limited.push(kind);
    }
    return data;
}
