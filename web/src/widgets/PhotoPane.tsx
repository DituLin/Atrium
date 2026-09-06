/**
 * Photo pane (W-201). Two stacked layers crossfade by opacity only — no
 * transform, no filter, no Ken Burns: a TV compositor can animate opacity for
 * free, while anything that re-rasterises a 2560 px image drops frames.
 *
 * The pane also carries the index/import progress line, so an empty library
 * explains itself instead of showing a black rectangle (FR-03).
 */

import type { ReactElement } from 'react';

import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import type { PhotoWidgetPayload } from '../types/api';
import { photoCaption } from '../ui/photoCaption';
import { useSlideshow } from './useSlideshow';

export interface PhotoPaneProps {
  payload: PhotoWidgetPayload | null;
}

function indexLabel(payload: PhotoWidgetPayload): string | null {
  const { index, baseline } = payload;
  if (index.state === 'baseline_import' || baseline.status === 'importing') {
    return `Importing library — ${index.progress.indexed} of ${index.progress.seen} files`;
  }
  if (index.state === 'scanning') {
    return `Scanning — ${index.progress.indexed} indexed, ${index.progress.pending_preview} previews pending`;
  }
  return null;
}

export function PhotoPane(props: PhotoPaneProps): ReactElement {
  const { state } = useApp();
  const slideshow = useSlideshow();
  const { payload } = props;
  const progress = payload ? indexLabel(payload) : null;
  const caption = slideshow.item ? photoCaption(slideshow.item, clockOptions(state.home)) : null;

  return (
    <section className="widget widget--photos" aria-label="Photos">
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
            {slideshow.status === 'empty' ? 'No photos yet' : 'Loading photos…'}
          </p>
        ) : null}
        {caption && caption.date !== '' ? (
          <p className="slide__caption">
            {caption.date}
            {caption.estimated ? <span className="slide__estimated"> estimated</span> : null}
          </p>
        ) : null}
      </div>
      {progress ? (
        <p className="photos__progress" role="status">
          {progress}
        </p>
      ) : null}
      {slideshow.fixed && slideshow.count === 1 ? (
        <p className="photos__progress">Only one photo is available — showing it continuously</p>
      ) : null}
      {payload && payload.totals.pending_preview > 0 ? (
        <p className="photos__progress">{payload.totals.pending_preview} previews pending</p>
      ) : null}
    </section>
  );
}
