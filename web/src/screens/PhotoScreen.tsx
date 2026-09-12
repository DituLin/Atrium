/** Manual viewer over the client-known entry sequence. DOM key ownership keeps
 * operations Back ahead of the provider's page-level Back listener. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { isWaiting, neighborId } from '../app/photoViewer';
import { mapRemoteKey } from '../ui/keys';
import { photoCaption } from '../ui/photoCaption';
import { RemoteButton } from '../ui/RemoteButton';
import { usePhotoViewer } from './usePhotoViewer';

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

  return <div ref={screen} tabIndex={-1} className="screen screen--photo" onKeyDown={event => {
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
      {isWaiting(viewer) ? <p className="viewer__status" role="status"><span className="spinner" aria-hidden="true" />正在准备照片…</p> : null}
      {failed ? <p className="viewer__status" role="status">这张照片暂时无法显示。{previous || next ? '可按左右键跳过，或返回。' : '本次照片已无法继续浏览，请返回。'}</p> : null}
      {operations ? <section className="viewer__operations" role="dialog" aria-modal="true" aria-label="照片操作">
        <p>{caption?.date || '拍摄时间未知'}{caption?.estimated ? ' · 估算' : ''} · 手动浏览，轮播已暂停</p>
        <div className="viewer__actions">
          <RemoteButton ref={closeButton} className="button" onClick={close}>关闭操作</RemoteButton>
          <RemoteButton className="button" disabled={!previous} onClick={() => go('previous')}>上一张</RemoteButton>
          <RemoteButton className="button" disabled={!next} onClick={() => go('next')}>下一张</RemoteButton>
          <RemoteButton className="button" onClick={goBack}>返回来源</RemoteButton>
        </div>
      </section> : null}
    </div>
    <footer className="viewer__bar">
      <p className="viewer__caption">{caption?.date || '拍摄时间未知'}{caption?.estimated ? <span className="viewer__estimated"> 估算</span> : null}</p>
      <div className="viewer__hints">
        <span className={previous ? '' : 'is-disabled'}>◀ 上一张</span>
        <span className={next ? '' : 'is-disabled'}>下一张 ▶</span>
        <RemoteButton tabIndex={operations ? -1 : 0} className="button" onClick={() => setOperations(true)}>照片操作</RemoteButton>
        <RemoteButton tabIndex={operations ? -1 : 0} className="button" onClick={goBack}>返回</RemoteButton>
      </div>
    </footer>
  </div>;
}
