import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { ApiError } from '../core/api';
import type { VideoItem } from '../types/api';
import { RemoteButton } from '../ui/RemoteButton';
import type { RemoteKey } from '../ui/keys';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { VideoPlayer } from '../video/VideoPlayer';
import { MediaRail, focusMediaRail } from './media/MediaRail';
import type { MediaEntry } from './media/MediaRail';
import { videoFacts } from './media/videoFacts';

const validID = (id: string) => /^[A-Za-z0-9_-]{1,64}$/.test(id);
const durationLabel = (item: VideoItem) => {
  if (!item.metadata) return item.status === 'pending' ? '正在整理' : item.status === 'unsupported' ? '格式暂不支持' : '视频';
  const seconds = Math.max(0, Math.floor(item.metadata.duration_ms / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
};

const Play = () => <svg className="tile__glyph" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5v14l11-7z" /></svg>;

/** Keep the list mounted beneath its preview so Back restores the same DOM and scroll. */
export function VideosScreen() {
  const { state, dispatch, api, videos, goBack } = useApp();
  const route = state.router.route;
  const selected = route.name === 'video' ? route.videoId : null;
  const [items, setItems] = useState<VideoItem[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [listStatus, setListStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  const [request, setRequest] = useState({ cursor: null as string | null, generation: 0 });
  const [detail, setDetail] = useState<VideoItem | null>(null);
  const [detailStatus, setDetailStatus] = useState<'loading' | 'ready' | 'error' | 'gone'>('loading');
  const [detailRetry, setDetailRetry] = useState(0);
  const owner = `${selected}:${state.connection.status}:${detailRetry}`;
  const [detailOwner, setDetailOwner] = useState(owner);
  if (detailOwner !== owner) {
    setDetailOwner(owner); setDetail(null); setDetailStatus('loading');
  }
  const cards = useRef(new Map<string, HTMLButtonElement>());
  const grid = useRef<HTMLDivElement>(null);
  const recovery = useRef<HTMLButtonElement>(null);
  const loadMore = useRef<HTMLButtonElement>(null);
  const previewBack = useRef<HTMLButtonElement>(null);
  const focusId = useRef<string | null>(null);
  const pageFocus = useRef<number | null>(null);
  const detailSequence = useRef(0);
  const listWindow = useRef({ items, cursor, listStatus });
  useLayoutEffect(() => { listWindow.current = { items, cursor, listStatus }; }, [items, cursor, listStatus]);
  const wasPreview = useRef(false);
  const [columns, setColumns] = useState(() => window.innerWidth <= 900 ? 3 : 4);

  useEffect(() => {
    const resize = () => setColumns(window.innerWidth <= 900 ? 3 : 4);
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);
  useEffect(() => {
    const abort = new AbortController();
    void api.listVideos({ limit: 50, cursor: request.cursor }, abort.signal).then(result => {
      if (abort.signal.aborted) return;
      const safe = result.items.filter(item => validID(item.id));
      setItems(previous => request.cursor ? [...new Map([...previous, ...safe].map(item => [item.id, item])).values()] : safe);
      setCursor(result.next_cursor);
      setListStatus('ready');
    }, () => { if (!abort.signal.aborted) setListStatus('error'); });
    return () => abort.abort();
  }, [api, request]);

  // Revalidate the selected item on entry, reconnect and periodically. A revision
  // change remounts the player in preview; gone/failed authorization removes it.
  useEffect(() => {
    if (!selected) return;
    let disposed = false;
    let active: AbortController | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = async () => {
      if (disposed || active || document.visibilityState !== 'visible') return;
      const abort = new AbortController(); active = abort;
      const sequence = ++detailSequence.current;
      try {
        const result = await api.getVideo(selected, abort.signal);
        if (!disposed && !abort.signal.aborted && sequence === detailSequence.current) {
          if (result.item.id !== selected) { setDetail(null); setDetailStatus('gone'); }
          else { setDetail(result.item); setDetailStatus('ready'); }
        }
      } catch (error) {
        if (!disposed && !abort.signal.aborted && sequence === detailSequence.current) {
          setDetail(null);
          setDetailStatus(error instanceof ApiError && error.isGone ? 'gone' : 'error');
        }
      } finally {
        active = null;
        if (!disposed) timer = setTimeout(() => { void refresh(); }, 30_000);
      }
    };
    const foreground = () => {
      if (document.visibilityState === 'visible') { clearTimeout(timer); void refresh(); }
    };
    void refresh();
    document.addEventListener('visibilitychange', foreground);
    return () => { disposed = true; active?.abort(); clearTimeout(timer); document.removeEventListener('visibilitychange', foreground); };
  }, [api, selected, state.connection.status, detailRetry]);

  useEffect(() => {
    if (!videos) return;
    return videos.register(selected ? { name: 'video', videoId: selected } : { name: 'videos' }, async signal => {
      if (selected) {
        const sequence = ++detailSequence.current;
        try {
          const result = await api.getVideo(selected, signal);
          if (signal.aborted || sequence !== detailSequence.current) throw new DOMException('Refresh superseded', 'AbortError');
          if (result.item.id !== selected) throw new Error('Video identity changed');
          setDetail(result.item); setDetailStatus('ready');
        } catch (error) {
          if (!signal.aborted && sequence === detailSequence.current) {
            setDetail(null); setDetailStatus(error instanceof ApiError && error.isGone ? 'gone' : 'error');
          }
          throw error;
        }
        return;
      }
      const before = listWindow.current;
      if (before.listStatus === 'loading') throw new Error('Video list is loading');
      const anchor = focusId.current;
      const collected = new Map<string, VideoItem>();
      const seen = new Set<string>();
      let next: string | null = null;
      const pages = Math.max(1, Math.ceil(before.items.length / 50));
      for (let page = 0; page < pages; page++) {
        const result = await api.listVideos({ limit: 50, cursor: next }, signal);
        if (signal.aborted || listWindow.current !== before || focusId.current !== anchor) throw new DOMException('Video list changed', 'AbortError');
        for (const item of result.items) if (validID(item.id)) collected.set(item.id, item);
        next = result.next_cursor;
        if (!next) break;
        if (seen.has(next)) throw new Error('Video cursor did not advance');
        seen.add(next);
      }
      // New rows may shift the current card beyond our loaded window. Keep its
      // position rather than treating an unobserved card as removed.
      if (next && anchor && !collected.has(anchor)) throw new Error('Video refresh deferred');
      setItems([...collected.values()]); setCursor(next); setListStatus('ready');
    });
  }, [api, videos, selected]);

  useLayoutEffect(() => {
    if (selected) { wasPreview.current = true; return; }
    const returning = wasPreview.current;
    wasPreview.current = false;
    if (listStatus === 'ready' && pageFocus.current !== null) {
      const item = items[Math.min(pageFocus.current, items.length - 1)];
      pageFocus.current = null;
      if (item) { focusId.current = item.id; cards.current.get(item.id)?.focus(); cards.current.get(item.id)?.scrollIntoView?.({ block: 'nearest' }); }
      else recovery.current?.focus();
    } else if (listStatus === 'ready' && (returning || !focusId.current || !cards.current.has(focusId.current))) {
      const id = focusId.current && cards.current.has(focusId.current) ? focusId.current : items[0]?.id;
      if (id && cards.current.has(id)) {
        focusId.current = id;
        cards.current.get(id)?.focus({ preventScroll: true });
      } else recovery.current?.focus();
    } else if (listStatus === 'error' && !items.length) recovery.current?.focus();
  }, [selected, listStatus, items]);
  useLayoutEffect(() => {
    if (selected && (detailStatus !== 'ready' || detail?.status !== 'ready')) previewBack.current?.focus();
  }, [selected, detailStatus, detail]);
  const focusCard = (index: number) => {
    const item = items[Math.max(0, Math.min(items.length - 1, index))];
    if (!item) { recovery.current?.focus(); return; }
    focusId.current = item.id;
    cards.current.get(item.id)?.focus();
    cards.current.get(item.id)?.scrollIntoView?.({ block: 'nearest' });
  };
  const screenRef = useRef<HTMLDivElement>(null);
  const focusRail = () => focusMediaRail(screenRef.current);
  const focusContent = () => {
    const index = items.findIndex(item => item.id === focusId.current);
    if (items.length) focusCard(index < 0 ? 0 : index); else recovery.current?.focus();
  };
  const selectEntry = (entry: MediaEntry) => {
    if (entry === 'videos') focusContent();
    else dispatch({ type: 'router.navigate', route: { name: 'photos', collection: entry } });
  };
  const direction = (key: RemoteKey, index: number) => {
    if (key === 'up') { if (index < columns) recovery.current?.focus(); else focusCard(index - columns); }
    if (key === 'down') {
      if (index + columns < items.length) focusCard(index + columns);
      else if (cursor) loadMore.current?.focus();
    }
    if (key === 'left') { if (index % columns === 0) focusRail(); else focusCard(index - 1); }
    if (key === 'right' && (index + 1) % columns !== 0) focusCard(index + 1);
  };
  const refreshList = () => { setListStatus('loading'); setRequest(previous => ({ cursor: null, generation: previous.generation + 1 })); };
  const playable = selected && detail?.id === selected && detailStatus === 'ready' && detail.status === 'ready';
  const selectedIndex = selected ? items.findIndex(item => item.id === selected) : -1;
  const facts = videoFacts(detail, selectedIndex, cursor === null ? items.length : null);
  return <>
    <div ref={screenRef} className="screen library surface--ink screen--videos" style={selected ? { display: 'none' } : undefined}>
      <TopNav onDown={focusRail} />
      <div className="library__body">
        <MediaRail selected="videos" onSelect={selectEntry} onRight={focusContent} />
        <section className="library__content" aria-label="视频">
          <header className="library__bar">
            <RemoteButton ref={recovery} className="btn btn--ghost-ink library__refresh" onClick={refreshList} onDirection={key => {
              if (key === 'down' && items.length) focusCard(0);
              if (key === 'up') focusTopNav();
              if (key === 'left') focusRail();
            }}>{listStatus === 'error' ? '重试' : '刷新'}</RemoteButton>
            <p className="library__count">{listStatus === 'loading' ? '正在读取…' : listStatus === 'error' ? '数量暂不可用' : `已载入 ${items.length}${cursor ? '+' : ''} 段 · 按加入时间`}</p>
          </header>
          {listStatus === 'error' && <p className="library__note" role="status">暂时无法读取视频列表</p>}
          {listStatus === 'loading' && <p className="library__note" role="status">正在读取视频…</p>}
          {listStatus === 'ready' && !items.length && <div className="library__empty"><p className="library__empty-text serif" role="status">还没有视频</p></div>}
          <div className="videos__scroll" ref={grid} role="region" aria-label="视频列表">
            <div className="media-grid videos__grid" style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}>
              {items.map((item, index) => <RemoteButton key={item.id} className="videos__card tile" aria-label={`视频 ${index + 1}`}
                ref={element => { if (element) cards.current.set(item.id, element); else cards.current.delete(item.id); }}
                onFocus={() => { focusId.current = item.id; }} onDirection={key => direction(key, index)}
                onClick={() => { focusId.current = item.id; dispatch({ type: 'router.navigate', route: { name: 'video', videoId: item.id } }); }}>
                <span className="tile__frame">{item.status === 'ready' && <img className="tile__image" src={`/api/v1/media/videos/${item.id}/cover`} alt="" loading="lazy" />}
                  <span className="tile__badge"><Play /><span>{durationLabel(item)}</span></span>
                  <span className="tile__caption">影像 {String(index + 1).padStart(2, '0')}</span></span>
              </RemoteButton>)}
            </div>
            {cursor && <RemoteButton ref={loadMore} className="btn btn--ghost-ink videos__more" disabled={listStatus === 'loading'}
              onClick={() => { pageFocus.current = items.length; setListStatus('loading'); setRequest(previous => ({ cursor, generation: previous.generation + 1 })); }}
              onDirection={key => { if (key === 'up') focusCard(items.length - 1); if (key === 'left') focusRail(); }}>加载更多</RemoteButton>}
          </div>
        </section>
      </div>
      <footer className="hints">
        <span>方向键 选择</span><span>OK 播放</span><span>← 回到分类</span><span className="hints__end">返回 回到上一页</span>
      </footer>
    </div>
    {selected && (playable ? <VideoPlayer id={detail.id} revision={detail.revision} onBack={goBack} {...facts} /> :
      <section className="screen library surface--ink videos__unavailable" aria-label="视频预览">
        <h1 className="serif">家庭影像</h1>
        <p role="status">{detailStatus === 'loading' ? '正在读取视频…' : detailStatus === 'gone' ? '此视频已不可用' : detailStatus === 'error' ? '暂时无法读取此视频' : detail?.status === 'pending' ? '视频正在整理，请稍后再试' : '此视频格式暂不支持'}</p>
        <div className="videos__unavailable-actions">
          <RemoteButton className="btn btn--ghost-ink" onClick={() => setDetailRetry(value => value + 1)} onDirection={key => { if (key === 'down' || key === 'right') previewBack.current?.focus(); }}>重新检查</RemoteButton>
          <RemoteButton ref={previewBack} className="btn btn--moon" onClick={goBack} onDirection={key => {
            const previous = previewBack.current?.previousElementSibling;
            if ((key === 'up' || key === 'left') && previous instanceof HTMLButtonElement) previous.focus();
          }}>返回视频</RemoteButton>
        </div>
      </section>)}
  </>;
}
