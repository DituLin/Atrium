/**
 * Ambient photo layer for the home screen (W-201, redesign "首页 · 常驻").
 *
 * Two stacked layers crossfade by opacity only — no transform, no filter, no
 * Ken Burns: a TV compositor can animate opacity for free, while anything that
 * re-rasterises a 2560 px image drops frames. Landscape photos fill the screen;
 * portraits are shown whole on ink (`photoFit`).
 *
 * The pane also carries the index/import progress line, so an empty library
 * explains itself instead of showing a black screen (FR-03).
 */

import type { ReactElement } from 'react';
import { useEffect, useRef } from 'react';

import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { photoFit } from '../screens/home/homeSummary';
import type { PhotoWidgetPayload } from '../types/api';
import type { DecodedImage } from '../ui/decodeImage';
import { mapRemoteKey } from '../ui/keys';
import type { RemoteKey } from '../ui/keys';
import { photoCaption } from '../ui/photoCaption';
import { useSlideshow } from './useSlideshow';

export interface PhotoPaneProps {
  payload: PhotoWidgetPayload | null;
  interactive?: boolean;
  onDirection?: (key: RemoteKey) => void;
  /** Fired when the pane receives focus (the menu closes). */
  onFocus?: () => void;
  /** Take focus on mount; false while home reopens its menu. */
  autoFocus?: boolean;
}

function indexLabel(payload: PhotoWidgetPayload): string | null {
  const { index, baseline } = payload;
  if (index.state === 'baseline_import' || baseline.status === 'importing') {
    return `首次导入 · ${index.progress.indexed} / ${index.progress.seen} 项`;
  }
  // Routine minute-level rescans are not news on an ambient screen; pending
  // previews are reported separately below when there is real work left.
  return null;
}

function slideClass(image: DecodedImage, layer: 'in' | 'out'): string {
  return `ambient__slide ambient__slide--${layer} ambient__slide--${photoFit(image.element.naturalWidth, image.element.naturalHeight)}`;
}

export function PhotoPane(props: PhotoPaneProps): ReactElement {
  const { state, dispatch } = useApp();
  const pane = useRef<HTMLElement>(null);
  const autoFocus = props.autoFocus !== false;
  // Mount-time only: later menu changes move focus themselves.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { if (props.interactive && autoFocus) pane.current?.focus(); }, [props.interactive]);
  const slideshow = useSlideshow();
  const { payload } = props;
  const progress = payload ? indexLabel(payload) : null;
  const visibleItem = state.slideshow.round.find(item => item.id === slideshow.shown?.id)
    ?? (slideshow.item?.id === slideshow.shown?.id ? slideshow.item : null);
  const caption = visibleItem ? photoCaption(visibleItem, clockOptions(state.home)) : null;
  const open = () => {
    if (props.interactive && slideshow.shown) dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: slideshow.shown.id } });
  };
  const notes = [
    progress,
    slideshow.fixed && slideshow.count === 1 ? '当前仅一张照片，持续展示' : null,
    payload && payload.totals.pending_preview > 0 ? `${payload.totals.pending_preview} 项预览待生成` : null,
  ].filter((note): note is string => note !== null);

  return (
    <section ref={pane} className="ambient" data-home-hero=""
      aria-label={props.interactive ? '打开当前照片' : '照片'}
      role={props.interactive ? 'button' : undefined} tabIndex={props.interactive ? 0 : undefined}
      onClick={open} onFocus={props.onFocus} onKeyDown={event => {
        if (!props.interactive) return;
        const key = mapRemoteKey(event);
        if (key === 'enter') { event.preventDefault(); event.stopPropagation(); if (!event.repeat) open(); }
        else if (key && key !== 'back') { event.preventDefault(); event.stopPropagation(); props.onDirection?.(key); }
      }}>
      {slideshow.previous ? (
        <img className={slideClass(slideshow.previous, 'out')} src={slideshow.previous.src} alt="" aria-hidden="true" key={`prev-${slideshow.previous.id}`} />
      ) : null}
      {slideshow.shown ? (
        <img className={slideClass(slideshow.shown, 'in')} src={slideshow.shown.src} alt="" key={`shown-${slideshow.shown.id}`} />
      ) : null}
      {slideshow.shown === null ? (
        <p className="ambient__empty">{slideshow.status === 'empty' ? '暂无照片' : '正在加载照片…'}</p>
      ) : null}
      <div className="ambient__scrim ambient__scrim--top" aria-hidden="true" />
      <div className="ambient__scrim ambient__scrim--bottom" aria-hidden="true" />
      {caption ? (
        <p className="ambient__caption">
          <span className="serif">{caption.date || '拍摄时间未知'}</span>
          {caption.estimated ? <span className="ambient__estimated">拍摄时间为估算</span> : null}
        </p>
      ) : null}
      {notes.length ? (
        <p className="ambient__notes" role="status">{notes.join(' · ')}</p>
      ) : null}
    </section>
  );
}
