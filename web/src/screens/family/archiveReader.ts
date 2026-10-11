import type { ApiClient } from '../../core/api';
import type { ArchiveAnalysis, ArchiveDetail, ArchiveEntities, ArchiveFilter, ArchivePage } from '../../types/archives';
export interface ArchiveSnapshot {
    viewToken: string;
    filter: ArchiveFilter;
    page: ArchivePage;
    entities: ArchiveEntities;
    analysis: ArchiveAnalysis;
    detail: ArchiveDetail | null;
    at: number;
}
export interface ArchiveReadState {
    snapshot: ArchiveSnapshot | null;
    busy: boolean;
    failed: boolean;
    epoch: number;
}
const navigation = new WeakMap<ApiClient, {
    filter: ArchiveFilter;
    pages: number;
    metric: string;
}>();
/** Owns only verified archive content. Other API successes cannot renew it.
 * Every poll rebuilds the complete loaded window, including expanded history. */
export class ArchiveReader {
    value: ArchiveReadState = { snapshot: null, busy: false, failed: false, epoch: 0 };
    metric = 'volume';
    filter: ArchiveFilter = {};
    documentID = '';
    viewID = '';
    private viewToken = '';
    private pages = 1;
 private entityPages=1;
    private sequence = 0;
    private controller: AbortController | null = null;
    private listeners = new Set<() => void>();
    private timer: number | null = null;
    private stopped = false;
    constructor(private api: ApiClient) { const saved = navigation.get(api); if (saved) {
        this.filter = { ...saved.filter };
        this.pages = saved.pages;
        this.metric = saved.metric;
    } }
    private start() {
        if (this.timer !== null)
            return;
        this.stopped = false;
        this.timer = window.setInterval(() => {
            if (this.value.snapshot && Date.now() - this.value.snapshot.at >= 30000)
                this.invalidate();
            if (!this.value.busy && (!this.value.snapshot || Date.now() - this.value.snapshot.at >= 15000))
                void this.load();
        }, 1000);
    }
    subscribe(fn: () => void): () => void { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; }
    private emit() { for (const fn of this.listeners)
        fn(); }
    invalidate() { this.sequence++; this.controller?.abort(); this.value = { snapshot: null, busy: false, failed: false, epoch: this.value.epoch + 1 }; this.emit(); }
    showView(id: string, token: string) { this.viewID = id; this.viewToken = token; this.documentID = ''; this.pages = 1; this.invalidate(); return this.load(); }
    select(filter: ArchiveFilter) { this.viewID = ''; this.viewToken = ''; this.filter = { ...filter }; this.documentID = ''; this.pages = 1; this.invalidate(); return this.load(); }
    open(id: string) { this.documentID = id; this.invalidate(); return this.load(); }
    selectMetric(metric: string) { this.metric = metric; this.invalidate(); return this.load(); }
    more() { if (this.value.snapshot?.page.next_cursor && this.pages < 20)
        this.pages++; return this.load(); }
    moreEntities(){if(this.entityPages<10)this.entityPages++;this.invalidate();return this.load();}
 stop() { if (!this.viewID)
        navigation.set(this.api, { filter: { ...this.filter }, pages: this.pages, metric: this.metric }); this.stopped = true; if (this.timer !== null)
        window.clearInterval(this.timer); this.timer = null; this.invalidate(); this.listeners.clear(); }
    async load(): Promise<void> {
        this.start();
        const started = Date.now();
        const seq = ++this.sequence;
        this.controller?.abort();
        const controller = new AbortController();
        this.controller = controller;
        const deadline = window.setTimeout(() => controller.abort(), 12000);
        const filter = { ...this.filter };
        const documentID = this.documentID;
        this.value = { ...this.value, busy: true };
        this.emit();
        try {
            const view = this.viewID ? await this.api.getArchiveView(this.viewID, controller.signal) : null;
            if (view && view.archive_view_id !== this.viewID)
                throw new Error('View mismatch');
            const page = view?.page ?? await this.api.getArchives({ ...filter, limit: 10 }, controller.signal);
            let cursor = page.next_cursor;
            const generation = page.generation;
            const seen = new Set<string>();
            for (let i = 1; i < this.pages && cursor; i++) {
                if (seen.has(cursor))
                    throw new Error('Repeated cursor');
                seen.add(cursor);
                const more = this.viewID ? (await this.api.getArchiveView(this.viewID, controller.signal, cursor)).page : await this.api.getArchives({ ...filter, limit: 10, cursor }, controller.signal);
                if (more.generation !== generation)
                    throw new Error('Generation changed');
                page.items = page.items.concat(more.items);
                cursor = more.next_cursor;
                page.coverage.complete = page.coverage.complete && more.coverage.complete;
            }
            page.next_cursor = cursor;
            if(this.pages===20&&cursor){page.next_cursor=undefined;page.coverage={complete:false,reason:'presentation_window_limit'};}
            const query = { domain: filter.domain, entity_id: filter.entity_id, query: filter.query };
            const [entities, analysis, detail] = await Promise.all([
                this.api.getArchiveEntities(controller.signal),
                view ? Promise.resolve(view.analysis) : this.api.analyzeArchives(filter.ownership ? { ...query, operation: 'count', count: 'documents' } : filter.domain === 'finance' ? { ...query, operation: 'sum', metric: 'amount' } : filter.domain === 'education' ? { ...query, operation: 'ratio', metric: 'score' } : filter.domain === 'health' ? { ...query, operation: 'series', metric: this.metric } : { ...query, operation: 'count', count: 'documents' }, controller.signal),
                documentID ? this.api.getArchiveDocument(documentID, controller.signal) : Promise.resolve(null),
            ]);
            let entityCursor=entities.next_cursor;const entityCursors=new Set<string>();
            for(let i=1;i<this.entityPages&&entityCursor;i++){
             if(entityCursors.has(entityCursor))throw new Error('Repeated entity cursor');entityCursors.add(entityCursor);
             const next=await this.api.getArchiveEntities(controller.signal,entityCursor);entities.items=entities.items.concat(next.items);entityCursor=next.next_cursor;entities.coverage.complete=entities.coverage.complete&&next.coverage.complete;
            }
            entities.next_cursor=this.entityPages<10?entityCursor:undefined;
            if (`${analysis.data_epoch}:${analysis.generation}` !== generation)
                throw new Error('Analysis generation changed');
            if (detail && detail.generation !== generation)
                throw new Error('Generation changed');
            if (this.viewID)
                await this.api.getArchiveView(this.viewID, controller.signal);
            if (seq !== this.sequence || controller.signal.aborted || this.stopped)
                return;
            this.value = { snapshot: { viewToken: this.viewToken, filter, page, entities, analysis, detail, at: started }, busy: false, failed: false, epoch: this.value.epoch };
            this.emit();
        }
        catch {
            if (seq === this.sequence && !this.stopped) {
                this.value = { ...this.value, snapshot: this.viewID ? null : this.value.snapshot, busy: false, failed: true };
                this.emit();
            }
        }
        finally {
            window.clearTimeout(deadline);
        }
    }
}
