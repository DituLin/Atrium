import { expect, it } from 'vitest';
import cases from '../../../testdata/overview-ordering.json';
import { projectOverview } from './overview';
import type { OverviewSources } from '../types/api';
for (const sample of cases) it(sample.name, () => {
 expect(projectOverview(sample.sources as OverviewSources, Date.parse('2026-09-12T10:00:30Z'), false).entries).toEqual(sample.entries);
});
it('recalculates expiry and removes notice exactly at its deadline despite offline transport', () => {
 const sources = structuredClone(cases[0]!.sources) as OverviewSources;
 sources.notice.items[0]!.valid_until = '2026-09-12T10:01:00Z';
 const before = projectOverview(sources, Date.parse('2026-09-12T10:00:59.999Z'), true);
 expect(before.entries.some(e => e.kind === 'notice')).toBe(true);
 const after = projectOverview(sources, Date.parse('2026-09-12T10:01:00Z'), false);
 expect(after.entries.some(e => e.kind === 'notice')).toBe(false);
 expect(after.entries.some(e => e.source_id === 'online')).toBe(true);
 expect(after.sources.notice.items).toEqual([]);
});
