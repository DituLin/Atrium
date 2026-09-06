/**
 * Collection queries for the mock core (design §6.4).
 *
 * `recent`, `captured_today` and `all` page with an opaque keyset cursor
 * (base64 of `sortKey|id`), exactly like the real store will. `random` shuffles
 * the eligible ids with a per-round seed, pages with `seed:offset`, and returns
 * `next_cursor: null` at the end of the round so the client reseeds.
 */

import { PHOTOS, homeDay } from './data.ts';
import type { MockPhoto } from './data.ts';

export interface MockCollectionResult {
  items: MockPhoto[];
  nextCursor: string | null;
  meta: Record<string, unknown>;
}

/** Screens only ever see photos that are indexed; previews may still be cold. */
function eligible(): MockPhoto[] {
  return PHOTOS.slice();
}

function encodeCursor(key: string, id: string): string {
  return Buffer.from(`${key}|${id}`, 'utf8').toString('base64url');
}

function decodeCursor(cursor: string | null): { key: string; id: string } | null {
  if (!cursor) return null;
  const raw = Buffer.from(cursor, 'base64url').toString('utf8');
  const index = raw.lastIndexOf('|');
  if (index < 0) return null;
  return { key: raw.slice(0, index), id: raw.slice(index + 1) };
}

/** PCG-ish 32-bit hash so a string seed produces a stable shuffle. */
function hashSeed(seed: string): number {
  let hash = 2166136261;
  for (let i = 0; i < seed.length; i += 1) {
    hash ^= seed.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function mulberry32(seed: number): () => number {
  let state = seed;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function shuffled(items: MockPhoto[], seed: string): MockPhoto[] {
  const random = mulberry32(hashSeed(seed));
  const out = items.slice();
  for (let i = out.length - 1; i > 0; i -= 1) {
    const j = Math.floor(random() * (i + 1));
    const a = out[i] as MockPhoto;
    const b = out[j] as MockPhoto;
    out[i] = b;
    out[j] = a;
  }
  return out;
}

function sortKey(collection: string, photo: MockPhoto): string {
  if (collection === 'recent') return photo.first_seen_at;
  return photo.captured_at ?? '';
}

function ordered(collection: string): MockPhoto[] {
  const items = eligible();
  switch (collection) {
    case 'recent':
      return items
        .filter((p) => !p.is_baseline)
        .sort((a, b) => b.first_seen_at.localeCompare(a.first_seen_at) || b.id.localeCompare(a.id));
    case 'captured_today': {
      const today = homeDay(0);
      return items
        .filter((p) => p.captured_at !== null && p.captured_at.startsWith(today))
        .sort((a, b) => (a.captured_at ?? '').localeCompare(b.captured_at ?? ''));
    }
    case 'all':
    default:
      return items.sort(
        (a, b) => (b.captured_at ?? '').localeCompare(a.captured_at ?? '') || b.id.localeCompare(a.id),
      );
  }
}

export function collectionPage(
  collection: string,
  cursor: string | null,
  limit: number,
  seed: string | null,
): MockCollectionResult {
  if (collection === 'random') {
    const parts = cursor ? cursor.split(':') : [];
    const roundSeed = parts[0] ?? seed ?? String(Math.floor(Math.random() * 1e9));
    const offset = parts.length > 1 ? Number.parseInt(parts[1] ?? '0', 10) || 0 : 0;
    const round = shuffled(eligible(), roundSeed);
    const items = round.slice(offset, offset + limit);
    const end = offset + items.length;
    return {
      items,
      // Null at the end of the round: the client starts a new seeded round.
      nextCursor: end < round.length ? `${roundSeed}:${end}` : null,
      meta: { seed: roundSeed, total: round.length },
    };
  }

  const list = ordered(collection);
  const from = decodeCursor(cursor);
  const start = from
    ? list.findIndex((p) => sortKey(collection, p) === from.key && p.id === from.id) + 1
    : 0;
  const items = list.slice(Math.max(start, 0), Math.max(start, 0) + limit);
  const last = items[items.length - 1];
  const consumed = Math.max(start, 0) + items.length;
  const meta: Record<string, unknown> = { total: list.length };
  if (collection === 'captured_today') {
    meta.unknown_captured_count = eligible().filter((p) => p.captured_at === null).length;
  }
  return {
    items,
    nextCursor: last && consumed < list.length ? encodeCursor(sortKey(collection, last), last.id) : null,
    meta,
  };
}

/** Previous/next ids inside a collection, for `GET /photos/{id}?neighbors=`. */
export function neighborsIn(
  collection: string,
  id: string,
): { previous_id: string | null; next_id: string | null } {
  const list = collection === 'random' ? eligible() : ordered(collection);
  const index = list.findIndex((p) => p.id === id);
  if (index < 0) return { previous_id: null, next_id: null };
  return {
    previous_id: index > 0 ? (list[index - 1]?.id ?? null) : null,
    next_id: index < list.length - 1 ? (list[index + 1]?.id ?? null) : null,
  };
}
