import { afterEach, expect, it, vi } from 'vitest';
import { ApiError, type ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveAnalysisQuery } from '../../types/archives';
import { readMilkTrend } from './feedingTrend';

const result = (groups: ArchiveAnalysis['groups'] = [], generation = 1) => ({ data_epoch: 1, generation, groups, coverage: { complete: true } } as ArchiveAnalysis);
afterEach(() => vi.useRealTimers());
it('gets the precise Core daily sum separately from the feeding curve', async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-20T17:00:00Z'));
    const analyzeArchives = vi.fn(async (q: ArchiveAnalysisQuery) => result(q.metric === 'volume' ? q.operation === 'series' ? [
        { metric: 'volume', value: '0.1', unit: 'ml', date: '2026-10-21T00:00:00+08:00' },
        { metric: 'volume', value: '0.2', unit: 'ml', date: '2026-10-21T01:00:00+08:00' },
    ] : [{ metric: 'volume', value: '0.3', unit: 'ml' }] : []));
    const data = await readMilkTrend({ analyzeArchives } as unknown as ApiClient, { entity_id: 'synthetic-child', query: '合成' }, 'day', 'Asia/Singapore', new AbortController().signal);
    expect(data.groups).toHaveLength(2); expect(data.totals).toEqual([{ metric: 'volume', value: '0.3', unit: 'ml' }]);
    const sums = analyzeArchives.mock.calls.map(([q]) => q).filter(q => q.operation === 'sum');
    expect(sums).toHaveLength(4);
    expect(sums.every(q => q.entity_id === 'synthetic-child' && q.query === '合成' && q.date_from === '2026-10-21' && q.date_to === '2026-10-21')).toBe(true);
});
it('rejects a daily sum from another generation', async () => {
    const analyzeArchives = vi.fn(async (q: ArchiveAnalysisQuery) => result([], q.operation === 'sum' ? 2 : 1));
    await expect(readMilkTrend({ analyzeArchives } as unknown as ApiClient, {}, 'day', 'UTC', new AbortController().signal)).rejects.toThrow('generation changed');
});
it('keeps a daily curve when its Core sum reaches the response bound', async () => {
    const analyzeArchives = vi.fn(async (q: ArchiveAnalysisQuery) => {
        if (q.operation === 'sum' && q.metric === 'volume') throw new ApiError(409, 'conflict', 'bounded', null, { reason: 'analysis_response_limit' });
        return result(q.metric === 'volume' ? [{ metric: 'volume', value: '5', unit: 'ml', date: '2026-10-21T00:00:00Z' }] : []);
    });
    const data = await readMilkTrend({ analyzeArchives } as unknown as ApiClient, {}, 'day', 'UTC', new AbortController().signal);
    expect(data.groups).toHaveLength(1); expect(data.totals).toEqual([]); expect(data.complete).toBe(false);
});
it('does not start milk queries after cancellation', async () => {
    const controller = new AbortController(); controller.abort(); const analyzeArchives = vi.fn();
    await expect(readMilkTrend({ analyzeArchives } as unknown as ApiClient, {}, 'day', 'UTC', controller.signal)).rejects.toMatchObject({ name: 'AbortError' });
    expect(analyzeArchives).not.toHaveBeenCalled();
});
