import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { GRID_COLUMNS, emptyKind, focusedItem } from '../app/photoList';
import type { PhotoCollection } from '../types/api';
import type { RemoteKey } from '../ui/keys';
import { mapRemoteKey } from '../ui/keys';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { PhotoGrid } from './PhotoGrid';
import { usePhotoList } from './usePhotoList';
import { gridSections, moveInSections } from './media/libraryLayout';
import { MediaRail, focusMediaRail } from './media/MediaRail';
import type { MediaEntry } from './media/MediaRail';

const EMPTY_TEXT: Readonly<Record<string, string>> = {
  recent_baseline_only: '暂无首次导入后新增的可展示照片。',
  captured_today: '暂无可展示的今日照片。',
  generic: '这个合集暂时没有照片。',
};
/** Orders as documented by the API (openapi `Collection`). */
const ORDER_TEXT: Readonly<Record<PhotoCollection, string>> = {
  recent: '按加入时间，最新在前',
  captured_today: '今天拍摄，按家庭时区',
  random: '随机排列，本轮不重复',
  all: '按拍摄时间，最新在前',
};
type Region = 'grid' | 'tabs' | 'nav' | 'recovery';

export function PhotosScreen() {
  const { state, dispatch } = useApp();
  const collection: PhotoCollection = state.router.route.name === 'photos' ? state.router.route.collection : 'recent';
  const list = usePhotoList(collection);
  const [region, setRegion] = useState<Region>(() => state.collection.restorePending ? 'grid' : 'tabs');
  const [columns, setColumns] = useState(() => window.innerWidth <= 900 ? 3 : GRID_COLUMNS);
  useEffect(() => {
    const resize = () => setColumns(window.innerWidth <= 900 ? 3 : GRID_COLUMNS);
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);
  const recovery = useRef<HTMLButtonElement>(null);
  const screenRef = useRef<HTMLDivElement>(null);
  const awaitingCollection = useRef<PhotoCollection | null>(null);
  const readyForRoute = list.collection === collection;
  const empty = readyForRoute ? emptyKind(list) : 'none';
  const failed = readyForRoute && list.status === 'error';
  const hasRecovery = empty !== 'none' || failed;
  const options = useMemo(() => clockOptions(state.home), [state.home]);
  const sections = useMemo(() => gridSections(list.items, collection, options), [list.items, collection, options]);
  const focusTab = useCallback(() => { setRegion('tabs'); focusMediaRail(screenRef.current); }, []);
  const focusNav = useCallback(() => { setRegion('nav'); focusTopNav(); }, []);
  const focusContent = () => {
    if (hasRecovery) { setRegion('recovery'); recovery.current?.focus(); }
    else setRegion('grid');
  };

  const restoringOnEntry = useRef(state.collection.restorePending);
  // A fresh screen starts at its selected collection. Loading never moves this
  // focus; only an explicit confirmation transfers ownership to the photo grid.
  useEffect(() => {
    if (!restoringOnEntry.current) {
      const selector = state.router.restoreFocus ? `[data-return-focus="${state.router.restoreFocus}"]` : '[aria-selected="true"]';
      const source = screenRef.current?.querySelector<HTMLElement>(selector);
      (source ?? screenRef.current?.querySelector<HTMLElement>('[aria-selected="true"]'))?.focus();
    }
  }, [state.router.restoreFocus]);

  useLayoutEffect(() => {
    if (!readyForRoute || !list.restorePending) return;
    const grid = screenRef.current?.querySelector<HTMLElement>('.photogrid');
    if (grid) {
      grid.querySelectorAll<HTMLElement>('.thumb')[list.focusIndex]?.focus({ preventScroll: true });
      grid.scrollTop = list.returnFocus?.scrollTop ?? 0;
    }
    dispatch({ type: 'photos.restored' });
  }, [dispatch, readyForRoute, list.restorePending, list.focusIndex, list.returnFocus]);

  useEffect(() => {
    if (!readyForRoute || (list.status !== 'ready' && list.status !== 'error')) return;
    // A confirmed collection applies once after its own response; stale pages cannot claim focus.
    if (awaitingCollection.current === collection) {
      awaitingCollection.current = null;
      if (region === 'tabs') {
        if (hasRecovery) recovery.current?.focus();
        else screenRef.current?.querySelector<HTMLElement>('.thumb')?.focus();
      }
    }
    if (hasRecovery && (region === 'grid' || region === 'recovery')
      && !screenRef.current?.querySelector('.photos__recovery')?.contains(document.activeElement)) recovery.current?.focus();
  }, [collection, readyForRoute, list.status, hasRecovery, region]);

  const applyCollection = (next: PhotoCollection) => {
    dispatch({ type: 'photos.focus', index: 0 });
    if (next === collection && readyForRoute && (list.status === 'ready' || list.status === 'error')) { focusContent(); return; }
    awaitingCollection.current = next;
    setRegion('tabs');
    dispatch({ type: 'router.navigate', route: { name: 'photos', collection: next } });
  };
  const selectEntry = (entry: MediaEntry) => {
    if (entry === 'videos') { awaitingCollection.current = null; dispatch({ type: 'router.navigate', route: { name: 'videos' } }); }
    else applyCollection(entry);
  };
  const onDirection = useCallback((direction: RemoteKey): boolean => {
    const target = moveInSections(sections, list.focusIndex, direction, columns);
    if (target < 0) { if (direction === 'up') focusNav(); else focusTab(); return true; }
    if (target !== list.focusIndex) dispatch({ type: 'photos.focus', index: target });
    return true;
  }, [dispatch, sections, list.focusIndex, columns, focusNav, focusTab]);
  const onActivate = useCallback((index: number) => {
    const item = list.items[index] ?? focusedItem(list);
    if (item) {
      dispatch({ type: 'photos.focus', index });
      dispatch({ type: 'photos.remember', scrollTop: screenRef.current?.querySelector('.photogrid')?.scrollTop ?? 0 });
      dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: item.id, collection } });
    }
  }, [dispatch, list, collection]);
  const onFocusIndex = useCallback((index: number) => dispatch({ type: 'photos.focus', index }), [dispatch]);
  const unknownCount = list.meta?.unknown_captured_count ?? 0;
  const total = readyForRoute && typeof list.meta?.total === 'number' ? list.meta.total : null;

  return <div ref={screenRef} className="screen library surface--ink screen--photos" style={{ '--photo-columns': columns } as React.CSSProperties} onFocusCapture={event => {
    const target = event.target as HTMLElement;
    if (target.closest('.topnav')) { awaitingCollection.current = null; setRegion('nav'); }
    else if (target.closest('.media-rail')) setRegion('tabs');
    else if (target.closest('.photogrid')) setRegion('grid');
    else if (target.closest('.photos__recovery')) setRegion('recovery');
  }}>
    <TopNav onDown={focusTab} />
    <div className="library__body">
      <MediaRail selected={collection} count={total === null ? null : total.toLocaleString('en-US')}
        onSelect={selectEntry} onRight={focusContent} onKey={() => { awaitingCollection.current = null; }} />
      <section className="library__content" aria-label="照片">
        <header className="library__bar">
          <p className="library__order">{ORDER_TEXT[collection]}</p>
          <p className="library__count photos__count">
            {!readyForRoute || list.status === 'loading' || list.status === 'idle' ? '正在加载…'
              : failed ? '数量暂不可用' : total !== null ? `共 ${total.toLocaleString('en-US')} 张` : `已载入 ${list.items.length}${list.cursor ? '+' : ''} 张`}
          </p>
        </header>
        {list.returnNotice ? <p className="library__note" role="status">{list.returnNotice}</p> : null}
        {collection === 'captured_today' && readyForRoute && unknownCount > 0 ? <p className="library__note" role="status">{unknownCount} 张照片缺少拍摄时间，未列入此合集。</p> : null}
        {hasRecovery ? <div className="library__empty">
          <p className="library__empty-text serif" role="status">{failed ? '暂时无法加载此合集。' : EMPTY_TEXT[empty]}</p>
          <div className="photos__recovery library__actions" onKeyDown={event => {
            // Each action owns its activation. Horizontal movement stays within recovery.
            const key = mapRemoteKey(event);
            if (key === 'up' || key === 'down') { event.preventDefault(); event.stopPropagation(); if (key === 'up') focusNav(); }
            if (key === 'left' || key === 'right') {
              event.preventDefault(); event.stopPropagation();
              const buttons = Array.from(event.currentTarget.querySelectorAll('button'));
              const index = buttons.indexOf(event.target as HTMLButtonElement);
              if (key === 'left' && index <= 0) { focusTab(); return; }
              buttons[Math.max(0, Math.min(buttons.length - 1, index + (key === 'left' ? -1 : 1)))]?.focus();
            }
          }}>
            <RemoteButton ref={recovery} className="btn btn--moon" onClick={() => {
              if (collection === 'all' && failed) { dispatch({ type: 'photos.reset' }); dispatch({ type: 'photos.open', collection }); setRegion('grid'); }
              else applyCollection('all');
            }}>{collection === 'all' && failed ? '重试' : '查看全部照片'}</RemoteButton>
            <RemoteButton className="btn btn--ghost-ink" data-return-focus="photos-status" onClick={() => dispatch({ type: 'router.navigate', sourceFocus: 'photos-status', route: { name: 'settings' } })}>查看状态</RemoteButton>
          </div>
        </div> : readyForRoute && list.items.length > 0 ? <PhotoGrid items={list.items} sections={sections} focusIndex={list.focusIndex} active={region === 'grid'}
          columns={columns} options={options} onDirection={onDirection} onActivate={onActivate} onFocusIndex={onFocusIndex} />
          : <div className="library__empty"><p className="library__note" role="status">正在加载照片…</p></div>}
      </section>
    </div>
    <footer className="hints">
      <span>方向键 选择</span><span>OK 打开</span><span>← 回到分类</span><span className="hints__end">返回 回到上一页</span>
    </footer>
  </div>;
}
