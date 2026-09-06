/**
 * Photo captions (W-202, PRD 5.2 photo data rules).
 *
 * A capture time without a timezone is interpreted in the home timezone and
 * must say so ("estimated"); an unknown capture time prints nothing at all,
 * because the file's modification time is never a substitute for it.
 */

import type { ClockFormatOptions } from '../core/clock';
import { formatCapturedDate } from '../core/clock';
import type { PhotoItem } from '../types/api';

export interface PhotoCaption {
  /** Empty when the capture time is unknown. */
  date: string;
  /** True when the date was inferred from a timestamp without a zone. */
  estimated: boolean;
}

export function photoCaption(item: PhotoItem, options: ClockFormatOptions): PhotoCaption {
  if (item.captured_confidence === 'unknown' || item.captured_at === null) {
    return { date: '', estimated: false };
  }
  const date = formatCapturedDate(item.captured_at, options);
  if (date === null) return { date: '', estimated: false };
  return { date, estimated: item.captured_confidence === 'inferred' };
}

/** One line for screen readers and for the grid's `aria-label`. */
export function captionText(item: PhotoItem, options: ClockFormatOptions): string {
  const caption = photoCaption(item, options);
  if (caption.date === '') return 'Photo, capture date unknown';
  return caption.estimated ? `Photo, ${caption.date} (estimated)` : `Photo, ${caption.date}`;
}
