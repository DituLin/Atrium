/**
 * Photo pane (W-201). Two stacked layers crossfade by opacity only — no
 * transform, no filter, no Ken Burns: a TV compositor can animate opacity for
 * free, while anything that re-rasterises a 2560 px image drops frames.
 *
 * The pane also carries the index/import progress line, so an empty library
 * explains itself instead of showing a black rectangle (FR-03).
 */

import type { ReactElement } from 'react';
import { useEffect, useRef } from 'react';
import { mapRemoteKey } from '../ui/keys';
import type { RemoteKey } from '../ui/keys';

import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import type { PhotoWidgetPayload } from '../types/api';
import { photoCaption } from '../ui/photoCaption';
import { useSlideshow } from './useSlideshow';

export interface PhotoPaneProps {
  payload: PhotoWidgetPayload | null;
  interactive?: boolean;
  onDirection?: (key: RemoteKey) => void;
}

function indexLabel(payload: PhotoWidgetPayload): string | null {
  const { index, baseline } = payload;
  if (index.state === 'baseline_import' || baseline.status === 'importing') {
    return `首次导入 · ${index.progress.indexed} / ${index.progress.seen} 项`;
  }
  if (index.state === 'scanning') {
    return `扫描中 · 已索引 ${index.progress.indexed} 项，${index.progress.pending_preview} 项预览待生成`;
  }
  return null;
}

export function PhotoPane(props: PhotoPaneProps): ReactElement {
  const { state, dispatch } = useApp();
  const pane = useRef<HTMLElement>(null);
  useEffect(() => { if (props.interactive) pane.current?.focus(); }, [props.interactive]);
  const slideshow = useSlideshow();
  const { payload } = props;
  const progress = payload ? indexLabel(payload) : null;
  const visibleItem = state.slideshow.round.find(item => item.id === slideshow.shown?.id)
    ?? (slideshow.item?.id === slideshow.shown?.id ? slideshow.item : null);
  const caption = visibleItem ? photoCaption(visibleItem, clockOptions(state.home)) : null;
  const open = () => {
    if (props.interactive && slideshow.shown) dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: slideshow.shown.id } });
  };

  return (
    <section ref={pane} className="widget widget--photos" aria-label={props.interactive ? '打开当前照片' : '照片'}
      role={props.interactive ? 'button' : undefined} tabIndex={props.interactive ? 0 : undefined}
      onClick={open} onKeyDown={event => {
        if (!props.interactive) return;
        const key = mapRemoteKey(event);
        if (key === 'enter') { event.preventDefault(); event.stopPropagation(); if (!event.repeat) open(); }
        else if (key && key !== 'back') { event.preventDefault(); event.stopPropagation(); props.onDirection?.(key); }
      }}>
      <div className="photos__frame">
        {slideshow.previous ? (
          <img
            className="slide slide--out"
            src={slideshow.previous.src}
            alt=""
            aria-hidden="true"
            key={`prev-${slideshow.previous.id}`}
          />
        ) : null}
        {slideshow.shown ? (
          <img
            className="slide slide--in"
            src={slideshow.shown.src}
            alt=""
            key={`shown-${slideshow.shown.id}`}
          />
        ) : null}
        {slideshow.shown === null ? (
          <p className="photos__empty">
            {slideshow.status === 'empty' ? '暂无照片' : '正在加载照片…'}
          </p>
        ) : null}
        {caption ? (
          <p className="slide__caption">
            {caption.date || '拍摄时间未知'}
            {caption.estimated ? <span className="slide__estimated"> 估算</span> : null}
          </p>
        ) : null}
      </div>
      {progress ? (
        <p className="photos__progress" role="status">
          {progress}
        </p>
      ) : null}
      {slideshow.fixed && slideshow.count === 1 ? (
        <p className="photos__progress">当前仅一张照片，持续展示</p>
      ) : null}
      {payload && payload.totals.pending_preview > 0 ? (
        <p className="photos__progress">{payload.totals.pending_preview} 项预览待生成</p>
      ) : null}
    </section>
  );
}
