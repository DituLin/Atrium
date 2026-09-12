import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { GRID_COLUMNS, emptyKind, focusedItem } from '../app/photoList';
import type { PhotoCollection } from '../types/api';
import type { RemoteKey } from '../ui/keys';
import { mapRemoteKey } from '../ui/keys';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { RemoteButton } from '../ui/RemoteButton';
import { PhotoGrid } from './PhotoGrid';
import { usePhotoList } from './usePhotoList';

const COLLECTIONS = [['recent', '最近新增'], ['captured_today', '今天拍摄'], ['random', '随心看看'], ['all', '全部照片']] as const;
const EMPTY_TEXT: Readonly<Record<string, string>> = {
  recent_baseline_only: '暂无首次导入后新增的可展示照片。',
  captured_today: '暂无可展示的今日照片。',
  generic: '这个合集暂时没有照片。',
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
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const recovery = useRef<HTMLButtonElement>(null);
  const screenRef = useRef<HTMLDivElement>(null);
  const awaitingCollection = useRef<PhotoCollection | null>(null);
  const readyForRoute = list.collection === collection;
  const empty = readyForRoute ? emptyKind(list) : 'none';
  const failed = readyForRoute && list.status === 'error';
  const hasRecovery = empty !== 'none' || failed;
  const activeTab = COLLECTIONS.findIndex(([id]) => id === collection);
  const focusTab = useCallback(() => { setRegion('tabs'); tabs.current[activeTab]?.focus(); }, [activeTab]);
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
  const onDirection = useCallback((direction: RemoteKey): boolean => {
    if (direction === 'up' && list.focusIndex < columns) { focusTab(); return true; }
    if (direction === 'down' && list.focusIndex >= Math.floor((list.items.length - 1) / columns) * columns) {
      setRegion('nav'); focusPrimaryNav('photos'); return true;
    }
    dispatch({ type: 'photos.move', direction, columns }); return true;
  }, [dispatch, list.focusIndex, list.items.length, focusTab, columns]);
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

  return <div ref={screenRef} className="screen screen--photos" style={{ '--photo-columns': columns } as React.CSSProperties} onFocusCapture={event => {
    if ((event.target as HTMLElement).closest('.primary-nav')) { awaitingCollection.current = null; setRegion('nav'); }
    else if ((event.target as HTMLElement).closest('.photogrid')) setRegion('grid');
    else if ((event.target as HTMLElement).closest('.photos__recovery')) setRegion('recovery');
  }}>
    <Masthead />
    <header className="photos__header"><h1 className="photos__title">照片</h1><p className="photos__count">
      {!readyForRoute || list.status === 'loading' || list.status === 'idle' ? '正在加载…'
        : failed ? '数量暂不可用' : `已载入 ${list.items.length}${list.cursor ? '+' : ''} 张`}
    </p></header>
    <div className="collection-tabs" role="tablist" aria-label="照片合集">
      {COLLECTIONS.map(([id, label], index) => <RemoteButton key={id} role="tab" aria-selected={id === collection}
        className="collection-tabs__tab" ref={element => { tabs.current[index] = element; }}
        onFocus={() => setRegion('tabs')} onClick={() => applyCollection(id)}
        onDirection={key => {
          awaitingCollection.current = null;
          if (key === 'left' || key === 'right') tabs.current[Math.max(0, Math.min(3, index + (key === 'left' ? -1 : 1)))]?.focus();
          if (key === 'down') focusContent();
          if (key === 'up') { setRegion('nav'); focusPrimaryNav('photos'); }
        }}>{label}</RemoteButton>)}
    </div>
    {list.returnNotice ? <p className="photos__note" role="status">{list.returnNotice}</p> : null}
    {collection === 'captured_today' && readyForRoute && unknownCount > 0 ? <p className="photos__note" role="status">{unknownCount} 张照片缺少拍摄时间，未列入此合集。</p> : null}
    {hasRecovery ? <div className="photos__emptystate">
      <p className="photos__empty" role="status">{failed ? '暂时无法加载此合集。' : EMPTY_TEXT[empty]}</p>
      <div className="photos__recovery" onKeyDown={event => {
        // Each action owns its activation. Horizontal movement stays within recovery.
        const key = mapRemoteKey(event);
        if (key === 'up') { event.preventDefault(); event.stopPropagation(); focusTab(); }
        if (key === 'down') { event.preventDefault(); event.stopPropagation(); setRegion('nav'); focusPrimaryNav('photos'); }
        if (key === 'left' || key === 'right') {
          event.preventDefault(); event.stopPropagation();
          const buttons = Array.from(event.currentTarget.querySelectorAll('button'));
          buttons[Math.max(0, Math.min(buttons.length - 1, buttons.indexOf(event.target as HTMLButtonElement) + (key === 'left' ? -1 : 1)))]?.focus();
        }
      }}>
        <RemoteButton ref={recovery} className="button" onClick={() => {
          if (collection === 'all' && failed) { dispatch({ type: 'photos.reset' }); dispatch({ type: 'photos.open', collection }); setRegion('grid'); }
          else applyCollection('all');
        }}>{collection === 'all' && failed ? '重试' : '查看全部照片'}</RemoteButton>
        <RemoteButton className="button" data-return-focus="photos-status" onClick={() => dispatch({ type: 'router.navigate', sourceFocus: 'photos-status', route: { name: 'settings' } })}>查看状态</RemoteButton>
      </div>
    </div> : readyForRoute && list.items.length > 0 ? <PhotoGrid items={list.items} focusIndex={list.focusIndex} active={region === 'grid'}
      columns={columns} options={clockOptions(state.home)} onDirection={onDirection} onActivate={onActivate} onFocusIndex={onFocusIndex} />
      : <div className="photos__emptystate"><p className="photos__note" role="status">正在加载照片…</p></div>}
    <PrimaryNav onUp={focusTab} />
  </div>;
}
