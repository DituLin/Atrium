import { expect, it } from 'vitest';
import { createRequire } from 'node:module';
import type { ArchiveAnalysis, ArchiveFilter, ArchiveOverview, ArchivePage } from '../types/archives';

// Load through the same native TS support as the mock server, without adding
// development-only mock sources to the client TypeScript composite project.
const { MockArchives } = createRequire(import.meta.url)('../../mock/archives.ts') as { MockArchives: new () => {
    search(filter: ArchiveFilter, now?: Date): ArchivePage;
    overview(now?: Date): ArchiveOverview;
    analysis(params: URLSearchParams, now?: Date): ArchiveAnalysis;
} };

it('derives all fixture dates from the household calendar across month and year boundaries', () => {
    const fixture = new MockArchives(), now = new Date('2026-01-31T17:00:00Z');
    expect(fixture.search({}, now).items[0]?.dates).toEqual(['2026-02-01']);
    const milk = fixture.analysis(new URLSearchParams({ operation: 'sum', metric: 'volume', domain: 'health', date_from: '2026-02-01', date_to: '2026-02-01' }), now);
    expect(milk.groups).toEqual([{ metric: 'volume', value: '830', unit: 'ml' }]);
    const amounts = fixture.analysis(new URLSearchParams({ operation: 'series', metric: 'amount', domain: 'finance' }), now);
    expect(amounts.groups.map(group => group.date)).toEqual(['2025-09-01', '2025-10-01', '2025-11-01', '2025-12-01', '2026-01-01', '2026-02-01']);
});
it('computes overview counts from the same public synthetic items served by search', () => {
    const fixture = new MockArchives(), now = new Date('2024-02-29T10:00:00Z');
    const first = fixture.search({ limit: 100 }, now), second = fixture.search({ limit: 100, cursor: first.next_cursor }, now);
    const all = [...first.items, ...second.items], overview = fixture.overview(now);
    expect(all).toHaveLength(128); expect(overview.total).toBe(all.length);
    const confirmed = all.filter(item => ['confirmed', 'household'].includes(item.ownership) && !item.review_required);
    expect(overview.confirmed).toBe(120); expect(overview.confirmed).toBe(confirmed.length);
    expect(overview.pending).toBe(8); expect(overview.pending).toBe(all.length - confirmed.length);
    for (const entity of overview.entities) expect(entity.count).toBe(all.filter(item => item.entity_ids.includes(entity.id)).length);
    for (const domain of overview.domains) expect(domain.count).toBe(all.filter(item => item.domain === domain.domain).length);
});
it('honors synthetic query and entity filters without fabricating sums for empty matches', () => {
    const fixture = new MockArchives();
    expect(fixture.search({ query: '不存在的合成记录' }).items).toEqual([]);
    expect(fixture.analysis(new URLSearchParams({ operation: 'sum', metric: 'amount', query: '不存在的合成记录' })).groups).toEqual([]);
    expect(fixture.analysis(new URLSearchParams({ operation: 'series', metric: 'height', entity_id: 'synthetic_person_a' })).groups).toEqual([]);
});
