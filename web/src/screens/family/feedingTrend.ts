import { ArchiveGenerationChanged } from './archiveGeneration';
import { ApiError, type ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveFilter } from '../../types/archives';

export type MilkPeriod = 'day' | 'week' | 'month';
export interface MilkTrendData {
    dates: string[];
    generation: string;
    groups: ArchiveAnalysis['groups'];
    totals?: ArchiveAnalysis['groups'];
    complete: boolean;
}
// Legacy measurements retain their original metric names. A health volume
// such as water, medicine or puree must not become a feeding measurement.
const milkMetrics = ['volume', 'formula', 'breastmilk', 'breast_milk'];

/** Query complete authorized days, never total only the visible archive page.
 * Core performs every decimal sum. Four concurrent requests keep monthly
 * evidence within each existing response bound without increasing that bound. */
export async function readMilkTrend(api: ApiClient, filter: ArchiveFilter, period: MilkPeriod, timezone: string, signal: AbortSignal): Promise<MilkTrendData> {
    const cancelled = () => { if (signal.aborted) throw new DOMException('Request cancelled', 'AbortError'); };
    cancelled();
    const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date());
    const part = (type: string) => parts.find(p => p.type === type)!.value;
    const today = `${part('year')}-${part('month')}-${part('day')}`;
    const days = period === 'day' ? 1 : period === 'week' ? 7 : 30;
    const dates = Array.from({ length: days }, (_, i) => new Date(Date.parse(`${today}T12:00:00Z`) - (days - i - 1) * 86400000).toISOString().slice(0, 10));
    const analyze = (operation: 'series' | 'sum', metric: string, from: string, to: string) => api.analyzeArchives({ operation, metric, domain: 'health', entity_id: filter.entity_id, query: filter.query, date_from: from, date_to: to }, signal);
    const discovery = await Promise.all(milkMetrics.map(async metric => {
        try { return await analyze('series', metric, dates[0]!, today); }
        catch (error) {
            if (period !== 'day' && error instanceof ApiError && error.status === 409 && error.code === 'conflict' && error.details?.reason === 'analysis_response_limit') return null;
            throw error;
        }
    }));
    const localDate = (value: string): string | null => {
        if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return value;
        if (!/^\d{4}-\d{2}-\d{2}T/.test(value) || !Number.isFinite(Date.parse(value))) return null;
        const p = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date(value));
        return `${p.find(v => v.type === 'year')!.value}-${p.find(v => v.type === 'month')!.value}-${p.find(v => v.type === 'day')!.value}`;
    };
    const queries: { date: string; metric: string }[] = [];
    if (period !== 'day') discovery.forEach((result, index) => {
        const selected = new Set<string>();
        let allDays = result === null;
        for (const group of result?.groups ?? []) {
            if (group.metric !== milkMetrics[index]) continue;
            const date = group.date ? localDate(group.date) : null;
            if (!date) allDays = true;
            else if (dates.includes(date)) selected.add(date);
        }
        for (const date of allDays ? dates : [...selected].sort()) queries.push({ date, metric: milkMetrics[index]! });
    });
    if (period === 'day') for (const metric of milkMetrics) queries.push({ date: today, metric });
    let boundedTotal = false;
    const results: (ArchiveAnalysis | null)[] = new Array(queries.length);
    let next = 0;
    await Promise.all(Array.from({ length: 4 }, async () => {
        while (next < queries.length) {
            cancelled();
            const index = next++;
            const { date, metric } = queries[index]!;
            try { results[index] = await analyze('sum', metric, date, date); }
            catch (error) {
                if (error instanceof ApiError && error.status === 409 && error.code === 'conflict' && error.details?.reason === 'analysis_response_limit') { results[index] = null; boundedTotal = true; }
                else throw error;
            }
        }
    }));
    cancelled();
    const proofs = discovery.concat(results).filter((result): result is ArchiveAnalysis => result !== null);
    const first = proofs[0];
    if (!first) throw new Error('Milk analysis missing');
    const generation = `${first.data_epoch}:${first.generation}`;
    if (proofs.some(result => `${result.data_epoch}:${result.generation}` !== generation)) throw new ArchiveGenerationChanged('Milk analysis generation changed');
    const groups: ArchiveAnalysis['groups'] = [];
    if (period === 'day') discovery.forEach((result, index) => {
        for (const group of result?.groups ?? []) if (group.metric === milkMetrics[index]) groups.push(group);
    });
    else results.forEach((result, index) => {
        for (const group of result?.groups ?? []) {
            if (group.metric === queries[index]!.metric) groups.push({ ...group, date: queries[index]!.date, date_precision: 'day' });
        }
    });
    return {
        dates, generation, groups,
        ...(period === 'day' ? { totals: results.reduce<ArchiveAnalysis['groups']>((totals, result, index) => totals.concat((result?.groups ?? []).filter(group => group.metric === queries[index]!.metric)), []) } : {}),
        complete: !boundedTotal && proofs.every(result => result.coverage.complete),
    };
}
