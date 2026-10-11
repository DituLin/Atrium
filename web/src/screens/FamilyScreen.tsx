import { useEffect, useMemo, useRef, useState } from 'react';
import { clockOptions } from '../app/homeSelect';
import { useApp } from '../app/context';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { mapRemoteKey } from '../ui/keys';
import { onReadingKey } from './info/readingRegion';
import { ArchiveAnalysisPanel } from './family/ArchiveAnalysisPanel';
import { ArchiveReader } from './family/archiveReader';
import type { ArchiveReadState } from './family/archiveReader';
import type {ApiClient} from '../core/api';
import type { ArchiveDetail, ArchiveFilter } from '../types/archives';
import { DOMAINS, processingLabel, parseLabel, archiveDate, evidenceLocation, factLabel, itemTitle, ownershipLabel } from './family/archiveLabels';
const archiveFocus=new WeakMap<ApiClient,{id:string;index:number;scroll:number}>();
export function FamilyScreen() {
    const { api, state, dispatch, archives } = useApp();
    const reader = useMemo(() => new ArchiveReader(api), [api]);
    const [metric, setMetric] = useState(reader.metric);
    const [value, setValue] = useState<ArchiveReadState>(reader.value);
    const [filter, setFilter] = useState<ArchiveFilter>(() => ({ ...reader.filter }));
    const [documentID, setDocumentID] = useState('');
    const generation = useRef(state.archiveGeneration);
    const [verifiedGeneration, setVerifiedGeneration] = useState(state.archiveGeneration);
    const content = useRef<HTMLDivElement>(null);
    const controls = useRef<(HTMLButtonElement | null)[]>([]);
    const cards = useRef<(HTMLButtonElement | null)[]>([]);
    const back = useRef<HTMLButtonElement>(null);
    const results = useRef<HTMLDivElement>(null);
    const lastView = useRef('/');
    const focusedDetail = useRef('');
    const moreButton = useRef<HTMLButtonElement>(null);
    const refreshButton = useRef<HTMLButtonElement>(null);
    useEffect(() => { if (document.activeElement === document.body)
        controls.current[0]?.focus(); }, []);
    const anchor = useRef(archiveFocus.get(api)??{ id: '', index: 0, scroll: 0 });
    const returning = useRef(archiveFocus.has(api));
    useEffect(() => { const region=content.current;const off = reader.subscribe(() => {setDocumentID(reader.documentID);setFilter({...reader.filter});setMetric(reader.metric);setValue({ ...reader.value });}); void reader.load(); return () => { archiveFocus.set(api,{...anchor.current,scroll:region?.scrollTop??anchor.current.scroll});off(); reader.stop(); }; }, [reader,api]);
    useEffect(() => { if (generation.current !== state.archiveGeneration) {
        generation.current = state.archiveGeneration;
        reader.invalidate();
        setVerifiedGeneration(state.archiveGeneration);
        void reader.load();
    } }, [reader, state.archiveGeneration]);
    useEffect(() => { const resume = () => { reader.invalidate(); if (document.visibilityState !== 'hidden')
        void reader.load(); }; document.addEventListener('visibilitychange', resume); return () => document.removeEventListener('visibilitychange', resume); }, [reader]);
    const route = state.router.route;
    const viewID = route.name === 'family' ? route.archiveViewId : undefined;
    const commandID = route.name === 'family' ? route.commandId : undefined;
    const snapshot = verifiedGeneration === state.archiveGeneration && !state.needsPairing && !state.authExpired && (!commandID || value.snapshot?.viewToken === commandID) ? value.snapshot : null;
    useEffect(() => {
        const key=`${viewID??''}/${commandID??''}`;
        if(lastView.current===key)return;
        lastView.current=key;
        focusedDetail.current='';anchor.current={id:'',index:0,scroll:0};returning.current=false;
        if(content.current)content.current.scrollTop=0;
        if(viewID){void reader.showView(viewID,commandID??'');}
        else {void reader.select(reader.filter);}
        controls.current[0]?.focus();
    }, [reader, viewID, commandID]);
    useEffect(() => { if (!commandID || !viewID)
        return; if (snapshot && snapshot.viewToken === commandID && !documentID && results.current?.dataset.archiveViewToken===commandID && results.current.querySelectorAll('.archive-card').length===snapshot.page.items.length)
        dispatch({ type: 'archives.rendered', commandId: commandID, id: viewID, status: 'ready' });
    else if (value.failed && value.viewToken===commandID)
        dispatch({ type: 'archives.rendered', commandId: commandID, id: null, status: 'error' }); }, [snapshot, value.failed, value.viewToken, commandID, viewID, dispatch, documentID]);
    useEffect(() => archives?.register(route, async (signal) => { reader.invalidate(); await reader.load(); if (signal.aborted || !reader.value.snapshot || reader.value.failed)
        throw new Error('Archive verification failed'); await new Promise<void>(resolve => requestAnimationFrame(() => resolve())); }), [archives, route, reader]);
    const dateLabel = (value: string) => archiveDate(value, clockOptions(state.home));
    const current = snapshot?.detail;
    const entities = snapshot?.entities.items ?? [];
    const choose = (next: ArchiveFilter) => { if (viewID)
        dispatch({ type: 'router.navigate', route: { name: 'family' } }); setFilter(next); setDocumentID(''); void reader.select(next); };
    const open = (id: string, index: number) => { anchor.current = { id, index, scroll: content.current?.scrollTop ?? 0 }; setDocumentID(id); void reader.open(id); };
    const close = () => { focusedDetail.current = ''; returning.current = true; setDocumentID(''); void reader.open(''); };
    useEffect(() => { if (current && focusedDetail.current !== current.item.document_id) {
        focusedDetail.current = current.item.document_id;
        back.current?.focus();
    }
    else if (snapshot && returning.current) {
        returning.current = false;
        const index = snapshot.page.items.findIndex(i => i.document_id === anchor.current.id);
        const target = cards.current[index >= 0 ? index : Math.min(anchor.current.index, snapshot.page.items.length - 1)];
        if (content.current)
            content.current.scrollTop = anchor.current.scroll;
        target?.focus();
    }
    else if (snapshot && !documentID && document.activeElement === document.body ) {
        const i = snapshot.page.items.findIndex(item => item.document_id === anchor.current.id);
        (anchor.current.id?cards.current[i >= 0 ? i : Math.min(anchor.current.index, snapshot.page.items.length - 1)]??controls.current[0]:controls.current[0])?.focus();
    } else if(!snapshot&&document.activeElement===document.body){controls.current[0]?.focus();} }, [current, snapshot, documentID]);
    const choices = [{ label: '全家', next: { ...filter, entity_id: undefined, ownership: undefined }, active: !filter.entity_id && !filter.ownership }, ...entities.filter(e => e.kind === 'person' || e.kind === 'property').map(e => ({ label: `${e.kind === 'property' ? '房屋 · ' : ''}${e.names[0] ?? '待命名'}`, next: { ...filter, entity_id: e.id, ownership: undefined }, active: filter.entity_id === e.id })), ...(snapshot?.entities.next_cursor?[{label:'更多人物与房屋',next:filter,active:false,moreEntities:true}]:[]),{ label: '家庭共有', next: { ...filter, entity_id: undefined, ownership: 'household' }, active: filter.ownership === 'household' }, { label: '待归属', next: { ...filter, entity_id: undefined, ownership: 'unassigned' }, active: filter.ownership === 'unassigned' }, { label: '待确认', next: { ...filter, entity_id: undefined, ownership: 'waiting' }, active: filter.ownership === 'waiting' }, ...(filter.domain === 'health' ? ['volume', 'height', 'weight'].map(value => ({ label: `趋势 · ${factLabel(value)}`, next: filter, active: metric === value, metric: value })) : []), ...['', 'health', 'education', 'finance', 'property'].map(domain => ({ label: domain ? DOMAINS[domain] : '全部领域', next: { ...filter, domain: domain || undefined }, active: (filter.domain ?? '') === domain }))];
    const sideDirection = (i: number, key: string) => { if (key === 'up') {
        if (i === 0)
            focusTopNav('family');
        else
            controls.current[i - 1]?.focus();
    } if (key === 'down')
        (controls.current[i + 1] ?? refreshButton.current)?.focus(); if (key === 'right')
        (documentID ? back.current : cards.current[0] ?? content.current)?.focus(); };
    return <div className="page family-page" onKeyDown={event => { if (documentID && mapRemoteKey(event) === 'back') {
        event.preventDefault();
        event.stopPropagation();
        close();
    } }}>
  <TopNav onDown={() => controls.current[0]?.focus()}/>
  <div className="page__heading"><h1 className="page__title">{viewID ? '投屏查询结果' : '全家档案'}</h1><span role="status" className="muted family-page__status">{value.busy ? '正在核验…' : value.failed ? '暂时无法核验' : '每 15 秒核验'} · 健康 · 教育 · 财务 · 房屋</span></div>
  <main className="page__body family-page__layout"><aside className="family-sidebar"><h2 className="page__section-title">家庭资料</h2>
   {choices.map((choice, i) => <RemoteButton key={`${i}-${choice.label}`} ref={el => { controls.current[i] = el; }} className="family-category" aria-pressed={!viewID && choice.active} onClick={() => { if('moreEntities' in choice){void reader.moreEntities();}else if ('metric' in choice) {
        setMetric(String(choice.metric));
        void reader.selectMetric(String(choice.metric));
    }
    else
        choose(choice.next); }} onDirection={key => sideDirection(i, key)}>{choice.label}</RemoteButton>)}
   <RemoteButton ref={refreshButton} className="btn" onClick={() => void reader.load()} onDirection={key => { if (key === 'up')
        controls.current[choices.length - 1]?.focus(); if (key === 'right')
        content.current?.focus(); }}>刷新档案</RemoteButton>
  </aside><div className="family-content" ref={content} role="region" aria-label="家庭档案与分析" tabIndex={0} onKeyDown={event => { if (event.target === event.currentTarget)
        onReadingKey(event, { left: () => controls.current[0]?.focus(), top: () => focusTopNav('family'), right: () => content.current?.querySelector<HTMLButtonElement>('.archive-preview button:not([disabled])')?.focus(), bottom: () => documentID ? content.current?.querySelector<HTMLButtonElement>('.archive-preview button:not([disabled])')?.focus() : cards.current[0]?.focus() }); }}>
   {!snapshot ? <div className="family-empty"><h2 className="serif">{value.failed ? '等待重新核验' : '正在读取家庭档案'}</h2><p>未核验超过 30 秒的内容会自动隐藏。</p></div> : documentID ? current ? <>
    <RemoteButton ref={back} className="btn" onClick={close} onDirection={key => { if (key === 'down')
            content.current?.focus(); if (key === 'left')
            controls.current[0]?.focus(); }}>返回档案</RemoteButton>
    <h2 className="archive-title serif">{itemTitle(current.item)}</h2><p className="muted">版本 {current.item.document_revision} · {ownershipLabel(current.item.ownership)} · {current.item.dates.map(dateLabel).join(' / ') || '日期未明确'}</p>
    <p>{current.original_saved ? '原件已保存' : '原始记录已保存'} · {processingLabel(current.processing)} · NAS {current.nas_status === 'saved' ? '已同步' : '待同步'}</p>
    {!current.coverage.complete ? <p className="family-message">分析覆盖尚不完整，待确认内容不计入确定统计。</p> : null}
    {current.task_summary?<p className="muted">解析：{parseLabel(current.task_summary.parse)} · 理解：{{pending:'处理中',complete:'已完成',needs_review:'待确认',unavailable:'暂不可用'}[current.task_summary.understanding]} · 归属：{ownershipLabel(current.task_summary.attribution)}</p>:null}
    <h3 className="page__section-title">结构化内容与来源</h3><p className="muted">以下为内容摘录，非原件版式。</p>
    {current.item.evidence.map((e, i) => <article className="archive-evidence" key={i}><h3>{factLabel(e.field_name ?? '')}</h3><p>{e.asserted_value ?? e.excerpt}</p>{e.asserted_value && e.asserted_value !== e.excerpt ? <p className="muted">原文：{e.excerpt}</p> : null}<small>{evidenceLocation(e)}{e.confirmed ? ' · 已确认' : ' · 待核实'}</small></article>)}
    <ArchivePreview key={`${value.epoch}:${current.generation}:${current.item.document_revision}`} detail={current} onRead={() => content.current?.focus()}/>
   </> : <p>该档案暂不可读取。</p> : <>
    {!filter.ownership ? <ArchiveAnalysisPanel analysis={snapshot.analysis} dateLabel={dateLabel}/> : <p className="muted">待归属与待确认内容不纳入确定统计。</p>}
    {!snapshot.page.items.length ? <div className="family-empty"><h2 className="serif">还没有可展示的档案</h2><p>直接发送文本、图片或文件，系统会识别内容和归属，无需先建立家人档案。</p></div> : null}
    {!snapshot.page.coverage.complete?<p className="muted">{snapshot.page.coverage.reason==='presentation_window_limit'?'已达到当前阅读窗口上限，请按人物或领域缩小范围。':'部分档案尚待识别或确认。'}</p>:null}
                <div ref={results} data-archive-view-token={snapshot.viewToken} className="family-history">{snapshot.page.items.map((item, i) => <RemoteButton key={item.document_id} ref={el => { cards.current[i] = el; }} className="family-record archive-card" onFocus={() => { anchor.current = { id: item.document_id, index: i, scroll: content.current?.scrollTop ?? 0 }; }} onClick={() => open(item.document_id, i)} onDirection={key => { if (key === 'left')
            controls.current[0]?.focus(); if (key === 'up')
            (i ? cards.current[i - 1] : content.current)?.focus(); if (key === 'down')
            (cards.current[i + 1] ?? moreButton.current)?.focus(); }}><span className="family-record__date">{item.dates.map(dateLabel).join(' / ') || '日期未明确'}<span>{DOMAINS[item.domain] ?? '待分类'}</span></span><span className="family-record__body"><span className="archive-card__title serif">{itemTitle(item)}</span><span className="archive-card__text">{item.evidence.slice(0, 2).map(e => `${factLabel(e.field_name ?? '')} ${e.asserted_value ?? e.excerpt}`).join(' · ') || '原件已保存'}</span><span className="muted">{ownershipLabel(item.ownership)} · {processingLabel(item.processing??(item.metadata_only?'saved_only':'ready'))} · 版本 {item.document_revision}{item.review_required ? ' · 待核实' : ''}</span></span></RemoteButton>)}</div>
    {snapshot.page.next_cursor ? <RemoteButton ref={moreButton} className="btn family-more" disabled={value.busy} onClick={() => void reader.more()} onDirection={key => { if (key === 'up')
            cards.current[snapshot.page.items.length - 1]?.focus(); if (key === 'left')
            controls.current[0]?.focus(); }}>更早档案</RemoteButton> : null}
   </>}
  </div></main>
 </div>;
}
function ArchivePreview({ detail, onRead }: {
    detail: ArchiveDetail;
    onRead: () => void;
}) {
    const previous = useRef<HTMLButtonElement>(null);
    const next = useRef<HTMLButtonElement>(null);
    const pagingFocus = useRef<'previous' | 'next' | null>(null);
    const { api } = useApp();
    const [page, setPage] = useState(1);
    const [pages, setPages] = useState(1);
    const [image, setImage] = useState('');
    const [failed, setFailed] = useState(false);
    useEffect(() => { const requested = pagingFocus.current; if (!requested)
        return; pagingFocus.current = null; const target = requested === 'previous' ? (page > 1 ? previous.current : next.current) : (page < pages ? next.current : previous.current); if (target && !target.disabled)
        target.focus();
    else
        onRead(); }, [page, pages, onRead]);
    useEffect(() => {
        if (detail.preview !== 'pages')
            return;
        let active = true;
        let url = '';
        const controller = new AbortController();
        void api.getArchivePreview(detail.item.document_id, detail.item.document_revision, page, controller.signal).then(result => { if (!active)
            return; url = URL.createObjectURL(result.blob); setPages(result.pages); setImage(url); }).catch(() => { if (active)
            setFailed(true); });
        return () => { active = false; controller.abort(); if (url)
            URL.revokeObjectURL(url); };
    }, [api, detail.item.document_id, detail.item.document_revision, detail.preview, page]);
    if (detail.preview !== 'pages')
        return <p className="family-message">{detail.preview === 'restricted' ? '当前公开范围不包含整页预览。' : detail.preview === 'unsupported' ? '原件已保存，暂不支持预览。' : '文本、表格与 Office 文件以上方结构化定位展示。'}</p>;
    return <section className="archive-preview"><h3 className="page__section-title">原件预览 · 第 {page} / {pages} 页</h3>{failed ? <p>原件已保存，当前无法生成预览。</p> : image ? <img src={image} alt={`档案原件第 ${page} 页`} onError={() => setFailed(true)}/> : <p>正在准备预览…</p>}<RemoteButton ref={previous} onDirection={key => { if (key === 'right')
        next.current?.focus();
    else
        onRead(); }} className="btn" disabled={page <= 1} onClick={() => { setImage(''); setFailed(false); pagingFocus.current = 'previous'; setPage(p => p - 1); }}>上一页</RemoteButton><RemoteButton ref={next} onDirection={key => { if (key === 'left' && page > 1)
        previous.current?.focus();
    else
        onRead(); }} className="btn" disabled={page >= pages} onClick={() => { setImage(''); setFailed(false); pagingFocus.current = 'next'; setPage(p => p + 1); }}>下一页</RemoteButton></section>;
}
