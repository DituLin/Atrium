/** Manual viewer over the client-known entry sequence. DOM key ownership keeps
 * operations Back ahead of the provider's page-level Back listener. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { isWaiting, neighborId } from '../app/photoViewer';
import { mapRemoteKey } from '../ui/keys';
import { photoCaption } from '../ui/photoCaption';
import { RemoteButton } from '../ui/RemoteButton';
import { COLLECTION_LABELS, filmstripIds, viewerPosition } from './media/viewerPosition';
import { usePhotoViewer } from './usePhotoViewer';

const Chevron = (props: { d: string; size: number }) => <svg width={props.size} height={props.size} viewBox="0 0 24 24" fill="none"
  stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><path d={props.d} /></svg>;
const LEFT = 'M15 5l-7 7 7 7';
const RIGHT = 'M9 5l7 7-7 7';

export function PhotoScreen() {
  const { state, dispatch, api, goBack } = useApp();
  const route = state.router.route;
  const photoId = route.name === 'photo' ? route.photoId : '';
  const collection = route.name === 'photo' ? (route.collection ?? null) : null;
  const viewer = usePhotoViewer(photoId, collection);
  const [operations, setOperations] = useState(false);
  const screen = useRef<HTMLDivElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  useEffect(() => { screen.current?.focus(); }, []);
  useEffect(() => { if (operations) closeButton.current?.focus(); }, [operations]);
  const close = () => { setOperations(false); screen.current?.focus(); };
  const go = (direction: 'previous' | 'next') => {
    const target = neighborId(viewer, direction);
    if (target) dispatch({ type: 'router.navigate', route: collection
      ? { name: 'photo', photoId: target, collection } : { name: 'photo', photoId: target } });
  };
  const captionItem = viewer.shownItem ?? viewer.item;
  const caption = captionItem ? photoCaption(captionItem, clockOptions(state.home)) : null;
  const retaining = viewer.shownItem && viewer.renderedId !== viewer.shownItem.id;
  const failed = viewer.status === 'error' || viewer.status === 'missing';
  const previous = neighborId(viewer, 'previous');
  const next = neighborId(viewer, 'next');
  const source = collection ? COLLECTION_LABELS[collection] : '首页';
  const position = operations ? viewerPosition(viewer, state.collection) : null;
  const strip = operations ? filmstripIds(viewer, 2) : [];

  return <div ref={screen} tabIndex={-1} className="screen screen--photo viewer-page surface--ink" onKeyDown={event => {
    const key = mapRemoteKey(event);
    if (!key) return;
    event.preventDefault(); event.stopPropagation();
    if ((key === 'enter' || key === 'back') && event.repeat) return;
    if (key === 'back') { if (operations) close(); else goBack(); }
    else if (key === 'enter') setOperations(true);
    else if (key === 'left' || key === 'right') {
      if (operations) {
        const buttons = Array.from(screen.current?.querySelectorAll<HTMLButtonElement>('.viewer__actions button:not(:disabled)') ?? []);
        const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
        buttons[Math.max(0, Math.min(buttons.length - 1, index + (key === 'left' ? -1 : 1)))]?.focus();
      } else go(key === 'left' ? 'previous' : 'next');
    }
  }}>
    <div className="viewer">
      {retaining ? <img className="viewer__image viewer__image--retained" src={api.mediaUrl(viewer.shownItem!.id, 'preview')} alt="" /> : null}
      {viewer.item && viewer.mediaReady && !failed ? <img className={`viewer__image${viewer.renderedId === viewer.item.id ? '' : ' viewer__image--pending'}`} src={api.mediaUrl(viewer.item.id, 'preview')} alt=""
        key={`${viewer.generation}:${viewer.item.id}`}
        onLoad={() => dispatch({ type: 'viewer.rendered', id: viewer.item!.id, generation: viewer.generation })}
        onError={() => dispatch({ type: 'viewer.imageFailed', id: viewer.item!.id, generation: viewer.generation })} /> : null}
      {isWaiting(viewer) ? <p className="viewer-page__status" role="status"><span className="spinner" aria-hidden="true" />正在准备照片…</p> : null}
      {failed ? <p className="viewer-page__status" role="status">这张照片暂时无法显示。{previous || next ? '可按左右键跳过，或返回。' : '本次照片已无法继续浏览，请返回。'}</p> : null}
    </div>
    <p className="viewer-page__back"><Chevron d={LEFT} size={28} /><span>返回 · {source}</span></p>
    <span className={`viewer-page__chevron viewer-page__chevron--previous${previous ? '' : ' is-disabled'}`} aria-hidden="true"><Chevron d={LEFT} size={36} /></span>
    <span className={`viewer-page__chevron viewer-page__chevron--next${next ? '' : ' is-disabled'}`} aria-hidden="true"><Chevron d={RIGHT} size={36} /></span>
    {operations ? <>
      <div className="viewer-page__scrim" aria-hidden="true" />
      <section className="viewer-layer" role="dialog" aria-modal="true" aria-label="照片操作">
        <div className="viewer-layer__meta">
          <p className="viewer-layer__date serif">{caption?.date || '拍摄时间未知'}{caption?.estimated ? <span className="viewer-layer__estimated"> 估算</span> : null}</p>
          <p className="viewer-layer__position">{position ? `${position} · ` : ''}轮播已暂停</p>
        </div>
        <div className="viewer-layer__strip" aria-hidden="true">
          {strip.map(id => <img key={id} alt="" src={api.mediaUrl(id, 'thumb')} decoding="async"
            className={`viewer-layer__frame${id === photoId ? ' is-current' : ''}`}
            onError={event => { event.currentTarget.style.visibility = 'hidden'; }} />)}
        </div>
        <div className="viewer__actions viewer-layer__actions">
          <RemoteButton ref={closeButton} className="btn btn--ghost-ink" onClick={close}>关闭操作</RemoteButton>
          <RemoteButton className="btn btn--ghost-ink" disabled={!previous} onClick={() => go('previous')}>上一张</RemoteButton>
          <RemoteButton className="btn btn--ghost-ink" disabled={!next} onClick={() => go('next')}>下一张</RemoteButton>
          <RemoteButton className="btn btn--ghost-ink" onClick={goBack}>返回来源</RemoteButton>
        </div>
      </section>
    </> : <footer className="hints viewer-page__hints">
      <span className={previous ? '' : 'is-disabled'}>← 上一张</span>
      <span className={next ? '' : 'is-disabled'}>下一张 →</span>
      <span>OK 照片操作</span>
      <span className="hints__end">返回 回到{source}</span>
    </footer>}
  </div>;
}
