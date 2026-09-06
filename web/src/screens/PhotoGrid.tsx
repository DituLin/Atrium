/**
 * The thumb grid of the collection browser (W-202). Presentation only: focus
 * arithmetic comes from the reducer through `onDirection`, and the caption
 * rules from `ui/photoCaption`.
 */

import type { ReactElement } from 'react';
import { useState } from 'react';

import type { ClockFormatOptions } from '../core/clock';
import type { PhotoItem } from '../types/api';
import { Focusable, FocusGroup } from '../ui/focus';
import type { RemoteKey } from '../ui/keys';
import { captionText, photoCaption } from '../ui/photoCaption';

function Thumb(props: { item: PhotoItem; options: ClockFormatOptions }): ReactElement {
  const [broken, setBroken] = useState(false);
  const caption = photoCaption(props.item, props.options);
  const ready = props.item.preview.status === 'ready';
  return (
    <>
      <div className="thumb__frame">
        {ready && !broken ? (
          <img
            className="thumb__image"
            src={props.item.urls.thumb}
            alt=""
            loading="lazy"
            decoding="async"
            onError={() => setBroken(true)}
          />
        ) : (
          <span className="thumb__placeholder" aria-hidden="true">
            {ready ? '■' : '◔'}
          </span>
        )}
      </div>
      <p className="thumb__caption">
        {caption.date === '' ? 'Date unknown' : caption.date}
        {caption.estimated ? <span className="thumb__estimated"> estimated</span> : null}
      </p>
    </>
  );
}

export interface PhotoGridProps {
  items: readonly PhotoItem[];
  focusIndex: number;
  columns: number;
  options: ClockFormatOptions;
  onDirection: (direction: RemoteKey) => boolean;
  onActivate: (index: number) => void;
  onFocusIndex: (index: number) => void;
}

export function PhotoGrid(props: PhotoGridProps): ReactElement {
  return (
    <FocusGroup
      className="photogrid"
      columns={props.columns}
      count={props.items.length}
      index={props.focusIndex}
      onIndexChange={props.onFocusIndex}
      onDirection={props.onDirection}
      onActivate={props.onActivate}
    >
      {props.items.map((item, index) => (
        <Focusable
          key={item.id}
          index={index}
          className="thumb"
          label={captionText(item, props.options)}
          onActivate={() => props.onActivate(index)}
        >
          <Thumb item={item} options={props.options} />
        </Focusable>
      ))}
    </FocusGroup>
  );
}
