import type { ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveFilter } from '../../types/archives';

export type MilkPeriod = 'day' | 'week' | 'month';
export interface MilkTrendData {
    dates: string[];
    generation: string;
    groups: ArchiveAnalysis['groups'];
    complete: boolean;
}

/** Query complete authorized days, never total only the visible archive page.
 * Core performs every decimal sum. Four concurrent requests keep monthly
 * evidence within each existing response bound without increasing that bound. */
export async function readMilkTrend(api: ApiClient, filter: ArchiveFilter, period: MilkPeriod, timezone: string, signal: AbortSignal): Promise<MilkTrendData> {
    const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date());
    const part = (type: string) => parts.find(p => p.type === type)!.value;
    const today = `${part('year')}-${part('month')}-${part('day')}`;
    const days = period === 'day' ? 1 : period === 'week' ? 7 : 30;
    const dates = Array.from({ length: days }, (_, i) => new Date(Date.parse(`${today}T12:00:00Z`) - (days - i - 1) * 86400000).toISOString().slice(0, 10));
    const results: ArchiveAnalysis[] = new Array(days);
    let next = 0;
    await Promise.all(Array.from({ length: Math.min(4, days) }, async () => {
        while (next < dates.length) {
            if (signal.aborted) throw new DOMException('Request cancelled', 'AbortError');
            const index = next++;
            const date = dates[index];
            results[index] = await api.analyzeArchives({ operation: period === 'day' ? 'series' : 'sum', metric: 'volume', domain: 'health', entity_id: filter.entity_id, query: filter.query, date_from: date, date_to: date }, signal);
        }
    }));
    const first = results[0];
    if (!first) throw new Error('Milk analysis missing');
    const generation = `${first.data_epoch}:${first.generation}`;
    if (results.some(result => `${result.data_epoch}:${result.generation}` !== generation)) throw new Error('Milk analysis generation changed');
    const groups: ArchiveAnalysis['groups'] = [];
    results.forEach((result, index) => {
        for (const group of result.groups) {
            if (group.metric === 'volume') groups.push(period === 'day' ? group : { ...group, date: dates[index], date_precision: 'day' });
        }
    });
    return {
        dates, generation, groups,
        complete: results.every(result => result.coverage.complete),
    };
}
