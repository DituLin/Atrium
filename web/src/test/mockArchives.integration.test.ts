import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { afterAll, beforeAll, expect, it } from 'vitest';
import type { ArchiveAnalysis, ArchiveDetail, ArchiveEntities, ArchiveOverview, ArchivePage } from '../types/archives';

const BASE = 'http://127.0.0.1:8794';
let child: ChildProcessWithoutNullStreams;
let cookie = '';
const post = (path: string, body: unknown) => fetch(`${BASE}${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
const get = (path: string) => fetch(`${BASE}${path}`, { headers: { cookie } });
async function data<T>(path: string): Promise<T> {
    const response = await get(path); expect(response.status).toBe(200);
    const envelope = await response.json() as { schema_version: string; availability: string; data: T };
    expect(envelope.schema_version).toBe('1'); expect(envelope.availability).toBe('available'); return envelope.data;
}
function homeDate(): string {
    const parts = new Intl.DateTimeFormat('en', { timeZone: 'Asia/Singapore', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date());
    return `${parts.find(p => p.type === 'year')!.value}-${parts.find(p => p.type === 'month')!.value}-${parts.find(p => p.type === 'day')!.value}`;
}
beforeAll(async () => {
    child = spawn('node', ['mock/server.ts'], { cwd: process.cwd(), env: { ...process.env, ATRIUM_MOCK_PORT: '8794' } }) as ChildProcessWithoutNullStreams;
    await new Promise<void>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error('archive mock did not start')), 15000);
        child.stdout.on('data', chunk => { if (String(chunk).includes('listening')) { clearTimeout(timeout); resolve(); } });
        child.on('error', reject);
    });
    const started = await (await post('/api/v1/pair/start', {})).json() as { code: string; pairing_id: string };
    await post('/__control/approve', { code: started.code, name: 'Synthetic archive QA screen' });
    const claimed = await post(`/api/v1/pair/${started.pairing_id}/claim`, {});
    cookie = claimed.headers.get('set-cookie')!.split(';')[0]!;
}, 20000);
afterAll(() => { child?.kill(); });

it('requires a screen credential for every archive fixture route', async () => {
    for (const route of ['entities', 'overview', 'search', 'analysis?operation=count&count=documents', 'documents/synthetic_doc_001']) {
        expect((await fetch(`${BASE}/api/v1/archives/${route}`)).status).toBe(401);
    }
});
it('serves synthetic whole-family counts and typed entities with one consistent generation', async () => {
    const overview = await data<ArchiveOverview>('/api/v1/archives/overview');
    expect(overview).toMatchObject({ generation: '1:1', total: 128, confirmed: 120, pending: 8, ownership: { household: 19, unassigned: 5, waiting: 3 }, coverage: { complete: true } });
    expect(overview.domains).toEqual([{ domain: 'health', count: 56 }, { domain: 'finance', count: 34 }, { domain: 'education', count: 22 }, { domain: 'property', count: 16 }]);
    const entities = await data<ArchiveEntities>('/api/v1/archives/entities');
    expect(entities.items).toHaveLength(4); expect(entities.items[0]?.names).toEqual(['合成宝宝']);
    expect(overview.entities.find(e => e.id === 'synthetic_child')?.count).toBe(61);
    const page = await data<ArchivePage>('/api/v1/archives/search?limit=10');
    expect(page.generation).toBe(overview.generation); expect(page.items).toHaveLength(10); expect(page.next_cursor).toBeTruthy();
});
it('filters and paginates synthetic archives and provides a structured document detail', async () => {
    const first = await data<ArchivePage>('/api/v1/archives/search?entity_id=synthetic_child&domain=health&limit=10');
    expect(first.items.every(item => item.domain === 'health' && item.entity_ids.includes('synthetic_child'))).toBe(true);
    const second = await data<ArchivePage>(`/api/v1/archives/search?entity_id=synthetic_child&domain=health&limit=10&cursor=${encodeURIComponent(first.next_cursor!)}`);
    expect(new Set([...first.items, ...second.items].map(item => item.document_id)).size).toBe(20);
    const detail = await data<ArchiveDetail>(`/api/v1/archives/documents/${first.items[0]!.document_id}`);
    expect(detail).toMatchObject({ generation: first.generation, preview: 'structured', original_saved: true, item: { document_id: first.items[0]!.document_id } });
    expect(detail.item.evidence[0]?.excerpt).toContain('合成');
    expect((await get('/api/v1/archives/documents/no-such-document')).status).toBe(404);
});
it('serves day milk total and feeds, growth deltas and adjacent monthly finance amounts using household dates', async () => {
    const today = homeDate();
    const query = '/api/v1/archives/analysis?domain=health&entity_id=synthetic_child&metric=volume';
    const sum = await data<ArchiveAnalysis>(`${query}&operation=sum&date_from=${today}&date_to=${today}`);
    expect(sum.groups).toEqual([{ metric: 'volume', value: '830', unit: 'ml' }]);
    const series = await data<ArchiveAnalysis>(`${query}&operation=series&date_from=${today}&date_to=${today}`);
    expect(series.groups).toHaveLength(6); expect(series.groups.every(group => group.date?.startsWith(today))).toBe(true);
    expect(`${sum.data_epoch}:${sum.generation}`).toBe('1:1');
    const height = await data<ArchiveAnalysis>('/api/v1/archives/analysis?domain=health&entity_id=synthetic_child&metric=height&operation=series');
    const weight = await data<ArchiveAnalysis>('/api/v1/archives/analysis?domain=health&entity_id=synthetic_child&metric=weight&operation=series');
    expect(height.groups.slice(-2).map(group => group.value)).toEqual(['71.9', '72.6']);
    expect(weight.groups.slice(-2).map(group => group.value)).toEqual(['9.1', '9.3']);
    const currentMonth = today.slice(0, 7), lastMonth = new Date(Date.UTC(Number(today.slice(0, 4)), Number(today.slice(5, 7)) - 2, 1)).toISOString().slice(0, 7);
    const amount = (month: string) => data<ArchiveAnalysis>(`/api/v1/archives/analysis?domain=finance&operation=sum&metric=amount&date_from=${month}-01&date_to=${month}-31`);
    expect((await amount(currentMonth)).groups).toEqual([{ metric: 'amount', value: '12480', currency: 'CNY' }]);
    expect((await amount(lastMonth)).groups).toEqual([{ metric: 'amount', value: '11785', currency: 'CNY' }]);
});
it('offers an empty-state control with a new shared generation and restores populated fixtures', async () => {
    const response = await post('/__control/archives', { empty: true }); expect(response.status).toBe(200);
    const overview = await data<ArchiveOverview>('/api/v1/archives/overview'); expect(overview).toMatchObject({ generation: '1:2', total: 0 });
    const page = await data<ArchivePage>('/api/v1/archives/search'); expect(page.items).toEqual([]); expect(page.generation).toBe(overview.generation);
    const analysis = await data<ArchiveAnalysis>('/api/v1/archives/analysis?operation=series&metric=height&domain=health');
    expect(analysis.groups).toEqual([]); expect(`${analysis.data_epoch}:${analysis.generation}`).toBe(overview.generation);
    await post('/__control/archives', { empty: false }); expect((await data<ArchiveOverview>('/api/v1/archives/overview')).total).toBe(128);
});
