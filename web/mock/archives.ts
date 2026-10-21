/** Public, entirely synthetic archive fixtures for dashboard development.
 * This is a bounded display fixture, not a replacement archive parser. */
import type { ArchiveAnalysis, ArchiveDetail, ArchiveEntities, ArchiveEntity, ArchiveFilter, ArchiveItem, ArchiveOverview, ArchivePage } from '../src/types/archives.ts';
import { HOME_TIMEZONE } from './data.ts';

type Group = ArchiveAnalysis['groups'][number];
const ENTITIES: ArchiveEntity[] = [
    { id: 'synthetic_child', kind: 'person', names: ['合成宝宝'], aliases: [] },
    { id: 'synthetic_person_a', kind: 'person', names: ['合成家人甲'], aliases: [] },
    { id: 'synthetic_person_b', kind: 'person', names: ['合成家人乙'], aliases: [] },
    { id: 'synthetic_home', kind: 'property', names: ['合成房屋'], aliases: [] },
];
const DOMAINS = ['health', 'finance', 'education', 'property'];

function homeDate(now: Date): string {
    const parts = new Intl.DateTimeFormat('en', { timeZone: HOME_TIMEZONE, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now);
    const part = (kind: string) => parts.find(p => p.type === kind)!.value;
    return `${part('year')}-${part('month')}-${part('day')}`;
}
function daysAgo(today: string, days: number): string {
    return new Date(Date.parse(`${today}T12:00:00Z`) - days * 86400000).toISOString().slice(0, 10);
}
function fixtureItems(today: string): ArchiveItem[] {
    return Array.from({ length: 128 }, (_, index) => {
        const domain = index < 56 ? 'health' : index < 90 ? 'finance' : index < 112 ? 'education' : 'property';
        const entity_ids = [
            ...(index < 61 ? ['synthetic_child'] : []),
            ...(index >= 61 && index < 85 ? ['synthetic_person_a'] : []),
            ...(index >= 80 && index < 101 ? ['synthetic_person_b'] : []),
            ...(index < 14 ? ['synthetic_home'] : []),
        ];
        const ownership = index < 101 ? 'confirmed' : index < 120 ? 'household' : index < 125 ? 'unassigned' : 'waiting';
        const review_required = index >= 124;
        const title = domain === 'health' ? index < 6 ? '合成喂养记录' : '合成成长测量' : domain === 'finance' ? '合成月度财务资料' : domain === 'education' ? '合成学习档案' : '合成房屋资料';
        const field_name = domain === 'health' ? index < 6 ? 'volume' : 'height' : domain === 'finance' ? 'amount' : domain === 'education' ? 'score' : 'warranty_end';
        const asserted_value = domain === 'health' ? index < 6 ? '140 ml' : '72.6 cm' : domain === 'finance' ? '12480 CNY' : domain === 'education' ? '88 / 100' : '示例保修资料';
        const excerpt = `${title} ${asserted_value}（仅用于界面验证）`;
        return { document_id: `synthetic_doc_${String(index + 1).padStart(3, '0')}`, document_revision: 1, domain, document_type: domain === 'health' ? 'measurement_log' : domain === 'finance' ? 'financial_statement' : domain === 'education' ? 'school_report' : 'property_record', ownership, parse_status: 'parsed', entity_ids, dates: [daysAgo(today, Math.floor(index / 3))], evidence: [
            { field_name: 'title', asserted_value: title, excerpt: title, confirmed: !review_required, locator: { text: { start: 0, end: title.length } }, start: 0, end: title.length },
            { field_name, asserted_value, excerpt, confirmed: !review_required, locator: { text: { start: 0, end: excerpt.length } }, start: 0, end: excerpt.length },
        ], metadata_only: false, truncated: false, review_required, original_saved: true, processing: review_required ? 'needs_review' : 'ready', task_summary: { parse: 'parsed', understanding: review_required ? 'needs_review' : 'complete', attribution: ownership } };
    });
}
function filtered(items: ArchiveItem[], filter: ArchiveFilter): ArchiveItem[] {
    return items.filter(item => (!filter.domain || filter.domain === item.domain) && (!filter.entity_id || item.entity_ids.includes(filter.entity_id)) && (!filter.ownership || item.ownership === filter.ownership) && (!filter.query || item.evidence.some(evidence => `${evidence.asserted_value ?? ''} ${evidence.excerpt}`.includes(filter.query!))));
}

export class MockArchives {
    private empty = false;
    private generation = 1;
    control(empty?: boolean): { empty: boolean; generation: string } {
        if (empty !== undefined && empty !== this.empty) { this.empty = empty; this.generation++; }
        return { empty: this.empty, generation: `1:${this.generation}` };
    }
    private items(now: Date): ArchiveItem[] { return this.empty ? [] : fixtureItems(homeDate(now)); }
    entities(): ArchiveEntities { return { items: this.empty ? [] : ENTITIES, coverage: { complete: true } }; }
    overview(now = new Date()): ArchiveOverview {
        const items = this.items(now);
        const confirmed = items.filter(item => ['confirmed', 'household'].includes(item.ownership) && !item.review_required).length;
        const birth = new Date(Date.UTC(Number(homeDate(now).slice(0,4))-1,Number(homeDate(now).slice(5,7))-1,Number(homeDate(now).slice(8,10)))).toISOString().slice(0,10);
        return { ...(items.length ? {growth_profiles:[{entity_id:'synthetic_child',birth_date:birth,sex:'male' as const,height_measurement:'length' as const}]} : {}),generation: `1:${this.generation}`, total: items.length, confirmed, pending: items.length - confirmed,
            domains: DOMAINS.map(domain => ({ domain, count: items.filter(item => item.domain === domain).length })),
            entities: ENTITIES.map(entity => ({ id: entity.id, count: items.filter(item => item.entity_ids.includes(entity.id)).length })),
            ownership: { household: items.filter(item => item.ownership === 'household').length, unassigned: items.filter(item => item.ownership === 'unassigned').length, waiting: items.filter(item => item.ownership === 'waiting').length }, coverage: { complete: true } };
    }
    search(filter: ArchiveFilter, now = new Date()): ArchivePage {
        const items = filtered(this.items(now), filter);
        const parsed = /^synthetic:(\d+)$/.exec(filter.cursor ?? '');
        const offset = parsed ? Number(parsed[1]) : 0, limit = Math.min(100, Math.max(1, Math.floor(filter.limit ?? 10)));
        return { items: items.slice(offset, offset + limit), ...(offset + limit < items.length ? { next_cursor: `synthetic:${offset + limit}` } : {}), generation: `1:${this.generation}`, coverage: { complete: true }, index_status: 'ready' };
    }
    detail(id: string, now = new Date()): ArchiveDetail | null {
        const item = this.items(now).find(item => item.document_id === id);
        return item ? { item, original_saved: true, nas_status: 'saved', processing: item.processing ?? 'ready', task_summary: item.task_summary, preview: 'structured', generation: `1:${this.generation}`, coverage: { complete: true } } : null;
    }
    analysis(params: URLSearchParams, now = new Date()): ArchiveAnalysis {
        const today = homeDate(now), metric = params.get('metric') ?? 'documents', operation = params.get('operation') ?? 'count';
        const entity_id = params.get('entity_id') ?? undefined, domain = params.get('domain') ?? undefined, query = params.get('query') ?? undefined, ownership = params.get('ownership') ?? undefined;
        const items = filtered(this.items(now), { entity_id, domain, query, ownership }).filter(item => !item.review_required && item.ownership !== 'unassigned' && item.ownership !== 'waiting');
        const from = params.get('date_from'), to = params.get('date_to');
        const inRange = (group: Group) => (!from || group.date!.slice(0, 10) >= from) && (!to || group.date!.slice(0, 10) <= to);
        let groups: Group[] = [];
        if (operation === 'count') groups = [{ metric: params.get('count') ?? 'documents', value: String(items.length) }];
        else if (items.length && metric === 'volume' && (!domain || domain === 'health') && (!entity_id || entity_id === 'synthetic_child')) {
            const daily: Group[] = [];
            for (let day = 29; day >= 0; day--) {
                const date = daysAgo(today, day), values = day === 0 ? [120, 130, 150, 140, 150, 140] : [110 + day % 4 * 10, 120, 130, 140, 130, 140];
                const times = ['00:30', '04:30', '08:30', '12:30', '16:30', '18:30'];
                values.forEach((value, index) => daily.push({ metric, unit: 'ml', value: String(value), date: `${date}T${times[index]}:00+08:00`, date_precision: 'instant' }));
            }
            const selected = daily.filter(inRange);
            groups = operation === 'sum' && selected.length ? [{ metric, unit: 'ml', value: String(selected.reduce((total, group) => total + Number(group.value), 0)) }] : operation === 'series' ? selected : [];
        } else if (items.length && ['height', 'weight'].includes(metric) && (!domain || domain === 'health') && (!entity_id || entity_id === 'synthetic_child')) {
            const values = metric === 'height' ? ['67.8', '68.9', '69.7', '70.4', '71.9', '72.6'] : ['7.4', '7.8', '8.2', '8.6', '9.1', '9.3'];
            const selected = [159, 128, 99, 68, 38, 8].map((day, index) => ({ metric, unit: metric === 'height' ? 'cm' : 'kg', value: values[index]!, date: daysAgo(today, day), date_precision: 'day' })).filter(inRange);
            groups = operation === 'latest' ? selected.slice(-1) : operation === 'series' ? selected : [];
        } else if (items.length && metric === 'amount' && (!domain || domain === 'finance')) {
            const values = ['9000', '10200', '11150', '11600', '11785', '12480'];
            const selected = values.map((value, index) => ({ metric, value, currency: 'CNY', date: new Date(Date.UTC(Number(today.slice(0, 4)), Number(today.slice(5, 7)) - 6 + index, 1)).toISOString().slice(0, 10) })).filter(inRange);
            groups = operation === 'sum' && selected.length ? [{ metric, value: String(selected.reduce((total, group) => total + Number(group.value), 0)), currency: 'CNY' }] : operation === 'series' ? selected : [];
        } else if (items.length && metric === 'score' && operation === 'ratio' && (!domain || domain === 'education')) groups = [{ metric, value: '88', unit: '%', numerator: '88', denominator: '100', sample: '合成课程', rounding: 'exact' }];
        return { snapshot_id: `synthetic_archive_${this.generation}`, data_epoch: 1, generation: this.generation, algorithm_version: 'synthetic-display-fixture-v1', projection: 'public', groups, coverage: { complete: true, included: items.length, excluded: 0, reasons: [] } };
    }
}
