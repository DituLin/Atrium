import { ArchiveGenerationChanged } from './archiveGeneration';
import { ApiError, type ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveFilter } from '../../types/archives';

export interface FinanceTrendData {
    months: string[];
    generation: string;
    groups: ArchiveAnalysis['groups'];
    complete: boolean;
}

/** Each monthly amount remains a Core decimal in its original unit, currency
 * and sample. A missing or bounded month never becomes a synthetic zero. */
export async function readFinanceTrend(api: ApiClient, filter: ArchiveFilter, timezone: string, signal: AbortSignal): Promise<FinanceTrendData> {
    const cancelled = () => { if (signal.aborted) throw new DOMException('Request cancelled', 'AbortError'); };
    cancelled();
    const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date());
    const part = (type: string) => parts.find(p => p.type === type)!.value;
    const today = `${part('year')}-${part('month')}-${part('day')}`;
    const months = Array.from({ length: 6 }, (_, i) => new Date(Date.UTC(Number(part('year')), Number(part('month')) - 6 + i, 1)).toISOString().slice(0, 7));
    const results: (ArchiveAnalysis | null)[] = new Array(months.length);
    let next = 0;
    await Promise.all(Array.from({ length: 4 }, async () => {
        while (next < months.length) {
            cancelled();
            const index = next++, month = months[index]!;
            const monthEnd = new Date(Date.UTC(Number(month.slice(0, 4)), Number(month.slice(5, 7)), 0)).toISOString().slice(0, 10);
            try {
                results[index] = await api.analyzeArchives({ operation: 'sum', metric: 'amount', domain: 'finance', entity_id: filter.entity_id, query: filter.query, date_from: `${month}-01`, date_to: index === months.length - 1 ? today : monthEnd }, signal);
            } catch (error) {
                if (error instanceof ApiError && error.status === 409 && error.code === 'conflict' && error.details?.reason === 'analysis_response_limit') results[index] = null;
                else throw error;
            }
        }
    }));
    cancelled();
    const proofs = results.filter((result): result is ArchiveAnalysis => result !== null);
    const first = proofs[0];
    const generation = first ? `${first.data_epoch}:${first.generation}` : '';
    if (proofs.some(result => `${result.data_epoch}:${result.generation}` !== generation)) throw new ArchiveGenerationChanged('Finance analysis generation changed');
    return { months, generation, groups: results.reduce<ArchiveAnalysis['groups']>((groups, result, index) => groups.concat((result?.groups ?? []).filter(group => group.metric === 'amount').map(group => ({ ...group, date: `${months[index]}-01` }))), []), complete: results.every(result => result !== null && result.coverage.complete) };
}
