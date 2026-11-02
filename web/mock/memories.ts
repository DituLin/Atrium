/** Synthetic home-only daily order with the same inclusive/exclusive contract. */
import { createHash } from 'node:crypto';
import type { MemoriesResponse } from '../src/types/api.ts';
import type { MockPhoto } from './data.ts';
interface Cursor { day: string; screen: string; key: string; id: string; inclusive: boolean }
const orderKey = (day: string, screen: string, id: string): string => createHash('sha256').update(`memories:${day}:${screen}:${id}`).digest('hex');
const encode = (value: Cursor): string => Buffer.from(JSON.stringify(value)).toString('base64url');
export function memoriesPage(photos: readonly MockPhoto[], day: string, screen: string, raw: string | null, limit: number): MemoriesResponse {
  let cursor: Cursor | null = null;
  if (raw) {
    try { cursor = JSON.parse(Buffer.from(raw, 'base64url').toString('utf8')) as Cursor; } catch { throw new Error('Invalid memories cursor'); }
    if (!cursor || cursor.screen !== screen || typeof cursor.key !== 'string' || typeof cursor.id !== 'string' || typeof cursor.inclusive !== 'boolean') throw new Error('Invalid memories cursor');
    if (cursor.day !== day) cursor = null;
  }
  const ordered = photos.filter(item => item.preview_status === 'ready').map(photo => ({ photo, key: orderKey(day, screen, photo.id) }))
    .sort((a, b) => a.key.localeCompare(b.key) || a.photo.id.localeCompare(b.photo.id));
  const after = ordered.filter(item => !cursor || item.key > cursor.key || (item.key === cursor.key && (item.photo.id > cursor.id || cursor.inclusive && item.photo.id === cursor.id)));
  const window = after.slice(0, limit);
  const items = window.map(({ photo, key }) => ({ id: photo.id, source_id: 'family_photos', captured_at: photo.captured_at,
    captured_confidence: photo.captured_confidence, first_seen_at: photo.first_seen_at, is_baseline: photo.is_baseline,
    width: photo.width, height: photo.height, preview: { status: photo.preview_status, width: photo.width, height: photo.height },
    urls: { preview: `/api/v1/media/photos/${photo.id}?variant=preview`, thumb: `/api/v1/media/photos/${photo.id}?variant=thumb` },
    resume_cursor: encode({ day, screen, key, id: photo.id, inclusive: true }) }));
  const last = window[window.length - 1];
  return { items, next_cursor: last && after.length > window.length ? encode({ day, screen, key: last.key, id: last.photo.id, inclusive: false }) : null,
    meta: { collection: 'memories', day, screen_id: screen, total: ordered.length } };
}
