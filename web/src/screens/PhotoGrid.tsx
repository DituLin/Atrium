/**
 * The thumb grid of the 影像 library (W-202). Presentation only: focus
 * arithmetic comes from the owner through `onDirection`, month sections from
 * `media/libraryLayout`, and the caption rules from `ui/photoCaption`.
 */

import type { ReactElement } from 'react';
import { Fragment, useEffect, useRef, useState } from 'react';

import { useApp } from '../app/context';
import type { ClockFormatOptions } from '../core/clock';
import type { PreparationOutcome } from '../core/mediaPreparation';
import type { PhotoItem } from '../types/api';
import { Focusable, FocusGroup } from '../ui/focus';
import type { RemoteKey } from '../ui/keys';
import { captionText, photoCaption } from '../ui/photoCaption';
import { useInViewport } from '../ui/useInViewport';
import { useMediaPreparation } from '../ui/useMediaPreparation';
import type { GridSection } from './media/libraryLayout';

interface ThumbProps {
  item: PhotoItem;
  options: ClockFormatOptions;
  blocked: boolean;
  prepare: (id: string) => Promise<PreparationOutcome>;
  release: (id: string) => void;
  invalidate: (id: string) => void;
}

function Thumb(props: ThumbProps): ReactElement {
  const [broken, setBroken] = useState(false);
  const [prepared, setPrepared] = useState({ version: -1, ready: false });
  const { ref, visible, version } = useInViewport();
  const ready = prepared.version === version && prepared.ready;
  const caption = photoCaption(props.item, props.options);
  const { item, prepare, release, invalidate, blocked } = props;
  const metadataStatus = useRef(item.preview.status);
  useEffect(() => {
    if (metadataStatus.current !== item.preview.status) {
      metadataStatus.current = item.preview.status;
      invalidate(item.id);
    }
    if (!visible || blocked || item.preview.status === 'failed') return;
    let cancelled = false;
    void prepare(item.id).then(outcome => {
      if (cancelled) return;
      setPrepared({ version, ready: outcome === 'ready' });
      if (outcome === 'ready') setBroken(false);
    });
    return () => { cancelled = true; release(item.id); };
  }, [visible, version, blocked, item.id, item.preview.status, prepare, release, invalidate]);
  return (
    <div className="tile__frame" ref={ref}>
      {ready && !broken && !blocked ? (
        <img
          className="tile__image"
          src={props.item.urls.thumb}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setBroken(true)}
        />
      ) : (
        <span className="tile__placeholder" aria-hidden="true">
          {ready ? '■' : '◔'}
        </span>
      )}
      <p className="tile__caption">
        {caption.date === '' ? '拍摄时间未知' : caption.date}
        {caption.estimated ? <span className="tile__estimated"> 估算</span> : null}
      </p>
    </div>
  );
}

export interface PhotoGridProps {
  items: readonly PhotoItem[];
  sections: readonly GridSection[];
  focusIndex: number;
  active?: boolean;
  columns: number;
  options: ClockFormatOptions;
  onDirection: (direction: RemoteKey) => boolean;
  onActivate: (index: number) => void;
  onFocusIndex: (index: number) => void;
}

export function PhotoGrid(props: PhotoGridProps): ReactElement {
  const { api, state } = useApp();
  const blocked = state.authExpired || state.needsPairing;
  const { prepare, release, invalidate } = useMediaPreparation(api, state.collection.generation, blocked, 'thumb');
  const headerAt = new Map<number, string>();
  for (const section of props.sections) if (section.label !== null) headerAt.set(section.start, section.label);
  return (
    <FocusGroup
      className="photogrid media-grid"
      active={props.active ?? true}
      columns={props.columns}
      count={props.items.length}
      index={props.focusIndex}
      onIndexChange={props.onFocusIndex}
      onDirection={props.onDirection}
      onActivate={props.onActivate}
    >
      {props.items.map((item, index) => {
        const header = headerAt.get(index);
        return (
          <Fragment key={item.id}>
            {header !== undefined ? <h2 className="media-grid__month serif">{header}</h2> : null}
            <Focusable
              index={index}
              className="thumb tile"
              label={captionText(item, props.options)}
              onActivate={() => props.onActivate(index)}
            >
              <Thumb item={item} options={props.options} blocked={blocked} prepare={prepare} release={release} invalidate={invalidate} />
            </Focusable>
          </Fragment>
        );
      })}
    </FocusGroup>
  );
}
