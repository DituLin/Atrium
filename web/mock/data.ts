/**
 * In-memory fixtures for the mock core. Nothing here touches the disk, and no
 * real photo, path or hostname appears (dev-plan §0.1).
 *
 * The photos are dated relative to "now" in the home timezone so that
 * `captured_today` is never permanently empty, and a few of them deliberately
 * carry a non-ready preview or an unknown capture time so the client's skip and
 * "unknown count" rules have something to act on.
 */

export const HOME_TIMEZONE = 'Asia/Singapore';
export const HOME_NAME = 'Demo Home';
export const HOME_UTC_OFFSET_SECONDS = 8 * 3600;

export type MockPreviewStatus = 'ready' | 'pending' | 'failed';

export interface MockPhoto {
  id: string;
  captured_at: string | null;
  captured_confidence: 'exact' | 'inferred' | 'unknown';
  first_seen_at: string;
  is_baseline: boolean;
  preview_status: MockPreviewStatus;
  width: number;
  height: number;
  hue: number;
}

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

/** Wall-clock day in the home timezone, `days` before today. */
export function homeDay(days: number): string {
  const shifted = new Date(Date.now() + HOME_UTC_OFFSET_SECONDS * 1000 - days * 86_400_000);
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

/** 30 deterministic placeholder photos spread over four home-timezone days. */
export const PHOTOS: MockPhoto[] = Array.from({ length: 30 }, (_, index) => {
  const daysAgo = Math.floor(index / 8);
  const hour = 7 + (index % 8) * 2;
  const unknownTime = index % 11 === 10;
  const previewStatus: MockPreviewStatus =
    index % 13 === 12 ? 'pending' : index % 17 === 16 ? 'failed' : 'ready';
  return {
    id: `ph_${pad(index + 1)}`,
    captured_at: unknownTime ? null : `${homeDay(daysAgo)}T${pad(hour)}:15:00+08:00`,
    captured_confidence: unknownTime ? 'unknown' : index % 3 === 0 ? 'inferred' : 'exact',
    first_seen_at: `${homeDay(daysAgo)}T${pad((index % 8) + 1)}:05:00Z`,
    is_baseline: daysAgo >= 3,
    preview_status: previewStatus,
    width: 1600,
    height: index % 4 === 0 ? 1600 : 1200,
    hue: (index * 37) % 360,
  };
});

/** Deterministic SVG placeholder; the browser decodes it like any image. */
export function placeholderSvg(photo: MockPhoto, variant: string): Buffer {
  const size = variant === 'thumb' ? 320 : 1280;
  const height = Math.round((size * photo.height) / photo.width);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${height}" viewBox="0 0 ${size} ${height}">
  <rect width="100%" height="100%" fill="hsl(${photo.hue} 45% 28%)"/>
  <rect x="6%" y="6%" width="88%" height="88%" fill="none" stroke="hsl(${photo.hue} 60% 70%)" stroke-width="8"/>
  <text x="50%" y="52%" font-family="sans-serif" font-size="${Math.round(size / 9)}" fill="hsl(${photo.hue} 70% 88%)" text-anchor="middle">${photo.id}</text>
</svg>`;
  return Buffer.from(svg, 'utf8');
}
