import { afterEach, expect, it, vi } from 'vitest';
import { ApiError, type ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveAnalysisQuery } from '../../types/archives';
import { readFinanceTrend } from './financeTrend';

const result = (groups: ArchiveAnalysis['groups'] = [], generation = 1) => ({ data_epoch: 1, generation, groups, coverage: { complete: true } } as ArchiveAnalysis);
afterEach(() => vi.useRealTimers());
it('queries six calendar months in the home timezone and preserves Core decimals and currencies', async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-01-31T17:00:00Z'));
    const analyzeArchives = vi.fn(async (q: ArchiveAnalysisQuery) => result(q.date_from === '2026-02-01' ? [
        { metric: 'amount', value: '0.3000', currency: 'CNY' }, { metric: 'amount', value: '5', currency: 'USD', sample: '合成类别' },
    ] : []));
    const data = await readFinanceTrend({ analyzeArchives } as unknown as ApiClient, { entity_id: 'synthetic-person', domain: 'health', query: '合成' }, 'Asia/Singapore', new AbortController().signal);
    expect(data.months).toEqual(['2025-09', '2025-10', '2025-11', '2025-12', '2026-01', '2026-02']);
    expect(data.generation).toBe('1:1'); expect(data.complete).toBe(true);
    expect(data.groups).toEqual([{ metric: 'amount', value: '0.3000', currency: 'CNY', date: '2026-02-01' }, { metric: 'amount', value: '5', currency: 'USD', sample: '合成类别', date: '2026-02-01' }]);
    const calls = analyzeArchives.mock.calls.map(([q]) => q);
    expect(calls).toHaveLength(6);
    expect(calls.every(q => q.operation === 'sum' && q.metric === 'amount' && q.domain === 'finance' && q.entity_id === 'synthetic-person' && q.query === '合成')).toBe(true);
    expect(calls).toContainEqual(expect.objectContaining({ date_from: '2025-09-01', date_to: '2025-09-30' }));
    expect(calls).toContainEqual(expect.objectContaining({ date_from: '2026-02-01', date_to: '2026-02-01' }));
});
it('runs at most four requests concurrently', async () => {
    let active = 0, peak = 0;
    const analyzeArchives = vi.fn(async () => {
        active++; peak = Math.max(peak, active); await Promise.resolve(); active--; return result();
    });
    await readFinanceTrend({ analyzeArchives } as unknown as ApiClient, {}, 'UTC', new AbortController().signal);
    expect(peak).toBe(4); expect(active).toBe(0);
});
it('rejects generation changes and authorization failures without partial data', async () => {
    let call = 0;
    const analyzeArchives = vi.fn(async () => result([], ++call === 1 ? 1 : 2));
    const api = { analyzeArchives } as unknown as ApiClient;
    await expect(readFinanceTrend(api, {}, 'UTC', new AbortController().signal)).rejects.toThrow('generation changed');
    analyzeArchives.mockRejectedValue(new ApiError(403, 'forbidden', 'revoked'));
    await expect(readFinanceTrend(api, {}, 'UTC', new AbortController().signal)).rejects.toMatchObject({ status: 403 });
});
it('reports incomplete coverage for a bounded month without inventing a zero', async () => {
    let call = 0;
    const analyzeArchives = vi.fn(async () => {
        if (++call === 2) throw new ApiError(409, 'conflict', 'bounded', null, { reason: 'analysis_response_limit' });
        return result();
    });
    const data = await readFinanceTrend({ analyzeArchives } as unknown as ApiClient, {}, 'UTC', new AbortController().signal);
    expect(data.complete).toBe(false); expect(data.groups).toEqual([]);
});
it('checks cancellation before and after pending requests', async () => {
    const controller = new AbortController(); controller.abort(); const analyzeArchives = vi.fn();
    const api = { analyzeArchives } as unknown as ApiClient;
    await expect(readFinanceTrend(api, {}, 'UTC', controller.signal)).rejects.toMatchObject({ name: 'AbortError' }); expect(analyzeArchives).not.toHaveBeenCalled();
    const other = new AbortController(); analyzeArchives.mockImplementation(async () => { other.abort(); return result(); });
    await expect(readFinanceTrend(api, {}, 'UTC', other.signal)).rejects.toMatchObject({ name: 'AbortError' });
});
