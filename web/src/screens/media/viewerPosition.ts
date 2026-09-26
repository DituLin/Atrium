/**
 * What the photo preview can honestly say about its place in a collection and
 * which neighbours it can show in the filmstrip. Only client-known data.
 */

import type { PhotoListState } from '../../app/photoList';
import type { PhotoViewerState } from '../../app/photoViewer';
import type { PhotoCollection } from '../../types/api';

export const COLLECTION_LABELS: Readonly<Record<PhotoCollection, string>> = {
  recent: '最近新增',
  captured_today: '今天拍摄',
  random: '随心看看',
  all: '全部照片',
};

/**
 * "最近新增 · 第 3 / 128 张" when the total is known (a fully loaded list or a
 * server total), "第 3 张" when only the position is, and null when the photo
 * was opened alone (no sequence to count in).
 */
export function viewerPosition(viewer: PhotoViewerState, list: PhotoListState): string | null {
  const index = viewer.sequence.indexOf(viewer.photoId ?? '');
  if (index < 0 || viewer.sequence.length < 2) return null;
  const sameList = viewer.collection !== null && list.collection === viewer.collection;
  let total: number | null = null;
  if (sameList && list.cursor === null && list.items.length === viewer.sequence.length) total = viewer.sequence.length;
  else if (sameList && typeof list.meta?.total === 'number' && list.meta.total >= viewer.sequence.length) total = list.meta.total;
  const position = total === null ? `第 ${index + 1} 张` : `第 ${index + 1} / ${total} 张`;
  return viewer.collection ? `${COLLECTION_LABELS[viewer.collection]} · ${position}` : position;
}

/** Up to `radius` shown neighbours on each side, skipping failed photos. */
export function filmstripIds(viewer: PhotoViewerState, radius = 3): string[] {
  const current = viewer.photoId ?? '';
  const usable = viewer.sequence.filter(id => id === current || !viewer.failedIds.includes(id));
  const index = usable.indexOf(current);
  if (index < 0 || usable.length < 2) return [];
  return usable.slice(Math.max(0, index - radius), index + radius + 1);
}
